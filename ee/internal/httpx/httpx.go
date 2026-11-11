//go:build ee

// Package httpx holds small HTTP helpers shared by the enterprise route handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api"
	"github.com/prat31/airval/internal/auth"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/rbac"
	"github.com/prat31/airval/internal/store"
)

// MaxBody bounds JSON request bodies.
const MaxBody = 1 << 20

// Errorf builds an API error.
func Errorf(status int, code, format string, args ...any) error {
	return &api.Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Invalid is a 422 validation error.
func Invalid(format string, args ...any) error {
	return Errorf(http.StatusUnprocessableEntity, "validation_failed", format, args...)
}

// Handler is an http handler returning an error (rendered with the core error body).
type Handler func(w http.ResponseWriter, r *http.Request) error

// H adapts handlers for a host.
type H struct{ Host ext.Host }

// Wrap renders errors and tags the request scope with the ee operation id (which keeps custom-role
// permissions out of enterprise administration: those always follow the built-in role).
func (h H) Wrap(op string, fn Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rbac.ScopeFrom(r.Context()).SetOperation("ee." + op)
		if err := fn(w, r); err != nil {
			h.Host.WriteError(w, r, err)
		}
	}
}

// JSON writes v with status.
func JSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// Decode reads a JSON body into v.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxBody))
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return Errorf(http.StatusBadRequest, "bad_request", "request body is required")
		}
		return Errorf(http.StatusBadRequest, "bad_request", "invalid request body: %v", err)
	}
	return nil
}

// UUIDParam parses a UUID URL parameter (invalid ids are reported as not found).
func UUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, store.ErrNotFound
	}
	return id, nil
}

// RequireOrg checks the caller's built-in role in the {org} URL parameter's organization.
func (h H) RequireOrg(r *http.Request, min store.Role) (uuid.UUID, *auth.Principal, error) {
	orgID, err := UUIDParam(r, "org")
	if err != nil {
		return uuid.Nil, nil, err
	}
	p := auth.FromContext(r.Context())
	if p == nil {
		return uuid.Nil, nil, rbac.ErrUnauthenticated
	}
	if err := h.Host.Authz().Require(r.Context(), p, orgID, nil, min); err != nil {
		return uuid.Nil, nil, err
	}
	return orgID, p, nil
}

// RequireUser rejects API-key callers for operations that need a person (security administration).
func RequireUser(p *auth.Principal) error {
	if p == nil || p.User == nil {
		return Errorf(http.StatusForbidden, "forbidden", "this operation requires a user session")
	}
	return nil
}

// Audit records an audit entry for an ee administrative action and marks the call as audited.
func (h H) Audit(r *http.Request, orgID uuid.UUID, action, target string, diff any) {
	ctx := r.Context()
	p := auth.FromContext(ctx)
	e := store.AuditEntry{OrgID: orgID, ActorID: p.UserID(), Actor: p.Actor(), Action: action, Target: target,
		IP: ClientIP(r), UserAgent: r.UserAgent(), RequestID: requestID(r)}
	if diff != nil {
		b, _ := json.Marshal(diff)
		e.Diff = b
	}
	if err := h.Host.Store().AppendAudit(context(r), &e); err != nil {
		h.Host.Logger().Warn("ee audit append failed", "action", action, "err", err)
	}
	rbac.ScopeFrom(ctx).MarkAudited()
}
