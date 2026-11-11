package evaluators

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/prat31/airval/internal/evalcore"
)

// Category groups kinds in the UI picker.
const (
	CategoryDeterministic = "deterministic"
	CategoryModel         = "model"
	CategoryHuman         = "human"
)

// Data an evaluator needs from the example/response (KindInfo.Requires).
const (
	NeedsExpected   = "expected"
	NeedsContext    = "context"
	NeedsBaseline   = "baseline"
	NeedsTranscript = "transcript"
	NeedsToolCalls  = "tool_calls"
)

// KindInfo describes one evaluator kind for the UI and API.
type KindInfo struct {
	Kind             string             `json:"kind"`
	Name             string             `json:"name"`
	Category         string             `json:"category"`
	Group            string             `json:"group"`
	Description      string             `json:"description"`
	Cost             CostClass          `json:"cost"`
	DefaultThreshold float64            `json:"default_threshold"`
	ConfigSchema     json.RawMessage    `json:"config_schema"`
	ApplyTo          []evalcore.ApplyTo `json:"apply_to"`
	Requires         []string           `json:"requires,omitempty"`
	PromptVersion    string             `json:"prompt_version,omitempty"`
	Prompt           string             `json:"prompt,omitempty"` // judge prompt template, shown to users
}

// validator is implemented by every config struct. Validate checks the config
// and fills in defaults.
type validator interface {
	Validate() error
}

type registration struct {
	info      KindInfo
	newConfig func() validator
	build     func(cfg validator, deps Deps) (Evaluator, error)
}

var (
	regMu    sync.RWMutex
	registry = map[string]*registration{}
)

// register adds a kind. C is the config struct; *C must implement validator.
func register[C any, PC interface {
	*C
	validator
}](info KindInfo, build func(c *C, deps Deps) (Evaluator, error)) {
	if info.ApplyTo == nil {
		info.ApplyTo = []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyConversation}
	}
	info.ConfigSchema = schemaFor(new(C))
	r := &registration{
		info:      info,
		newConfig: func() validator { return PC(new(C)) },
		build: func(cfg validator, deps Deps) (Evaluator, error) {
			return build((*C)(cfg.(PC)), deps)
		},
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[info.Kind]; dup {
		panic("evaluators: duplicate kind " + info.Kind)
	}
	registry[info.Kind] = r
}

func lookup(kind string) (*registration, error) {
	regMu.RLock()
	r := registry[kind]
	regMu.RUnlock()
	if r == nil {
		if s := suggest(kind, Kinds()); s != "" {
			return nil, fmt.Errorf("%w %q (did you mean %q?)", ErrUnknownKind, kind, s)
		}
		return nil, fmt.Errorf("%w %q", ErrUnknownKind, kind)
	}
	return r, nil
}

// ParseConfig decodes and validates raw into the kind's config struct
// (with defaults applied). Unknown fields are rejected.
func ParseConfig(kind string, raw json.RawMessage) (any, error) {
	r, err := lookup(kind)
	if err != nil {
		return nil, err
	}
	return r.parse(raw)
}

func (r *registration) parse(raw json.RawMessage) (validator, error) {
	cfg := r.newConfig()
	dec := json.NewDecoder(bytes.NewReader(rawOrEmpty(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("%s: invalid config: %w", r.info.Kind, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", r.info.Kind, err)
	}
	return cfg, nil
}

// ValidateConfig reports whether raw is a valid config for kind, without
// needing runtime dependencies. Use it in API handlers.
func ValidateConfig(kind string, raw json.RawMessage) error {
	_, err := ParseConfig(kind, raw)
	return err
}

// New builds an evaluator of the given kind from its raw JSON config.
func New(kind string, raw json.RawMessage, deps Deps) (Evaluator, error) {
	r, err := lookup(kind)
	if err != nil {
		return nil, err
	}
	cfg, err := r.parse(raw)
	if err != nil {
		return nil, err
	}
	ev, err := r.build(cfg, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", kind, err)
	}
	return ev, nil
}

// Info returns the catalog entry of one kind.
func Info(kind string) (KindInfo, error) {
	r, err := lookup(kind)
	if err != nil {
		return KindInfo{}, err
	}
	return r.info, nil
}

// Kinds returns every registered kind, sorted.
func Kinds() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var categoryOrder = map[string]int{CategoryDeterministic: 0, CategoryModel: 1, CategoryHuman: 2}

// Catalog describes every evaluator kind, ordered by category, group, then
// name. The UI renders evaluator pickers and config forms from it.
func Catalog() []KindInfo {
	regMu.RLock()
	out := make([]KindInfo, 0, len(registry))
	for _, r := range registry {
		out = append(out, r.info)
	}
	regMu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if categoryOrder[a.Category] != categoryOrder[b.Category] {
			return categoryOrder[a.Category] < categoryOrder[b.Category]
		}
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.Name < b.Name
	})
	return out
}

// suggest returns the closest candidate to s by edit distance, or "" when
// nothing is close enough to be a plausible typo.
func suggest(s string, candidates []string) string {
	best, bestD := "", 1<<30
	ls := strings.ToLower(s)
	for _, c := range candidates {
		d := levenshteinDistance(ls, strings.ToLower(c))
		if d < bestD {
			best, bestD = c, d
		}
	}
	if best == "" || bestD > max(2, len(s)/3) {
		return ""
	}
	return best
}

// Suggest is the exported typo helper used by gates for unknown evaluator ids.
func Suggest(s string, candidates []string) string { return suggest(s, candidates) }
