package planner

import (
	"encoding/json"
	"maps"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// TriggerConfig mirrors the API's TriggerConfig schema.
type TriggerConfig struct {
	Cron             string            `json:"cron,omitempty"`
	Timezone         string            `json:"timezone,omitempty"`
	TargetIDs        []uuid.UUID       `json:"target_ids,omitempty"`
	Sampling         *Sampling         `json:"sampling,omitempty"`
	Trials           *int              `json:"trials,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Variables        map[string]string `json:"variables,omitempty"`
	PayloadVariables map[string]string `json:"payload_variables,omitempty"`
	PayloadLabels    map[string]string `json:"payload_labels,omitempty"`
}

// ParseTriggerConfig decodes a trigger's config (unknown fields ignored).
func ParseTriggerConfig(raw json.RawMessage) TriggerConfig {
	var c TriggerConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

// FromTrigger builds the start request of a trigger fire. Extra labels/variables (e.g. from a
// webhook payload) override the trigger's static ones.
func FromTrigger(t store.Trigger, c TriggerConfig, labels, vars map[string]string) (StartRequest, []uuid.UUID) {
	req := StartRequest{SuiteID: t.SuiteID, TriggerKind: t.Kind, TriggerRef: t.ID.String(), Sampling: c.Sampling,
		Trials: c.Trials, Labels: map[string]string{}, Variables: map[string]string{}}
	maps.Copy(req.Labels, c.Labels)
	maps.Copy(req.Labels, labels)
	maps.Copy(req.Variables, c.Variables)
	maps.Copy(req.Variables, vars)
	if t.Name != "" {
		req.Labels["trigger"] = t.Name
	}
	return req, c.TargetIDs
}
