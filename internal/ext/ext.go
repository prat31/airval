// Package ext is the extension point used by enterprise features in ee/ (build tag `ee`).
// Core never imports ee/; instead ee packages call Register from init(), and the binary links
// them in via cmd/airval/ee.go (//go:build ee).
package ext

import (
	"context"
	"log/slog"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/config"
	"github.com/airval/airval/internal/rbac"
	"github.com/airval/airval/internal/store"
)

// Host is what core hands to extensions at startup.
type Host interface {
	// Router is mounted at the server root before core routes (use it for /v1/auth/oidc/*, /scim/v2/* ...).
	Router() chi.Router
	Store() store.Store
	Auth() *auth.Service
	Config() *config.Config
	Logger() *slog.Logger
	// SetRoleOverride installs per-project / custom role resolution.
	SetRoleOverride(rbac.Override)
	// AddAuditSink receives every audit event core emits.
	AddAuditSink(AuditSink)
	// AddSSOProvider advertises an SSO provider in GET /v1/meta.
	AddSSOProvider(name string)
}

// AuditSink receives audit events.
type AuditSink func(ctx context.Context, e store.AuditEntry)

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
