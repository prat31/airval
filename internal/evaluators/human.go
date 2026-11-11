package evaluators

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/prat31/airval/internal/evalcore"
)

// Review modes for HumanConfig.Mode.
const (
	ReviewAll      = "all"
	ReviewFailures = "failures"
	ReviewSample   = "sample"
)

// HumanConfig configures the human evaluator.
type HumanConfig struct {
	Mode         string   `json:"mode,omitempty" title:"Send to review" enum:"all|failures|sample" default:"all" desc:"all: every result. failures: only results another evaluator failed or errored on. sample: a random share of results."`
	SampleRate   float64  `json:"sample_rate,omitempty" title:"Sample rate" min:"0" max:"1" default:"0.1" desc:"With mode = sample: share of results to review (0.1 = 10%). Sampling is deterministic per example, so re-runs review the same examples."`
	Scale        string   `json:"scale,omitempty" title:"Grading scale" enum:"pass_fail|1_5|labels" default:"pass_fail" desc:"How reviewers grade: pass/fail, a 1–5 score, or one of your labels."`
	Labels       []string `json:"labels,omitempty" title:"Labels" ui:"list" desc:"With scale = labels: the labels reviewers choose from."`
	Instructions string   `json:"instructions,omitempty" title:"Reviewer instructions" ui:"textarea" desc:"Guidance shown to reviewers next to each result."`
	Queue        string   `json:"queue,omitempty" title:"Review queue" desc:"Name of the review queue to use. Empty uses the suite's default queue."`
}

func (c *HumanConfig) Validate() error {
	switch c.Mode {
	case "":
		c.Mode = ReviewAll
	case ReviewAll, ReviewFailures, ReviewSample:
	default:
		return fmt.Errorf("mode: must be all, failures or sample (got %q)", c.Mode)
	}
	if c.Mode == ReviewSample {
		if c.SampleRate == 0 {
			c.SampleRate = 0.1
		}
		if c.SampleRate < 0 || c.SampleRate > 1 {
			return errors.New("sample_rate must be between 0 and 1")
		}
	}
	switch c.Scale {
	case "":
		c.Scale = "pass_fail"
	case "pass_fail", "1_5":
	case "labels":
		if len(c.Labels) == 0 {
			return errors.New("labels are required when scale = labels")
		}
	default:
		return fmt.Errorf("scale: must be pass_fail, 1_5 or labels (got %q)", c.Scale)
	}
	return nil
}

type humanEval struct{ c *HumanConfig }

func init() {
	register(KindInfo{
		Kind: "human", Name: "Human review", Category: CategoryHuman, Group: groupHuman,
		Cost: CostHuman, DefaultThreshold: 0.5,
		ApplyTo:     []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyConversation},
		Description: "Sends results to a review queue where people grade them with keyboard shortcuts. Review everything, only failures, or a sample. Human grades can calibrate your LLM judges.",
	}, func(c *HumanConfig, _ Deps) (Evaluator, error) {
		return &humanEval{c: c}, nil
	})
}

func (h *humanEval) Kind() string    { return "human" }
func (h *humanEval) Cost() CostClass { return CostHuman }

// sampled deterministically decides whether an example falls in the sample.
func sampled(key string, rate float64) bool {
	f := fnv.New64a()
	f.Write([]byte(key))
	return float64(f.Sum64()%1_000_000)/1_000_000 < rate
}

func anyFailed(prior []evalcore.Score) bool {
	for _, s := range prior {
		if (s.Passed != nil && !*s.Passed) || s.Label == LabelError {
			return true
		}
	}
	return false
}

// Evaluate never blocks: it returns a pending score (Value nil, Label
// "pending") and sets Meta["review_required"] so the runner enqueues a review
// item. Results outside the review selection get Label "skipped".
func (h *humanEval) Evaluate(_ context.Context, in Input) (evalcore.Score, error) {
	var required bool
	var why string
	switch h.c.Mode {
	case ReviewFailures:
		required = anyFailed(in.Prior)
		why = "no other evaluator failed"
	case ReviewSample:
		key := in.example().ID
		if in.Turn != nil {
			key += "#" + strconv.Itoa(*in.Turn)
		}
		required = sampled(key, h.c.SampleRate)
		why = "not in the review sample"
	default:
		required = true
	}
	if !required {
		return evalcore.Score{
			Label:  LabelSkipped,
			Reason: "not sent to review: " + why,
			Meta:   map[string]any{MetaReviewRequired: false, MetaSkipped: true},
		}, nil
	}
	review := map[string]any{"mode": h.c.Mode, "scale": h.c.Scale}
	if len(h.c.Labels) > 0 {
		review["labels"] = h.c.Labels
	}
	if h.c.Instructions != "" {
		review["instructions"] = h.c.Instructions
	}
	if h.c.Queue != "" {
		review["queue"] = h.c.Queue
	}
	return evalcore.Score{
		Label:  LabelPending,
		Reason: "waiting for human review",
		Meta:   map[string]any{MetaReviewRequired: true, MetaReview: review},
	}, nil
}

// ReviewRequired reports whether a score asks the runner to enqueue a review.
func ReviewRequired(s evalcore.Score) bool {
	v, _ := s.Meta[MetaReviewRequired].(bool)
	return v
}
