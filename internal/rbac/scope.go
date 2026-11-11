package rbac

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Scope is request-scoped authorization context. The API stores the operation being served; the
// Authorizer records the organization/project every access check resolved to, so middleware (audit)
// can attribute a call without extra queries, and ee can memoize lookups per request.
type Scope struct {
	mu        sync.Mutex
	operation string
	orgID     uuid.UUID
	projectID *uuid.UUID
	audited   bool
	memo      map[any]any
}

type scopeKey struct{}

// WithScope returns ctx carrying a new Scope.
func WithScope(ctx context.Context) (context.Context, *Scope) {
	sc := &Scope{}
	return context.WithValue(ctx, scopeKey{}, sc), sc
}

// ScopeFrom returns the request's Scope or nil.
func ScopeFrom(ctx context.Context) *Scope {
	sc, _ := ctx.Value(scopeKey{}).(*Scope)
	return sc
}

// Operation is the API operation id being served ("" when none was set).
func (s *Scope) Operation() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operation
}

// SetOperation records the operation id.
func (s *Scope) SetOperation(op string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.operation = op
	s.mu.Unlock()
}

func (s *Scope) record(orgID uuid.UUID, projectID *uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orgID == uuid.Nil {
		s.orgID = orgID
	}
	if projectID != nil && s.projectID == nil && orgID == s.orgID {
		id := *projectID
		s.projectID = &id
	}
}

// Resource returns the first organization (and project) an access check resolved to.
func (s *Scope) Resource() (uuid.UUID, *uuid.UUID) {
	if s == nil {
		return uuid.Nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orgID, s.projectID
}

// MarkAudited notes that the handler already wrote a detailed audit entry for this call.
func (s *Scope) MarkAudited() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.audited = true
	s.mu.Unlock()
}

// Audited reports whether MarkAudited was called.
func (s *Scope) Audited() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.audited
}

// Memo returns the cached value for key, computing it with fn once per request (errors are not cached).
func (s *Scope) Memo(key any, fn func() (any, error)) (any, error) {
	if s == nil {
		return fn()
	}
	s.mu.Lock()
	if v, ok := s.memo[key]; ok {
		s.mu.Unlock()
		return v, nil
	}
	s.mu.Unlock()
	v, err := fn()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.memo == nil {
		s.memo = map[any]any{}
	}
	s.memo[key] = v
	s.mu.Unlock()
	return v, nil
}
