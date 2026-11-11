package aggregate

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Gate expressions decide whether a run passes. Grammar (case-insensitive
// keywords):
//
//	expr    = or
//	or      = and { ("or" | "||") and }
//	and     = unary { ("and" | "&&") unary }
//	unary   = ("not" | "!") unary | primary
//	primary = "(" expr ")" | compare
//	compare = operand cmp operand          cmp = >= | > | <= | < | == | = | !=
//	operand = number | call                number may end in % (95% = 0.95)
//	call    = ident "(" [ ident { "," ident } ] ")"
//
// Functions (see Funcs): mean, median, p50, p90, p95, p99, min, max, stddev,
// sum, pass_rate, count, regression_vs_baseline. Arguments are evaluator ids,
// built-in metrics (latency_ms, cost_usd, tokens, input_tokens,
// output_tokens), "all" (pass_rate), or a count category.
//
// Example: mean(faithfulness) >= 0.85 and count(critical_fail) == 0

// Node is a gate expression AST node, JSON-serializable for storage and the
// visual builder.
type Node struct {
	// Op is one of: and, or, not, cmp, call, num.
	Op string `json:"op"`
	// Args: and/or (2+ operands), not (1), cmp (left, right).
	Args []*Node `json:"args,omitempty"`
	// Cmp is the comparison operator for op=cmp: >=, >, <=, <, ==, !=.
	Cmp string `json:"cmp,omitempty"`
	// Func and Params for op=call, e.g. mean(faithfulness).
	Func   string   `json:"func,omitempty"`
	Params []string `json:"params,omitempty"`
	// Value for op=num.
	Value *float64 `json:"value,omitempty"`
}

// Node ops.
const (
	OpAnd  = "and"
	OpOr   = "or"
	OpNot  = "not"
	OpCmp  = "cmp"
	OpCall = "call"
	OpNum  = "num"
)

// Constructors for building ASTs in code.
func And(args ...*Node) *Node               { return &Node{Op: OpAnd, Args: args} }
func Or(args ...*Node) *Node                { return &Node{Op: OpOr, Args: args} }
func Not(n *Node) *Node                     { return &Node{Op: OpNot, Args: []*Node{n}} }
func Cmp(l *Node, op string, r *Node) *Node { return &Node{Op: OpCmp, Cmp: op, Args: []*Node{l, r}} }
func Call(fn string, params ...string) *Node {
	return &Node{Op: OpCall, Func: fn, Params: params}
}
func Num(v float64) *Node { return &Node{Op: OpNum, Value: &v} }

// Count categories for count(...).
const (
	CountFail         = "fail"
	CountCriticalFail = "critical_fail"
	CountMajorFail    = "major_fail"
	CountMinorFail    = "minor_fail"
	CountError        = "error"
	CountPending      = "pending"
	CountPass         = "pass"
	CountResults      = "results"
)

var countCategories = []string{CountFail, CountCriticalFail, CountMajorFail, CountMinorFail, CountError, CountPending, CountPass, CountResults}

// FuncInfo documents a gate function (for the visual builder).
type FuncInfo struct {
	Name        string `json:"name"`
	Args        string `json:"args"`
	Description string `json:"description"`
}

// Funcs lists every gate function.
var Funcs = []FuncInfo{
	{"mean", "evaluator | metric", "Average score (or metric) over all results."},
	{"median", "evaluator | metric", "Median score (or metric)."},
	{"p50", "evaluator | metric", "50th percentile."},
	{"p90", "evaluator | metric", "90th percentile."},
	{"p95", "evaluator | metric", "95th percentile, e.g. p95(latency_ms) < 2000."},
	{"p99", "evaluator | metric", "99th percentile."},
	{"min", "evaluator | metric", "Lowest value."},
	{"max", "evaluator | metric", "Highest value."},
	{"stddev", "evaluator | metric", "Standard deviation."},
	{"sum", "metric", "Total, e.g. sum(cost_usd) for the run's target cost."},
	{"pass_rate", "evaluator | all", "Share of results that passed (0–1). pass_rate(all) counts results where every evaluator passed."},
	{"count", "fail | critical_fail | major_fail | minor_fail | error | pending | pass | results [, evaluator]", "Number of results in a category, optionally for one evaluator."},
	{"regression_vs_baseline", "evaluator", "Mean score change vs the baseline run over shared examples (negative = worse). Skipped when there is no baseline."},
}

func funcInfo(name string) (FuncInfo, bool) {
	for _, f := range Funcs {
		if f.Name == name {
			return f, true
		}
	}
	return FuncInfo{}, false
}

func funcNames() []string {
	out := make([]string, len(Funcs))
	for i, f := range Funcs {
		out[i] = f.Name
	}
	return out
}

// ---------------------------------------------------------------- printer

func precedence(n *Node) int {
	switch n.Op {
	case OpOr:
		return 1
	case OpAnd:
		return 2
	case OpNot:
		return 3
	}
	return 4
}

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// String renders the expression as canonical text; Parse(n.String())
// yields an equal AST.
func (n *Node) String() string {
	var b strings.Builder
	n.write(&b)
	return b.String()
}

func (n *Node) write(b *strings.Builder) {
	if n == nil {
		b.WriteString("<nil>")
		return
	}
	child := func(c *Node, minPrec int) {
		if c != nil && precedence(c) < minPrec {
			b.WriteByte('(')
			c.write(b)
			b.WriteByte(')')
			return
		}
		c.write(b)
	}
	switch n.Op {
	case OpAnd, OpOr:
		p := precedence(n)
		for i, a := range n.Args {
			if i > 0 {
				b.WriteString(" " + n.Op + " ")
			}
			// Same-precedence children are flattened by the parser; parenthesize
			// them so the round trip keeps the structure.
			child(a, p+1)
		}
	case OpNot:
		b.WriteString("not ")
		if len(n.Args) == 1 {
			child(n.Args[0], precedence(n))
		}
	case OpCmp:
		if len(n.Args) == 2 {
			n.Args[0].write(b)
			b.WriteString(" " + n.Cmp + " ")
			n.Args[1].write(b)
		}
	case OpCall:
		b.WriteString(n.Func)
		b.WriteByte('(')
		b.WriteString(strings.Join(n.Params, ", "))
		b.WriteByte(')')
	case OpNum:
		if n.Value != nil {
			b.WriteString(formatNum(*n.Value))
		}
	default:
		b.WriteString("<" + n.Op + ">")
	}
}

// ---------------------------------------------------------------- lexer

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tOp // comparison
	tLParen
	tRParen
	tComma
	tAnd
	tOr
	tNot
)

