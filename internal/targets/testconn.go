package targets

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/prat31/airval/internal/evalcore"
)

// TestReport is what the UI playground shows for "Test connection".
type TestReport struct {
	OK         bool              `json:"ok"`
	Error      string            `json:"error,omitempty"`
	ErrorClass string            `json:"error_class,omitempty"`
	RetryAfter time.Duration     `json:"retry_after,omitempty"`
	Request    json.RawMessage   `json:"request,omitempty"` // rendered request, secrets redacted
	Raw        json.RawMessage   `json:"raw,omitempty"`     // raw response body (possibly truncated)
	Response   evalcore.Response `json:"response"`          // the mapped response
	Mapping    []MappingResult   `json:"mapping,omitempty"` // per JSONPath: matched? value?
	Elapsed    time.Duration     `json:"elapsed"`
}

type mappingDebugger interface {
	DebugMapping(raw []byte) []MappingResult
}

// TestConnection sends one sample example and reports the rendered request, the
// raw response, the mapped Response and how each configured JSONPath matched.
// It never returns an error: failures are described in the report.
func TestConnection(ctx context.Context, a Adapter, sample evalcore.Example, opts InvokeOptions) TestReport {
	start := time.Now()
	resp, err := a.Invoke(ctx, sample, opts)
	rep := TestReport{
		OK: err == nil, Request: resp.Request, Raw: resp.Raw, Response: resp, Elapsed: time.Since(start),
	}
	if err != nil {
		rep.Error = err.Error()
		rep.ErrorClass = Classify(err).String()
		var rl ErrRateLimited
		if errors.As(err, &rl) {
			rep.RetryAfter = rl.RetryAfter
		}
	}
	if d, ok := a.(mappingDebugger); ok && len(resp.Raw) > 0 {
		rep.Mapping = d.DebugMapping(resp.Raw)
	}
	return rep
}
