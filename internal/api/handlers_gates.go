package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/aggregate"
	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/auth"
	"github.com/airval/airval/internal/store"
)

// Gates are stored as the aggregate package's expression AST with evaluators referenced by
// id (UUID), so renames never break a gate. Clients may write evaluators by name or slug (in
// the text form or the AST); the server resolves them against the suite's evaluators, rewrites
// them to ids, and renders the canonical text back with names.

// gateEvaluators maps a suite's evaluators to the names a gate may use.
type gateEvaluators struct {
	byAlias map[string]string // lower-case slug/name/uuid -> uuid string
	slugOf  map[string]string // uuid string -> slug (as rendered in text)
	idOf    map[string]string // slug -> uuid string
	slugs   []string
}

func gateSlug(name string) string {
	s := auth.Slugify(name)
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		s = "e-" + s // identifiers cannot start with a digit
	}
	return s
}

func newGateEvaluators(evs []store.Evaluator) gateEvaluators {
	g := gateEvaluators{byAlias: map[string]string{}, slugOf: map[string]string{}, idOf: map[string]string{}, slugs: []string{}}
	for _, e := range evs {
		id, slug := e.ID.String(), gateSlug(e.Name)
		g.byAlias[strings.ToLower(id)] = id
		g.byAlias[strings.ToLower(slug)] = id
		g.byAlias[strings.ToLower(e.Name)] = id
		g.slugOf[id] = slug
		g.idOf[slug] = id
		g.slugs = append(g.slugs, slug)
	}
	sort.Strings(g.slugs)
	return g
}

// suiteEvaluatorIDs lists the evaluator ids a suite config references.
func suiteEvaluatorIDs(cfg json.RawMessage) []uuid.UUID {
	var c struct {
		Evaluators []struct {
			EvaluatorID uuid.UUID `json:"evaluator_id"`
		} `json:"evaluators"`
	}
	_ = json.Unmarshal(cfg, &c)
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, e := range c.Evaluators {
		if e.EvaluatorID != uuid.Nil && !seen[e.EvaluatorID] {
			seen[e.EvaluatorID] = true
			out = append(out, e.EvaluatorID)
		}
	}
	return out
}

func (s *Server) gateEvaluators(ctx context.Context, x store.Suite) (gateEvaluators, error) {
	ids := suiteEvaluatorIDs(x.Config)
	if len(ids) == 0 {
		return newGateEvaluators(nil), nil
	}
	evs, err := s.Store.GetEvaluators(ctx, ids)
	if err != nil {
		return gateEvaluators{}, err
	}
	return newGateEvaluators(evs), nil
}

// mapParams applies f to every call parameter of the tree (in place).
func mapParams(n *aggregate.Node, f func(string) string) {
	if n == nil {
		return
	}
	if n.Op == aggregate.OpCall {
		for i, p := range n.Params {
			n.Params[i] = f(p)
		}
	}
	for _, a := range n.Args {
		mapParams(a, f)
	}
}

// parseGateInput turns request text or AST into a validated AST with evaluator ids, the
// canonical text and the referenced evaluator names. field is "text" or "expr" for errors.
func (g gateEvaluators) parse(text *string, expr *json.RawMessage) (*aggregate.Node, string, error) {
	hasText := text != nil && strings.TrimSpace(*text) != ""
	hasExpr := expr != nil && len(bytes.TrimSpace(*expr)) > 0 && string(bytes.TrimSpace(*expr)) != "null"
	var n *aggregate.Node
	field := "expr"
	switch {
	case hasText == hasExpr:
		return nil, "", fieldError("expr", "send exactly one of text or expr")
	case hasText:
		field = "text"
		var err error
		if n, err = aggregate.Parse(*text); err != nil {
			e := fieldError("text", "%v", err)
			var pe *aggregate.ParseError
			if errors.As(err, &pe) {
				e.with("position", pe.Pos)
			}
			return nil, "", e
		}
	default:
		dec := json.NewDecoder(bytes.NewReader(*expr))
		dec.DisallowUnknownFields()
		n = &aggregate.Node{}
		if err := dec.Decode(n); err != nil {
			return nil, "", fieldError("expr", "expr must be a gate expression tree: %v", err)
		}
	}
	// Resolve evaluator names/uuids to slugs so Validate can suggest close names.
	mapParams(n, func(p string) string {
		if id, ok := g.byAlias[strings.ToLower(p)]; ok {
			return g.slugOf[id]
		}
		return p
	})
	if err := aggregate.Validate(n, g.slugs); err != nil {
		e := fieldError(field, "%v", err)
		var ue *aggregate.UnknownEvaluatorError
		if errors.As(err, &ue) {
			e.with("unknown_evaluator", ue.ID).with("known_evaluators", g.slugs)
			if ue.Suggestion != "" {
				e.with("suggestion", ue.Suggestion)
			}
		}
		return nil, "", e
	}
	text2 := n.String()
	refs := map[string]bool{}
	for _, r := range aggregate.References(n) {
		refs[r] = true
	}
	mapParams(n, func(p string) string {
		if refs[p] {
			if id, ok := g.idOf[p]; ok {
				return id
			}
		}
		return p
	})
	return n, text2, nil
}