type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
}

// ParseError points at the offending position of the expression.
type ParseError struct {
	Pos int    `json:"pos"`
	Msg string `json:"message"`
}

func (e *ParseError) Error() string { return fmt.Sprintf("gate: %s (at position %d)", e.Msg, e.Pos+1) }

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func isIdentPart(r rune) bool {
	return r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func lex(s string) ([]token, error) {
	var toks []token
	rs := []rune(s)
	i := 0
	for i < len(rs) {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			toks = append(toks, token{kind: tLParen, text: "(", pos: i})
			i++
		case r == ')':
			toks = append(toks, token{kind: tRParen, text: ")", pos: i})
			i++
		case r == ',':
			toks = append(toks, token{kind: tComma, text: ",", pos: i})
			i++
		case r == '&' || r == '|':
			if i+1 < len(rs) && rs[i+1] == r {
				k := tAnd
				if r == '|' {
					k = tOr
				}
				toks = append(toks, token{kind: k, text: string([]rune{r, r}), pos: i})
				i += 2
				continue
			}
			return nil, &ParseError{i, fmt.Sprintf("unexpected %q (use %c%c or %s)", r, r, r, map[rune]string{'&': "and", '|': "or"}[r])}
		case r == '>' || r == '<' || r == '=' || r == '!':
			two := ""
			if i+1 < len(rs) {
				two = string(rs[i : i+2])
			}
			switch two {
			case ">=", "<=", "==", "!=":
				toks = append(toks, token{kind: tOp, text: two, pos: i})
				i += 2
				continue
			}
			switch r {
			case '>', '<':
				toks = append(toks, token{kind: tOp, text: string(r), pos: i})
			case '=':
				toks = append(toks, token{kind: tOp, text: "==", pos: i})
			case '!':
				toks = append(toks, token{kind: tNot, text: "!", pos: i})
			}
			i++
		case unicode.IsDigit(r) || r == '.' || ((r == '-' || r == '+') && i+1 < len(rs) && (unicode.IsDigit(rs[i+1]) || rs[i+1] == '.')):
			j := i + 1
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.' || rs[j] == '_' ||
				((rs[j] == 'e' || rs[j] == 'E') && j+1 < len(rs) && (unicode.IsDigit(rs[j+1]) || rs[j+1] == '-' || rs[j+1] == '+')) ||
				((rs[j] == '-' || rs[j] == '+') && (rs[j-1] == 'e' || rs[j-1] == 'E'))) {
				j++
			}
			text := strings.ReplaceAll(string(rs[i:j]), "_", "")
			v, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, &ParseError{i, fmt.Sprintf("invalid number %q", string(rs[i:j]))}
			}
			if j < len(rs) && rs[j] == '%' {
				v /= 100
				j++
			}
			toks = append(toks, token{kind: tNum, text: string(rs[i:j]), num: v, pos: i})
			i = j
		case isIdentStart(r):
			j := i + 1
			for j < len(rs) && isIdentPart(rs[j]) {
				j++
			}
			word := string(rs[i:j])
			switch strings.ToLower(word) {
			case "and":
				toks = append(toks, token{kind: tAnd, text: word, pos: i})
			case "or":
				toks = append(toks, token{kind: tOr, text: word, pos: i})
			case "not":
				toks = append(toks, token{kind: tNot, text: word, pos: i})
			default:
				toks = append(toks, token{kind: tIdent, text: word, pos: i})
			}
			i = j
		default:
			return nil, &ParseError{i, fmt.Sprintf("unexpected character %q", r)}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: len(rs)})
	return toks, nil
}

