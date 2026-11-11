package targets

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var varRef = regexp.MustCompile(`\{\{\s*\.?vars\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// ApplyVariables substitutes {{ .vars.NAME }} references anywhere in a raw target config
// (templates and plain fields such as base_url) with run variables. Values are JSON-escaped
// since they land inside JSON strings. Unset variables are an error.
func ApplyVariables(raw json.RawMessage, vars map[string]string) (json.RawMessage, error) {
	if !varRef.Match(raw) {
		return raw, nil
	}
	var missing []string
	out := varRef.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(varRef.FindSubmatch(m)[1])
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return nil
		}
		b, _ := json.Marshal(v)
		return b[1 : len(b)-1]
	})
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("target config references unset variable(s): %s", strings.Join(slices.Compact(missing), ", "))
	}
	return out, nil
}
