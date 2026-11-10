// Package targets contains the adapters that call the AI service under
// evaluation (http, agent, openai, anthropic, bedrock, vertex), the shared
// HTTP client factory with its SSRF guard, request templating, JSONPath
// response mapping, multi-turn drivers and evalcore.LLM implementations.
//
// Adapters classify errors (ErrRateLimited / ErrTransient / ErrPermanent) but
// never retry: the runner owns retries, backoff and AIMD.
package targets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

// InvokeOptions carries per-call dependencies.
type InvokeOptions struct {
	// Secrets resolves {{ secret "NAME" }} references.
	Secrets evalcore.SecretResolver
	// Trial is the 0-based repetition number of this example.
	Trial int
	// LLM plays the user in simulated conversations (Conversation.Simulation).
	LLM evalcore.LLM
}

// Adapter calls a target for one example (one trial). It is safe for
// concurrent use and is built once per target version.
type Adapter interface {
	Kind() string
	Invoke(ctx context.Context, ex evalcore.Example, opts InvokeOptions) (evalcore.Response, error)
}

// Deps are the process-wide dependencies of adapters.
type Deps struct {
	// Clients is the shared HTTP client factory (carries the SSRF guard).
	// Nil uses DefaultClients().
	Clients *ClientFactory
	// DefaultConcurrency sizes idle connection pools when a target does not set max_concurrency.
	DefaultConcurrency int
	// Now overrides the clock (SigV4 signing); nil = time.Now.
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Config is implemented by every typed per-kind config struct.
type Config interface {
	Validate() error
}

// Kinds lists the supported target kinds.
func Kinds() []string {
	k := []string{"http", "agent", "openai", "anthropic", "bedrock", "vertex"}
	sort.Strings(k)
	return k
}

func newConfig(kind string) (Config, error) {
	switch kind {
	case "http":
		return &HTTPConfig{}, nil
	case "agent":
		return &AgentConfig{}, nil
	case "openai":
		return &OpenAIConfig{}, nil
	case "anthropic":
		return &AnthropicConfig{}, nil
	case "bedrock":
		return &BedrockConfig{}, nil
	case "vertex":
		return &VertexConfig{}, nil
	}
	return nil, fmt.Errorf("unknown target kind %q (supported: %s)", kind, strings.Join(Kinds(), ", "))
}

// ParseConfig strictly decodes and validates raw config for a kind. Unknown
// fields are rejected so typos surface in the UI.
func ParseConfig(kind string, raw json.RawMessage) (Config, error) {
	cfg, err := newConfig(kind)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("%s config: %w", kind, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s config: %w", kind, err)
	}
	return cfg, nil
}

// ValidateConfig checks config without building an adapter.
func ValidateConfig(kind string, raw json.RawMessage) error {
	_, err := ParseConfig(kind, raw)
	return err
}

// New builds an adapter from a target's raw config. Templates and JSONPaths
// are compiled here, once per target version.
func New(kind string, raw json.RawMessage, deps Deps) (Adapter, error) {
	cfg, err := ParseConfig(kind, raw)
	if err != nil {
		return nil, err
	}
	switch c := cfg.(type) {
	case *HTTPConfig:
		return newHTTPAdapter("http", c, deps)
	case *AgentConfig:
		return newHTTPAdapter("agent", &c.HTTPConfig, deps)
	case *OpenAIConfig:
		p, err := newOpenAIProvider(c, deps)
		if err != nil {
			return nil, err
		}
		return newChatAdapter("openai", &c.ChatCommon, p)
	case *AnthropicConfig:
		p, err := newAnthropicProvider(c, deps)
		if err != nil {
			return nil, err
		}
		return newChatAdapter("anthropic", &c.ChatCommon, p)
	case *BedrockConfig:
		p, err := newBedrockProvider(c, deps)
		if err != nil {
			return nil, err
		}
		return newChatAdapter("bedrock", &c.ChatCommon, p)
	case *VertexConfig:
		p, err := newVertexProvider(c, deps)
		if err != nil {
			return nil, err
		}
		return newChatAdapter("vertex", &c.ChatCommon, p)
	}
	return nil, fmt.Errorf("unhandled kind %q", kind)
}

// turnFunc performs one request/response exchange. It returns the response and
// the session id extracted from it (if any).
type turnFunc func(ctx context.Context, tc *turnCtx) (evalcore.Response, string, error)

// adapter is the generic Adapter: a turn function plus the multi-turn driver.
type adapter struct {
	kind string
	call turnFunc
	dbg  func(raw []byte) []MappingResult
}

func (a *adapter) Kind() string { return a.kind }

func (a *adapter) Invoke(ctx context.Context, ex evalcore.Example, opts InvokeOptions) (evalcore.Response, error) {
	if ex.Conversation == nil {
		r, _, err := a.call(ctx, &turnCtx{ex: ex, opts: opts})
		return r, err
	}
	return runConversation(ctx, a.call, ex, opts)
}

// DebugMapping implements the optional mappingDebugger used by TestConnection.
func (a *adapter) DebugMapping(raw []byte) []MappingResult {
	if a.dbg == nil {
		return nil
	}
	return a.dbg(raw)
}
