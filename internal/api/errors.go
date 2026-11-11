package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/rbac"
	"github.com/airval/airval/internal/store"
)

// Error is an API error with an HTTP status. Handlers return it (or store/rbac sentinels) as the
// error value; the strict server's error handler renders the JSON body.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func badRequest(format string, args ...any) *Error {
	return errorf(http.StatusBadRequest, "bad_request", format, args...)
}

func invalid(format string, args ...any) *Error {
	return errorf(http.StatusUnprocessableEntity, "validation_failed", format, args...)
}

func notFound(what string) *Error {
	return errorf(http.StatusNotFound, "not_found", "%s not found", what)
}

// notImplemented marks endpoints owned by other workstreams.
func notImplemented(feature string) *Error {
	return errorf(http.StatusNotImplemented, "not_implemented", "%s is not implemented yet", feature)
}

// toError maps any error to an API error.
func toError(err error) *Error {
	var ae *Error
	switch {
	case errors.As(err, &ae):
		return ae
	case errors.Is(err, store.ErrNotFound), errors.Is(err, rbac.ErrNoAccess):
		return errorf(http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, store.ErrConflict):
		return errorf(http.StatusConflict, "conflict", "a resource with the same unique name or slug already exists")
	case errors.Is(err, store.ErrInvalidCursor):
		return badRequest("invalid cursor")
	case errors.Is(err, rbac.ErrUnauthenticated), errors.Is(err, auth.ErrUnauthenticated):
		return errorf(http.StatusUnauthorized, "unauthorized", "authentication required")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return errorf(http.StatusUnauthorized, "unauthorized", "invalid email or password")
	case errors.Is(err, rbac.ErrForbidden):
		return errorf(http.StatusForbidden, "forbidden", "your role does not allow this action")
	case errors.Is(err, auth.ErrCSRF):
		return errorf(http.StatusForbidden, "csrf", "missing or invalid CSRF token (send X-CSRF-Token)")
	case errors.Is(err, auth.ErrSetupDone):
		return errorf(http.StatusConflict, "setup_completed", "setup has already been completed")
	case errors.Is(err, auth.ErrWeakPassword):
		return invalid("%v", err)
	}
	return errorf(http.StatusInternalServerError, "internal", "internal error")
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	ae := toError(err)
	if ae.Status >= 500 && ae.Status != http.StatusNotImplemented {
		s.Logger.Error("request failed", "err", err, "method", r.Method, "path", r.URL.Path,
			"request_id", middleware.GetReqID(r.Context()))
	}
	body := gen.Error{Code: ae.Code, Message: ae.Message}
	if id := middleware.GetReqID(r.Context()); id != "" {
		body.RequestId = &id
	}
	if ae.Details != nil {
		body.Details = &ae.Details
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ae.Status)
	_ = json.NewEncoder(w).Encode(body)
}

func debugStack() []byte { return debug.Stack() }
