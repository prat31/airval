# Enterprise edition

airval is open core. The core (everything outside `ee/`) is Apache-2.0 and runs without any enterprise code. Enterprise
features live only under `ee/`, under a separate license ([`LICENSE-EE`](../LICENSE-EE)), and are compiled in with the `ee`
build tag. Core never imports `ee/`.

> Status: the enterprise features below are being implemented. This page describes the intended scope at a high level;
> check `GET /v1/meta` on your server for what is actually enabled.

## Building and checking the edition

```sh
make build-ee           # produces bin/airval-ee (go build -tags ee)
./bin/airval-ee version # prints "..., ee"
curl $B/v1/meta         # {"edition":"ee","features":{"sso":true,"sso_providers":["..."],"audit_log":true,...},...}
```

The `edition` field is `core` or `ee`; `features` lists what is enabled (`sso`, `sso_providers`, `audit_log`, `signup`).
In a core build, enterprise endpoints answer `501`. The edition is chosen by the binary: `airval` (core) and `airval-ee`
share the same configuration, database schema and API.

## Planned features

| Feature | What it gives you |
|---|---|
| **SSO** (OIDC, SAML) | Sign in through your identity provider. OIDC endpoints are `GET /v1/auth/oidc/{provider}/start` and `/callback`; configured providers appear in `GET /v1/meta` (`sso_providers`). |
| **SCIM** | Automatic user and group provisioning and deprovisioning from the identity provider (`/scim/v2/*`). |
| **Custom roles and per-project RBAC** | Roles beyond owner / admin / editor / viewer, and role overrides per Project. |
| **Audit log** | Complete record of who changed what and when, queryable with `GET /v1/orgs/{org}/audit-log` and exportable to sinks. Core records a subset of events. |
| **Data retention** | Policies that delete Runs and Results older than a configured age, per Project (the `retention_days` Project setting). |
| **White-labeling** | Custom branding of the UI. |

## How it plugs in

Enterprise modules implement `internal/ext.Extension`, register themselves from `init()` and are linked by `cmd/airval/ee.go`
(`//go:build ee`). At startup they receive a host that lets them mount routes (before core routes), install role resolution, receive audit events and advertise SSO providers. Contributors:
see [Contributing](contributing.md).

## Licensing

Using `ee/` code in production requires a commercial license. The core remains fully functional on its own, including Runs,
Gates, CI, scheduling, webhooks, review, local accounts and API keys.
