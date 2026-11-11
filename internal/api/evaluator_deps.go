package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/evaluators"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
)

// validEvaluatorKind checks the kind against the evaluator registry (with a "did you mean").
func validEvaluatorKind(kind string) error {
	if _, err := evaluators.Info(kind); err != nil {
		return fieldError("kind", "%v", err).with("kinds", evaluators.Kinds())
	}
	return nil
}

// validateEvaluatorConfig validates a kind's config strictly (unknown fields are rejected).
func validateEvaluatorConfig(kind string, cfg json.RawMessage) error {
	if err := evaluators.ValidateConfig(kind, cfg); err != nil {
		return fieldError("config", "%v", err).with("kind", kind)
	}
	return nil
}

// evaluatorDeps wires model-based evaluators: judges call the provider named in their config
// through internal/targets, authenticating with a secret of the project.
func (s *Server) evaluatorDeps(ctx context.Context, proj *store.Project) evaluators.Deps {
	return evaluators.Deps{LLMs: func(provider, model, secret string) (evalcore.LLM, error) {
		if proj == nil {
			return nil, errors.New("send project_id so judges can use the project's secrets")
		}
		cfg := map[string]any{"model": model}
		switch provider {
		case "openai", "anthropic", "vertex":
			if secret != "" {
				cfg["api_key"] = "{{ secret " + strconv.Quote(secret) + " }}"
			}
		default:
			return nil, fmt.Errorf("judge provider %q is not supported (use openai, anthropic or vertex)", provider)
		}
		return targets.NewLLM(provider, mustJSON(cfg), s.targetDeps(), s.secretResolver(ctx, *proj))
	}}
}
