//go:build ee

// Package ee contains airval enterprise features: OIDC/SAML SSO, SCIM provisioning, custom roles and
// per-project RBAC, the full audit log, data retention and white-labeling. It is licensed separately
// (see LICENSE-EE) and is only compiled with `-tags ee`. Core never imports this package; it plugs in
// through internal/ext.
package ee

import (
	"context"
	"time"

	"github.com/prat31/airval/ee/audit"
	"github.com/prat31/airval/ee/branding"
	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/ee/retention"
	"github.com/prat31/airval/ee/roles"
	"github.com/prat31/airval/ee/scim"
	"github.com/prat31/airval/ee/sso"
	"github.com/prat31/airval/internal/ext"
)

func init() { ext.Register(Module{}) }

// Features lists the enterprise features this module enables (GET /v1/edition).
var Features = []string{"sso", "saml", "scim", "custom_roles", "audit_log", "retention", "branding"}

// Module is the enterprise extension.
type Module struct{}

func (Module) Name() string { return "ee" }

// Init wires every enterprise feature into the host.
func (Module) Init(_ context.Context, host ext.Host) error {
	h := httpx.H{Host: host}
	r := host.Router()

	az := &roles.Authz{Store: host.Store()}
	host.SetRoleOverride(az.Override)
	host.SetDecider(az.Decide)
	roles.Routes(r, h)

	s := sso.New(host, nil)
	s.Routes(r)
	host.SetAuthProviders(s.AuthProviders)

	scim.New(h).Routes(r)

	a := audit.New(h)
	a.Routes(r)
	host.AddCallObserver(a.Observe)

	ret := retention.New(h)
	ret.Routes(r)

	branding.Routes(r, h)

	host.EnableFeatures(Features...)

	host.Go("ee.retention", func(ctx context.Context) {
		host.RunAsLeader(ctx, retention.LeaseName, func(lctx context.Context) {
			t := time.NewTicker(time.Hour)
			defer t.Stop()
			for {
				if n, err := ret.Apply(lctx); err != nil && lctx.Err() == nil {
					host.Logger().Warn("ee: apply data retention", "err", err)
				} else if n > 0 {
					host.Logger().Info("ee: data retention deleted runs", "runs", n)
				}
				if n, err := a.ApplyRetention(lctx); err != nil && lctx.Err() == nil {
					host.Logger().Warn("ee: apply audit retention", "err", err)
				} else if n > 0 {
					host.Logger().Info("ee: audit retention deleted entries", "entries", n)
				}
				select {
				case <-lctx.Done():
					return
				case <-t.C:
				}
			}
		})
	})
	return nil
}