// gateOut renders a stored gate; evaluator ids in the text are shown as names.
func (g gateEvaluators) gateOut(x store.Gate) gen.Gate {
	out := toGate(x)
	var n aggregate.Node
	if json.Unmarshal(x.Expr, &n) == nil {
		mapParams(&n, func(p string) string {
			if slug, ok := g.slugOf[p]; ok {
				return slug
			}
			return p
		})
		out.Text = n.String()
	}
	return out
}

func (s *Server) gatesOut(ctx context.Context, suiteID uuid.UUID, gs ...store.Gate) ([]gen.Gate, error) {
	x, err := s.Store.GetSuite(ctx, suiteID)
	if err != nil {
		return nil, err
	}
	g, err := s.gateEvaluators(ctx, x)
	if err != nil {
		return nil, err
	}
	return mapSlice(gs, g.gateOut), nil
}

func (s *Server) ListGates(ctx context.Context, req gen.ListGatesRequestObject) (gen.ListGatesResponseObject, error) {
	if _, err := s.suite(ctx, req.Suite, store.RoleViewer); err != nil {
		return nil, err
	}
	gs, err := s.Store.ListGates(ctx, req.Suite)
	if err != nil {
		return nil, err
	}
	out, err := s.gatesOut(ctx, req.Suite, gs...)
	if err != nil {
		return nil, err
	}
	return gen.ListGates200JSONResponse{Items: out}, nil
}

func (s *Server) CreateGate(ctx context.Context, req gen.CreateGateRequestObject) (gen.CreateGateResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldError("expr", "send text or expr")
	}
	ge, err := s.gateEvaluators(ctx, x)
	if err != nil {
		return nil, err
	}
	var raw *json.RawMessage
	if req.Body.Expr != nil {
		raw = (*json.RawMessage)(req.Body.Expr)
	}
	node, _, err := ge.parse(req.Body.Text, raw)
	if err != nil {
		return nil, err
	}
	g := store.Gate{OrgID: x.OrgID, ProjectID: x.ProjectID, SuiteID: x.ID, Name: trimmed(req.Body.Name), Expr: mustJSON(node),
		Enabled: deref(req.Body.Enabled, true)}
	if err := s.Store.CreateGate(ctx, &g); err != nil {
		return nil, err
	}
	return gen.CreateGate201JSONResponse(ge.gateOut(g)), nil
}

func (s *Server) UpdateGate(ctx context.Context, req gen.UpdateGateRequestObject) (gen.UpdateGateResponseObject, error) {
	g, err := s.suiteGate(ctx, req.Suite, req.Gate, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	x, err := s.Store.GetSuite(ctx, req.Suite)
	if err != nil {
		return nil, err
	}
	ge, err := s.gateEvaluators(ctx, x)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil {
			g.Name = strings.TrimSpace(*b.Name)
		}
		if b.Expr != nil || b.Text != nil {
			var raw *json.RawMessage
			if b.Expr != nil {
				raw = (*json.RawMessage)(b.Expr)
			}
			node, _, err := ge.parse(b.Text, raw)
			if err != nil {
				return nil, err
			}
			g.Expr = mustJSON(node)
		}
		if b.Enabled != nil {
			g.Enabled = *b.Enabled
		}
	}
	if err := s.Store.UpdateGate(ctx, &g); err != nil {
		return nil, err
	}
	return gen.UpdateGate200JSONResponse(ge.gateOut(g)), nil
}

// ValidateGate checks a gate without saving it (live validation in the gate builder).
func (s *Server) ValidateGate(ctx context.Context, req gen.ValidateGateRequestObject) (gen.ValidateGateResponseObject, error) {
	x, err := s.suite(ctx, req.Suite, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, badRequest("request body is required")
	}
	ge, err := s.gateEvaluators(ctx, x)
	if err != nil {
		return nil, err
	}
	var raw *json.RawMessage
	if req.Body.Expr != nil {
		raw = (*json.RawMessage)(req.Body.Expr)
	}
	node, text, err := ge.parse(req.Body.Text, raw)
	if err != nil {
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 400 {
			return nil, err
		}
		res := gen.GateValidateResult{Ok: false, Error: ptr(ae.Message)}
		if p, ok := ae.Details["position"].(int); ok {
			res.Position = ptr(p)
		}
		if sg, ok := ae.Details["suggestion"].(string); ok {
			res.Suggestion = ptr(sg)
		}
		return gen.ValidateGate200JSONResponse(res), nil
	}
	var names []string
	for _, r := range aggregate.References(&aggregate.Node{Op: aggregate.OpAnd, Args: []*aggregate.Node{node}}) {
		if slug, ok := ge.slugOf[r]; ok {
			names = append(names, slug)
		}
	}
	e := json.RawMessage(mustJSON(node))
	res := gen.GateValidateResult{Ok: true, Expr: (*gen.GateExpr)(&e), Text: &text}
	if len(names) > 0 {
		res.Evaluators = &names
	}
	return gen.ValidateGate200JSONResponse(res), nil
}