// ---------------------------------------------------------------- parser

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

// Parse parses a gate expression and checks that it is well-typed (a
// boolean condition built from numeric comparisons). Use Validate to also
// check evaluator ids against a suite.
func Parse(s string) (*Node, error) {
	if strings.TrimSpace(s) == "" {
		return nil, &ParseError{0, "empty expression"}
	}
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, &ParseError{t.pos, fmt.Sprintf("unexpected %q; join conditions with and/or", t.text)}
	}
	return n, nil
}

// MustParse is Parse for static expressions; it panics on error.
func MustParse(s string) *Node {
	n, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return n
}

func (p *parser) or() (*Node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	args := []*Node{l}
	for p.peek().kind == tOr {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		args = append(args, r)
	}
	if len(args) == 1 {
		return l, nil
	}
	return Or(args...), nil
}

func (p *parser) and() (*Node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	args := []*Node{l}
	for p.peek().kind == tAnd {
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		args = append(args, r)
	}
	if len(args) == 1 {
		return l, nil
	}
	return And(args...), nil
}

func (p *parser) unary() (*Node, error) {
	if p.peek().kind == tNot {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Not(x), nil
	}
	if p.peek().kind == tLParen {
		open := p.next()
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		if t := p.next(); t.kind != tRParen {
			return nil, &ParseError{t.pos, fmt.Sprintf("missing ')' to close '(' at position %d", open.pos+1)}
		}
		return x, nil
	}
	return p.compare()
}

func (p *parser) compare() (*Node, error) {
	l, err := p.operand()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.kind != tOp {
		return nil, &ParseError{t.pos, fmt.Sprintf("expected a comparison (>=, >, <=, <, ==, !=) after %s", l)}
	}
	p.next()
	r, err := p.operand()
	if err != nil {
		return nil, err
	}
	return Cmp(l, t.text, r), nil
}

func (p *parser) operand() (*Node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		return Num(t.num), nil
	case tIdent:
		if p.peek().kind != tLParen {
			return nil, &ParseError{t.pos, fmt.Sprintf("%q must be called as a function, e.g. mean(%s)", t.text, t.text)}
		}
		p.next()
		var params []string
		for p.peek().kind != tRParen {
			a := p.next()
			if a.kind != tIdent {
				if a.kind == tEOF {
					return nil, &ParseError{a.pos, fmt.Sprintf("missing ')' after %s(", t.text)}
				}
				return nil, &ParseError{a.pos, fmt.Sprintf("expected an evaluator or metric name in %s(...), got %q", t.text, a.text)}
			}
			params = append(params, a.text)
			if p.peek().kind == tComma {
				p.next()
			} else if p.peek().kind != tRParen {
				q := p.peek()
				return nil, &ParseError{q.pos, fmt.Sprintf("expected ',' or ')' in %s(...)", t.text)}
			}
		}
		p.next()
		fn := strings.ToLower(t.text)
		if _, ok := funcInfo(fn); !ok {
			msg := fmt.Sprintf("unknown function %q", t.text)
			if s := suggest(fn, funcNames()); s != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", s)
			}
			return nil, &ParseError{t.pos, msg}
		}
		n := Call(fn, params...)
		if err := checkArity(n); err != nil {
			return nil, &ParseError{t.pos, err.Error()}
		}
		return n, nil
	case tEOF:
		return nil, &ParseError{t.pos, "unexpected end of expression"}
	}
	return nil, &ParseError{t.pos, fmt.Sprintf("expected a number or function call, got %q", t.text)}
}

func checkArity(n *Node) error {
	switch n.Func {
	case "count":
		if len(n.Params) < 1 || len(n.Params) > 2 {
			return errors.New("count takes a category and an optional evaluator, e.g. count(critical_fail) or count(fail, faithfulness)")
		}
		cat := strings.ToLower(n.Params[0])
		for _, c := range countCategories {
			if c == cat {
				return nil
			}
		}
		msg := fmt.Sprintf("unknown count category %q (use %s)", n.Params[0], strings.Join(countCategories, ", "))
		return errors.New(msg)
	default:
		if len(n.Params) != 1 {
			return fmt.Errorf("%s takes exactly one argument", n.Func)
		}
	}
	return nil
}

