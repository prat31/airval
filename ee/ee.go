//go:build ee

// Package ee contains airval enterprise features (OIDC/SAML SSO, SCIM, custom roles and per-project
// RBAC, full audit log, retention, white-labeling). It is licensed separately (see LICENSE-EE) and
// is only compiled with `-tags ee`. Core never imports this package.
package ee

import (
	"context"
	"net/http"

	"github.com/airval/airval/internal/ext"
)

func init() { ext.Register(module{}) }

type module struct{}

func (module) Name() string { return "ee" }

// Init wires enterprise features into the host. Feature packages (ee/sso, ee/audit, ...) are
// added in milestone 12; this skeleton registers the SSO route surface.
func (module) Init(_ context.Context, h ext.Host) error {
	r := h.Router()
	notConfigured := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"code":"not_configured","message":"SSO provider is not configured"}`))
	}
	r.Get("/v1/auth/oidc/{provider}/start", notConfigured)
	r.Get("/v1/auth/oidc/{provider}/callback", notConfigured)
	return nil
}
