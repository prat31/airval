package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// Principal is the authenticated caller: a user with a session, or an API key.
type Principal struct {
	User    *store.User    // set for sessions
	Session *store.Session // set for sessions
	APIKey  *store.APIKey  // set for API keys
}

// IsAPIKey reports whether the caller authenticated with an API key.
func (p *Principal) IsAPIKey() bool { return p != nil && p.APIKey != nil }

// UserID returns the user id for sessions (nil for API keys).
func (p *Principal) UserID() *uuid.UUID {
	if p == nil || p.User == nil {
		return nil
	}
	id := p.User.ID
	return &id
}

// Actor is a short human-readable identity for audit logs.
func (p *Principal) Actor() string {
	switch {
	case p == nil:
		return "anonymous"
	case p.User != nil:
		return p.User.Email
	case p.APIKey != nil:
		return "apikey:" + p.APIKey.Prefix
	}
	return "unknown"
}

type ctxKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal or nil.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}
