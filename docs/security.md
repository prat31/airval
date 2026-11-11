# Security

## Authentication and authorization

- **Local accounts** with argon2id password hashes and secure cookie sessions (`HttpOnly`, `SameSite=Lax`, `Secure` when
  `auth.cookie_secure=true`). Session lifetime is `auth.session_ttl` (default 720h). Mutations made with a session require a CSRF token.
- **First boot:** if no users exist, `/setup` (or `AIRVAL_BOOTSTRAP_ADMIN_*`) creates the first owner and Organization. Self-service
  signup is off unless `auth.allow_signup` is set.
- **Roles:** `owner`, `admin`, `editor`, `viewer`, per Organization. See [API](api.md#authentication).
- **API keys:** prefix `airv_` plus a random value; only the SHA-256 hash is stored and the key is shown once. Keys have a role,
  can be scoped to one Project and can expire. Use the least role that works (`editor` for CI).
- **SSO, SCIM, custom roles:** see [Enterprise](enterprise.md).

Run airval behind TLS, set `server.base_url` and `auth.cookie_secure=true`.

## Secrets

Credentials for Targets, LLM judges and notification channels are **Secrets**:

- Encrypted at rest with AES-256-GCM using the master key (`secrets.master_key`, 32 bytes base64), with a key version for rotation
  ([Deployment](deployment.md#secrets-master-key-and-rotation)).
- **Write-only** through the API: after creation the plaintext is never returned.
- Decrypted only inside workers at request time, referenced as `{{ secret "NAME" }}`.
- Rendered requests stored on Results and shown in "Test connection" have every resolved secret value replaced by `[REDACTED]`.
- Never logged.
- Scoped to an Organization or a Project.

Keep the master key in a secret manager, not in the YAML file or the image. Back it up separately from the database.

## Outbound requests and the SSRF guard

Targets, webhooks and notification channels make outbound requests on your behalf. The guard validates the **resolved IP at
dial time** (defeating DNS rebinding and redirect tricks) and never uses environment proxies.

- Always denied: link-local and cloud metadata addresses (`169.254.0.0/16`, `fe80::/10`, `fd00:ec2::/32`, `100.100.100.200`, `192.0.0.192`).
- `security.deny_cidrs` adds ranges to deny. `security.allow_cidrs` switches to allow-list mode.
- Loopback and private ranges are reachable by default so you can test internal services; in a shared deployment set
  `security.allow_cidrs` (or `deny_cidrs` for `10.0.0.0/8` and friends) to restrict editors.

Anyone with the `editor` role can point a Target at a URL, so treat the policy as part of your network security.
In Kubernetes also enable `networkPolicy.enabled`.

## Webhooks

Inbound webhook triggers are authenticated by HMAC-SHA256 over `timestamp + "." + body` with a per-trigger secret (shown once,
rotatable), and reject timestamps more than 5 minutes off, which bounds replay. Compare signatures in constant time if you
verify airval's outgoing signed webhooks yourself. Payloads are capped at 1 MiB. See [Runs](runs.md#webhook).

## Container and cluster hardening

The image is distroless and non-root (uid 65532) with no shell, and runs with a read-only root filesystem. Compose and Helm drop all
capabilities, disable privilege escalation and use the `RuntimeDefault` seccomp profile. The Helm chart offers a NetworkPolicy.

## Operational advice

- Rotate API keys and webhook secrets on a schedule. Revoke a key with `DELETE /v1/orgs/{org}/api-keys/{key}`.
- Use Project-scoped keys for CI.
- Keep Postgres and NATS on a private network with authentication and TLS.
- Review the audit log (`GET /v1/orgs/{org}/audit-log`, admin only; full coverage in the enterprise build).
- LLM judges and Targets send Example data to the providers you configure. Treat datasets as the data you are willing to send there.

## Reporting vulnerabilities

Report security issues privately to the maintainers through GitHub security advisories on `github.com/prat31/airval` rather than in a public issue.
