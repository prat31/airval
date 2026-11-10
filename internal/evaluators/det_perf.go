package evaluators

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/airval/airval/internal/evalcore"
)

// LatencyConfig configures latency_ms.
type LatencyConfig struct {
	MaxMs  float64 `json:"max_ms" title:"Max latency (ms)" min:"0" desc:"Fail when the target took longer than this many milliseconds."`
	Metric string  `json:"metric,omitempty" title:"Measure" enum:"total|ttft" default:"total" desc:"total: full response time. ttft: time to first token (streaming targets)."`
}

func (c *LatencyConfig) Validate() error {
	if c.MaxMs <= 0 {
		return errors.New("max_ms must be > 0")
	}
	switch c.Metric {
	case "":
		c.Metric = "total"
	case "total", "ttft":
	default:
		return fmt.Errorf("metric: must be total or ttft (got %q)", c.Metric)
	}
	return nil
}

// CostConfig configures cost_usd.
type CostConfig struct {
	MaxUSD float64 `json:"max_usd" title:"Max cost (USD)" min:"0" desc:"Fail when one response cost more than this, in US dollars."`
}

func (c *CostConfig) Validate() error {
	if c.MaxUSD <= 0 {
		return errors.New("max_usd must be > 0")
	}
	return nil
}

// TokensConfig configures tokens.
type TokensConfig struct {
	Count string `json:"count,omitempty" title:"Count" enum:"total|input|output" default:"total" desc:"Which tokens to count, as reported by the target."`
	Min   *int   `json:"min,omitempty" title:"Minimum" min:"0" desc:"Fail when fewer tokens were used."`
	Max   *int   `json:"max,omitempty" title:"Maximum" min:"0" desc:"Fail when more tokens were used."`
}

func (c *TokensConfig) Validate() error {
	switch c.Count {
	case "":
		c.Count = "total"
	case "total", "input", "output":
	default:
		return fmt.Errorf("count: must be total, input or output (got %q)", c.Count)
	}
	if c.Min == nil && c.Max == nil {
		return errors.New("set min, max or both")
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return errors.New("min must not exceed max")
	}
	return nil
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func init() {
	perf := []evalcore.ApplyTo{evalcore.ApplyFinal}
	register(KindInfo{
		Kind: "latency_ms", Name: "Latency", Category: CategoryDeterministic, Group: groupPerf, DefaultThreshold: 1, ApplyTo: perf,
		Description: "Passes when the response arrived within a time budget (total time or time to first token).",
	}, func(c *LatencyConfig, _ Deps) (Evaluator, error) {
		return free("latency_ms", func(_ context.Context, in Input) (evalcore.Score, error) {
			r := in.response()
			d := r.Latency
			if c.Metric == "ttft" {
				d = r.TTFT
			}
			v := ms(d)
			s := boolScore(v <= c.MaxMs, fmt.Sprintf("%s %.0f ms (budget %.0f ms)", c.Metric, v, c.MaxMs))
			s.Meta = map[string]any{"latency_ms": v}
			return s, nil
		}), nil
	})
	register(KindInfo{
		Kind: "cost_usd", Name: "Cost", Category: CategoryDeterministic, Group: groupPerf, DefaultThreshold: 1, ApplyTo: perf,
		Description: "Passes when a single response cost no more than a dollar budget.",
	}, func(c *CostConfig, _ Deps) (Evaluator, error) {
		return free("cost_usd", func(_ context.Context, in Input) (evalcore.Score, error) {
			v := in.response().CostUSD
			s := boolScore(v <= c.MaxUSD, fmt.Sprintf("$%.6f (budget $%.6f)", v, c.MaxUSD))
			s.Meta = map[string]any{"cost_usd": v}
			return s, nil
		}), nil
	})
	register(KindInfo{
		Kind: "tokens", Name: "Token usage", Category: CategoryDeterministic, Group: groupPerf, DefaultThreshold: 1, ApplyTo: perf,
		Description: "Passes when the number of tokens the target reported (total, input or output) is within bounds.",
	}, func(c *TokensConfig, _ Deps) (Evaluator, error) {
		return free("tokens", func(_ context.Context, in Input) (evalcore.Score, error) {
			u := in.response().Usage
			n := u.InputTokens + u.OutputTokens
			switch c.Count {
			case "input":
				n = u.InputTokens
			case "output":
				n = u.OutputTokens
			}
			var s evalcore.Score
			switch {
			case c.Min != nil && n < *c.Min:
				s = boolScore(false, fmt.Sprintf("%d %s tokens, below the minimum of %d", n, c.Count, *c.Min))
			case c.Max != nil && n > *c.Max:
				s = boolScore(false, fmt.Sprintf("%d %s tokens, above the maximum of %d", n, c.Count, *c.Max))
			default:
				s = boolScore(true, fmt.Sprintf("%d %s tokens", n, c.Count))
			}
			s.Meta = map[string]any{"tokens": n}
			return s, nil
		}), nil
	})
}
