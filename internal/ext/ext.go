// Package ext is the extension point used by enterprise features in ee/ (build tag `ee`).
// Core never imports ee/; instead ee packages call Register from init(), and the binary links
// them in via cmd/airval/ee.go (//go:build ee).
package ext

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/config"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/secrets"
	"github.com/prat31/airval/internal/store"
)

// Host is what core hands to extensions at startup.
type Host interface {
	// Router is mounted at the server root. Routes registered here replace core routes with the same
	// pattern (core answers 501 on documented ee paths). Wrap authenticated routes with Authenticate.
	Router() chi.Router
	Store() store.Store
	Auth() *auth.Service
	Authz() *rbac.Authorizer
	Keyring() *secrets.Keyring
	Config() *config.Config
	Logger() *slog.Logger

	// Authenticate is the core API middleware: it resolves the session/API-key principal, enforces CSRF on
	// session mutations, starts the request rbac.Scope and reports mutating calls to call observers.
	Authenticate(next http.Handler) http.Handler
	// WriteError renders err as a core API error body (rbac/store sentinels map to 401/403/404/409).
	WriteError(w http.ResponseWriter, r *http.Request, err error)

	// SetRoleOverride installs per-project / assigned role resolution.
	SetRoleOverride(rbac.Override)
	// SetDecider installs permission-based decisions (custom roles).
	SetDecider(rbac.Decider)
	// AddAuditSink receives every audit event core emits.
	AddAuditSink(AuditSink)
	// AddCallObserver receives every authenticated mutating API call after it completes.
	AddCallObserver(CallObserver)
	// AddSSOProvider advertises a static SSO provider name in GET /v1/meta.
	AddSSOProvider(name string)
	// SetAuthProviders lists the login providers shown on the sign-in page (GET /v1/auth/providers).
	SetAuthProviders(AuthProvidersFunc)
	// EnableFeatures adds feature flags to GET /v1/edition.
	EnableFeatures(features ...string)
	// Go runs fn in the background for the life of the server process (started with the HTTP server).
	Go(name string, fn func(ctx context.Context))
	// RunAsLeader runs fn while this replica holds the named cluster-wide lease (blocks until ctx is done).
	RunAsLeader(ctx context.Context, lease string, fn func(ctx context.Context))
}

// AuditSink receives audit events.
type AuditSink func(ctx context.Context, e store.AuditEntry)

// AuthProvider is a login option shown on the sign-in page.
type AuthProvider struct {
	ID       string // provider slug
	Kind     string // oidc | saml
	Name     string
	StartURL string
}

// AuthProvidersFunc lists the enabled login providers.
type AuthProvidersFunc func(ctx context.Context) ([]AuthProvider, error)

// APICall describes one completed mutating API call (POST/PUT/PATCH/DELETE).
type APICall struct {
	Principal *auth.Principal
	Method    string
	Path      string
	Route     string // chi route pattern, e.g. /v1/datasets/{dataset}
	Operation string // OpenAPI operation id when known
	Params    map[string]string
	Status    int
	// OrgID/ProjectID are what the request's authorization checks resolved to (uuid.Nil when none).
	OrgID     uuid.UUID
	ProjectID *uuid.UUID
	// Audited is true when the handler already recorded a detailed audit entry.
	Audited   bool
	IP        string
	UserAgent string
	RequestID string
	// Body is the request body (JSON, truncated to 64 KiB). It may contain secrets: redact before storing.
	Body []byte
	// CreatedID is the "id" of a 201 response body, when present.
	CreatedID string
	At        time.Time
	Duration  time.Duration
}

// CallObserver is notified after mutating API calls.
type CallObserver func(ctx context.Context, c APICall)

// Extension is an enterprise feature module.
type Extension interface {
	Name() string
	Init(ctx context.Context, h Host) error
}

var (
	mu   sync.Mutex
	exts []Extension
)

// Register adds an extension (call from init()).
func Register(e Extension) {
	mu.Lock()
	defer mu.Unlock()
	exts = append(exts, e)
}

// All returns the registered extensions.
func All() []Extension {
	mu.Lock()
	defer mu.Unlock()
	return append([]Extension(nil), exts...)
}

// Edition is "ee" when any extension is linked in, else "core".
func Edition() string {
	if len(All()) > 0 {
		return "ee"
	}
	return "core"
}
