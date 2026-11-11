package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// validateSuiteConfig checks a suite config: shapes, value ranges, and that every referenced
// target, dataset (and pinned version) and evaluator exists in the project. Errors are 400s
// whose details.field is the JSON path of the offending value.
func (s *Server) validateSuiteConfig(ctx context.Context, projectID uuid.UUID, raw json.RawMessage) error {
	var c struct {
		TargetIDs []uuid.UUID `json:"target_ids"`
		Datasets  []struct {
			DatasetID uuid.UUID `json:"dataset_id"`
			Version   *int      `json:"version"`
		} `json:"datasets"`
		Evaluators []struct {
			EvaluatorID uuid.UUID `json:"evaluator_id"`
			Threshold   *float64  `json:"threshold"`
			Severity    string    `json:"severity"`
			ApplyTo     string    `json:"apply_to"`
			Weight      *float64  `json:"weight"`
		} `json:"evaluators"`
		Trials *int `json:"trials"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return fieldError("config", "config does not match the suite schema: %v", err)
	}
	if c.Trials != nil && (*c.Trials < 1 || *c.Trials > 10) {
		return fieldError("config.trials", "trials must be between 1 and 10")
	}
	for i, id := range c.TargetIDs {
		t, err := s.Store.GetTarget(ctx, id)
		if err != nil || t.ProjectID != projectID {
			return fieldError(fmt.Sprintf("config.target_ids[%d]", i), "target %s does not exist in this project", id)
		}
	}
	for i, d := range c.Datasets {
		ds, err := s.Store.GetDataset(ctx, d.DatasetID)
		if err != nil || ds.ProjectID != projectID {
			return fieldError(fmt.Sprintf("config.datasets[%d].dataset_id", i), "dataset %s does not exist in this project", d.DatasetID)
		}
		if d.Version != nil {
			if _, err := s.Store.GetDatasetVersionByNumber(ctx, d.DatasetID, *d.Version); err != nil {
				return fieldError(fmt.Sprintf("config.datasets[%d].version", i), "dataset %q has no version %d", ds.Name, *d.Version)
			}
		}
	}
	if len(c.Evaluators) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(c.Evaluators))
	for i, e := range c.Evaluators {
		ids[i] = e.EvaluatorID
	}
	found, err := s.Store.GetEvaluators(ctx, ids)
	if err != nil {
		return err
	}
	ok := map[uuid.UUID]bool{}
	for _, e := range found {
		if e.ProjectID == projectID {
			ok[e.ID] = true
		}
	}
	for i, e := range c.Evaluators {
		f := fmt.Sprintf("config.evaluators[%d]", i)
		switch {
		case !ok[e.EvaluatorID]:
			return fieldError(f+".evaluator_id", "evaluator %s does not exist in this project", e.EvaluatorID)
		case e.Threshold != nil && (*e.Threshold < 0 || *e.Threshold > 1):
			return fieldError(f+".threshold", "threshold must be between 0 and 1")
		case e.Severity != "" && !set("critical", "major", "minor")[e.Severity]:
			return fieldError(f+".severity", "severity must be critical, major or minor")
		case e.ApplyTo != "" && !set("final", "per_turn", "conversation", "trace")[e.ApplyTo]:
			return fieldError(f+".apply_to", "apply_to must be final, per_turn, conversation or trace")
		case e.Weight != nil && *e.Weight < 0:
			return fieldError(f+".weight", "weight must be >= 0")
		}
	}
	return nil
}
