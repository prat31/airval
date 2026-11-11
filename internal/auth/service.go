package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/store"
)

// Cookie and header names.
const (
	SessionCookie = "airval_session"
	CSRFCookie    = "airval_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrUnauthenticated    = errors.New("auth: not authenticated")
	ErrCSRF               = errors.New("auth: missing or invalid CSRF token")
	ErrSetupDone          = errors.New("auth: setup already completed")
	ErrWeakPassword       = fmt.Errorf("auth: password must be at least %d characters", MinPasswordLen)
)

// Service authenticates requests and manages sessions.
type Service struct {
	Store store.Store
	Cfg   config.Auth
	Log   *slog.Logger
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) ttl() time.Duration {
	if s.Cfg.SessionTTL <= 0 {
		return 30 * 24 * time.Hour
	}
	return s.Cfg.SessionTTL
}

// Authenticate resolves the caller from the Authorization header (API key) or session cookie.
// It returns (nil, nil) for anonymous requests and ErrUnauthenticated for bad credentials.
func (s *Service) Authenticate(r *http.Request) (*Principal, error) {
	ctx := r.Context()
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || !IsAPIKey(tok) {
			return nil, ErrUnauthenticated
		}
		return s.authenticateKey(ctx, tok)
	}
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	se, err := s.Store.GetSessionByTokenHash(ctx, HashToken(c.Value))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthenticated
	} else if err != nil {
		return nil, err
	}
	now := s.now()
	if now.After(se.ExpiresAt) {
		_ = s.Store.DeleteSession(ctx, se.ID)
		return nil, ErrUnauthenticated
	}
	u, err := s.Store.GetUser(ctx, se.UserID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	// Sliding expiry, written at most every 5 minutes.
	if now.Sub(se.LastSeenAt) > 5*time.Minute {
		se.LastSeenAt, se.ExpiresAt = now, now.Add(s.ttl())
		if err := s.Store.TouchSession(ctx, se.ID, se.LastSeenAt, se.ExpiresAt); err != nil {
			s.Log.Warn("auth: touch session", "err", err)
		}
	}
	return &Principal{User: &u, Session: &se}, nil
}

func (s *Service) authenticateKey(ctx context.Context, key string) (*Principal, error) {
	k, err := s.Store.GetAPIKeyByHash(ctx, HashToken(key))
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthenticated
	} else if err != nil {
		return nil, err
	}
	now := s.now()
	if k.ExpiresAt != nil && now.After(*k.ExpiresAt) {
		return nil, ErrUnauthenticated
	}
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) > time.Minute {
		if err := s.Store.TouchAPIKey(ctx, k.ID, now); err != nil {
			s.Log.Warn("auth: touch api key", "err", err)
		}
	}
	return &Principal{APIKey: &k}, nil
}

// CheckCSRF enforces the CSRF token for session-authenticated unsafe requests.
func CheckCSRF(r *http.Request, p *Principal) error {
	if p == nil || p.Session == nil {
		return nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	tok := r.Header.Get(CSRFHeader)
	if tok == "" || !constantEq(tok, p.Session.CSRFToken) {
		return ErrCSRF
	}
	return nil
}

func constantEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// Login verifies credentials and creates a session. It returns the session and its raw token.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (store.User, store.Session, string, error) {
	u, err := s.Store.GetUserByEmail(ctx, email)
	if err != nil || u.PasswordHash == nil {
		_, _ = VerifyPassword(dummyHash, password)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return u, store.Session{}, "", err
		}
		return u, store.Session{}, "", ErrInvalidCredentials
	}
	ok, err := VerifyPassword(*u.PasswordHash, password)
	if err != nil || !ok {
		return u, store.Session{}, "", ErrInvalidCredentials
	}
	se, tok, err := s.NewSession(ctx, u.ID, userAgent, ip)
	return u, se, tok, err
}

// NewSession creates a session for userID.
func (s *Service) NewSession(ctx context.Context, userID uuid.UUID, userAgent, ip string) (store.Session, string, error) {
	tok := NewToken()
	se := store.Session{UserID: userID, TokenHash: HashToken(tok), CSRFToken: NewToken(), UserAgent: trunc(userAgent, 256),
		IP: trunc(ip, 64), ExpiresAt: s.now().Add(s.ttl())}
	err := s.Store.CreateSession(ctx, &se)
	return se, tok, err
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SetSessionCookies writes the session (HttpOnly) and CSRF (JS-readable) cookies.
func (s *Service) SetSessionCookies(w http.ResponseWriter, se store.Session, token string) {
	exp := se.ExpiresAt
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", Domain: s.Cfg.CookieDomain, Expires: exp,
		HttpOnly: true, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: CSRFCookie, Value: se.CSRFToken, Path: "/", Domain: s.Cfg.CookieDomain, Expires: exp,
		Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

// ClearSessionCookies removes both cookies.
func (s *Service) ClearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", Domain: s.Cfg.CookieDomain, MaxAge: -1,
			HttpOnly: name == SessionCookie, Secure: s.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a name into a URL slug.
func Slugify(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	if s == "" {
		s = "default"
	}
	return s
}

// SetupInput is the first-run owner + organization.
type SetupInput struct {
	Email, Password, Name, OrgName, OrgSlug string
}

// Setup creates the first owner and organization. It fails with ErrSetupDone if any user exists.
func (s *Service) Setup(ctx context.Context, in SetupInput) (store.User, store.Organization, error) {
	var (
		u   store.User
		org store.Organization
	)
	if len(in.Password) < MinPasswordLen {
		return u, org, ErrWeakPassword
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return u, org, err
	}
	err = s.Store.WithTx(ctx, func(tx store.Store) error {
		n, err := tx.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		name := in.Name
		if name == "" {
			name, _, _ = strings.Cut(in.Email, "@")
		}
		u = store.User{Email: in.Email, Name: name, PasswordHash: &hash}
		if err := tx.CreateUser(ctx, &u); err != nil {
			return err
		}
		slug := in.OrgSlug
		if slug == "" {
			slug = Slugify(in.OrgName)
		}
		org = store.Organization{Name: in.OrgName, Slug: slug}
		if err := tx.CreateOrg(ctx, &org); err != nil {
			return err
		}
		return tx.UpsertMembership(ctx, &store.Membership{OrgID: org.ID, UserID: u.ID, Role: store.RoleOwner})
	})
	return u, org, err
}

// Bootstrap runs Setup from AIRVAL_BOOTSTRAP_ADMIN_* settings when configured and no users exist.
func (s *Service) Bootstrap(ctx context.Context, b config.Bootstrap) error {
	if b.AdminEmail == "" || b.AdminPassword == "" {
		return nil
	}
	_, _, err := s.Setup(ctx, SetupInput{Email: b.AdminEmail, Password: b.AdminPassword, Name: b.AdminName, OrgName: b.OrgName})
	switch {
	case errors.Is(err, ErrSetupDone):
		return nil
	case err != nil:
		return fmt.Errorf("auth: bootstrap admin: %w", err)
	}
	s.Log.Info("bootstrapped admin user and organization", "email", strings.ToLower(b.AdminEmail), "org", b.OrgName)
	return nil
}