// ---------------------------------------------------------------- validation

// Validate checks the AST shape (so hand-built or stored ASTs are safe) and,
// when evaluators is non-nil, that every referenced evaluator exists.
func Validate(n *Node, evaluators []string) error {
	return validate(n, evaluators, true)
}

func validate(n *Node, evals []string, wantBool bool) error {
	if n == nil {
		return errors.New("gate: empty expression")
	}
	switch n.Op {
	case OpAnd, OpOr:
		if !wantBool {
			return fmt.Errorf("gate: %q used where a number is expected", n.Op)
		}
		if len(n.Args) < 2 {
			return fmt.Errorf("gate: %s needs at least two conditions", n.Op)
		}
		for _, a := range n.Args {
			if err := validate(a, evals, true); err != nil {
				return err
			}
		}
	case OpNot:
		if !wantBool || len(n.Args) != 1 {
			return errors.New("gate: not takes one condition")
		}
		return validate(n.Args[0], evals, true)
	case OpCmp:
		if !wantBool {
			return errors.New("gate: a comparison cannot be compared")
		}
		switch n.Cmp {
		case ">=", ">", "<=", "<", "==", "!=":
		default:
			return fmt.Errorf("gate: unknown comparison %q", n.Cmp)
		}
		if len(n.Args) != 2 {
			return errors.New("gate: a comparison needs two operands")
		}
		for _, a := range n.Args {
			if err := validate(a, evals, false); err != nil {
				return err
			}
		}
	case OpNum:
		if wantBool {
			return fmt.Errorf("gate: %s is a number, not a condition", n)
		}
		if n.Value == nil || math.IsNaN(*n.Value) {
			return errors.New("gate: number node without a value")
		}
	case OpCall:
		if wantBool {
			return fmt.Errorf("gate: %s is a number; compare it, e.g. %s >= 0.9", n, n)
		}
		if _, ok := funcInfo(n.Func); !ok {
			return fmt.Errorf("gate: unknown function %q", n.Func)
		}
		if err := checkArity(n); err != nil {
			return fmt.Errorf("gate: %w", err)
		}
		if evals != nil {
			if id, ok := evaluatorRef(n); ok && !contains(evals, id) {
				return unknownEvaluator(id, evals)
			}
		}
	default:
		return fmt.Errorf("gate: unknown node op %q", n.Op)
	}
	return nil
}

// evaluatorRef returns the evaluator id a call refers to, if any.
func evaluatorRef(n *Node) (string, bool) {
	switch n.Func {
	case "count":
		if len(n.Params) == 2 {
			return n.Params[1], true
		}
		return "", false
	case "pass_rate":
		if strings.EqualFold(n.Params[0], MetricAll) {
			return "", false
		}
		return n.Params[0], true
	case "regression_vs_baseline":
		return n.Params[0], true
	case "sum":
		if isBuiltinMetric(n.Params[0]) {
			return "", false
		}
		return n.Params[0], true
	default:
		if isBuiltinMetric(n.Params[0]) {
			return "", false
		}
		return n.Params[0], true
	}
}

// UnknownEvaluatorError reports a gate reference to a missing evaluator.
type UnknownEvaluatorError struct {
	ID         string
	Suggestion string
	Known      []string
}

func (e *UnknownEvaluatorError) Error() string {
	msg := fmt.Sprintf("gate: unknown evaluator %q", e.ID)
	if e.Suggestion != "" {
		msg += fmt.Sprintf(" (did you mean %q?)", e.Suggestion)
	} else if len(e.Known) > 0 {
		msg += " (suite evaluators: " + strings.Join(e.Known, ", ") + ")"
	}
	return msg
}

func unknownEvaluator(id string, known []string) error {
	return &UnknownEvaluatorError{ID: id, Suggestion: suggest(id, known), Known: known}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// References lists the evaluator ids an expression uses.
func References(n *Node) []string {
	var out []string
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Op == OpCall {
			if id, ok := evaluatorRef(n); ok && !contains(out, id) {
				out = append(out, id)
			}
		}
		for _, a := range n.Args {
			walk(a)
		}
	}
	walk(n)
	return out
}

func suggest(s string, candidates []string) string {
	best, bestD := "", 1<<30
	for _, c := range candidates {
		if d := editDistance(strings.ToLower(s), strings.ToLower(c)); d < bestD {
			best, bestD = c, d
		}
	}
	if best == "" || bestD > max(2, len(s)/3) {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			cur[j] = min(prev[j-1]+c, prev[j]+1, cur[j-1]+1)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
