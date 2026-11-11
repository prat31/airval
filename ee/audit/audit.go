//go:build ee

// Package audit records every mutating API call in the audit log (via the core call-observer hook),
// exports the log as CSV and enforces audit-log retention.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/prat31/airval/ee/internal/httpx"
	"github.com/prat31/airval/internal/ext"
	"github.com/prat31/airval/internal/store"
)

const settingsKey = "audit"

// Settings is the org's audit configuration.
type Settings struct {
	RetentionDays int `json:"retention_days"`
}

// Service records and serves the audit log.
type Service struct {
	h   httpx.H
	now func() time.Time
}

// New returns the audit service.
func New(h httpx.H) *Service {
	return &Service{h: h, now: func() time.Time { return time.Now().UTC() }}
}

// Routes registers export and settings endpoints.
func (s *Service) Routes(r chi.Router) {
	a := r.With(s.h.Host.Authenticate)
	a.Get("/v1/orgs/{org}/audit-log/export", s.h.Wrap("exportAuditLog", s.export))
	a.Get("/v1/orgs/{org}/audit-settings", s.h.Wrap("getAuditSettings", s.getSettings))
	a.Put("/v1/orgs/{org}/audit-settings", s.h.Wrap("putAuditSettings", s.putSettings))
}

// Observe turns a completed mutating API call into an audit entry (ext.CallObserver).
func (s *Service) Observe(ctx context.Context, c ext.APICall) {
	if c.Audited || c.OrgID == uuid.Nil {
		return
	}
	if strings.HasPrefix(c.Path, "/v1/auth/") || strings.HasPrefix(c.Path, "/v1/setup") {
		return // sign-in flows are audited explicitly
	}
	e := store.AuditEntry{OrgID: c.OrgID, Actor: c.Principal.Actor(), ActorID: c.Principal.UserID(), Action: Action(c),
		Target: Target(c), IP: c.IP, UserAgent: c.UserAgent, RequestID: c.RequestID, Status: c.Status, At: c.At}
	diff := map[string]any{"method": c.Method, "path": c.Path}
	if c.ProjectID != nil {
		diff["project_id"] = c.ProjectID.String()
	}
	if sum := Summarize(c.Body); sum != nil {
		diff["request"] = sum
	}
	e.Diff, _ = json.Marshal(diff)
	if err := s.h.Host.Store().AppendAudit(ctx, &e); err != nil {
		s.h.Host.Logger().Warn("audit: append", "action", e.Action, "err", err)
	}
}

var camelRe = regexp.MustCompile(`[A-Z][a-z0-9]*`)

// Action derives "resource.verb" from an operation id ("createDataset" → "dataset.create",
// "rerunFailed" → "run.rerun_failed"); raw routes fall back to "<method> <route>".
func Action(c ext.APICall) string {
	op := strings.TrimPrefix(c.Operation, "ee.")
	if op == "" {
		return strings.ToLower(c.Method) + " " + c.Route
	}
	switch op {
	case "rerunFailed":
		return "run.rerun_failed"
	case "compareRuns":
		return "run.compare"
	}
	i := strings.IndexFunc(op, unicode.IsUpper)
	if i <= 0 {
		return op
	}
	verb, rest := op[:i], camelRe.FindAllString(op[i:], -1)
	for j := range rest {
		rest[j] = strings.ToLower(rest[j])
	}
	return strings.TrimSuffix(strings.Join(rest, "_"), "s") + "." + verb
}

// Target is "<param>:<id>" of the created resource, else of the most specific URL parameter.
func Target(c ext.APICall) string {
	if c.CreatedID != "" {
		res := strings.SplitN(Action(c), ".", 2)[0]
		return res + ":" + c.CreatedID
	}
	best := ""
	if c.Route != "" {
		for _, seg := range strings.Split(c.Route, "/") {
			seg = strings.SplitN(seg, ":", 2)[0]
			if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
				k := strings.Trim(seg, "{}")
				if v, ok := c.Params[k]; ok {
					best = k + ":" + v
				}
			}
		}
	}
	return best
}

var sensitive = []string{"password", "secret", "token", "authorization", "api_key", "apikey", "private", "credential",
	"cookie", "signing", "value", "key"}

func isSensitive(k string) bool {
	k = strings.ToLower(k)
	for _, s := range sensitive {
		if k == s || strings.Contains(k, s) && s != "value" && s != "key" {
			return true
		}
	}
	return false
}

// Summarize returns a redacted, size-bounded view of a JSON request body (nil when empty or not JSON).
func Summarize(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return map[string]any{"bytes": len(body)}
	}
	return summarize(v, 0)
}

func summarize(v any, depth int) any {
	switch x := v.(type) {
	case map[string]any:
		if depth >= 3 {
			return "{…}"
		}
		out := make(map[string]any, len(x))
		n := 0
		for k, val := range x {
			if n == 40 {
				out["…"] = strconv.Itoa(len(x)-n) + " more fields"
				break
			}
			n++
			if isSensitive(k) {
				out[k] = "[redacted]"
				continue
			}
			out[k] = summarize(val, depth+1)
		}
		return out
	case []any:
		if len(x) > 10 || depth >= 3 {
			return strconv.Itoa(len(x)) + " items"
		}
		out := make([]any, len(x))
		for i := range x {
			out[i] = summarize(x[i], depth+1)
		}
		return out
	case string:
		if len(x) > 200 {
			return x[:200] + "…"
		}
		return x
	}
	return v
}

// ---- export ----

func filterFrom(r *http.Request, orgID uuid.UUID) (store.AuditFilter, error) {
	q := r.URL.Query()
	f := store.AuditFilter{OrgID: orgID, Actor: strings.TrimSpace(q.Get("actor")), Action: strings.TrimSpace(q.Get("action")),
		Target: strings.TrimSpace(q.Get("target"))}
	for name, dst := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, httpx.Errorf(http.StatusBadRequest, "bad_request", "%s must be an RFC 3339 timestamp", name)
			}
			*dst = &t
		}
	}
	return f, nil
}

// csvCell neutralizes spreadsheet formula injection.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

const maxExportRows = 200_000

func (s *Service) export(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	f, err := filterFrom(r, orgID)
	if err != nil {
		return err
	}
	ctx := r.Context()
	st := s.h.Host.Store()
	first, err := st.ListAudit(ctx, f, store.ListParams{Limit: store.MaxLimit})
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log-`+s.now().Format("20060102-150405")+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"at", "actor", "actor_id", "action", "target", "status", "ip", "user_agent", "request_id", "details"})
	page, rows := first, 0
	for {
		for _, e := range page.Items {
			actorID := ""
			if e.ActorID != nil {
				actorID = e.ActorID.String()
			}
			status := ""
			if e.Status > 0 {
				status = strconv.Itoa(e.Status)
			}
			_ = cw.Write([]string{e.At.UTC().Format(time.RFC3339), csvCell(e.Actor), actorID, csvCell(e.Action), csvCell(e.Target),
				status, csvCell(e.IP), csvCell(e.UserAgent), e.RequestID, csvCell(string(e.Diff))})
			rows++
		}
		cw.Flush()
		if page.NextCursor == "" || rows >= maxExportRows || ctx.Err() != nil {
			break
		}
		if page, err = st.ListAudit(ctx, f, store.ListParams{Cursor: page.NextCursor, Limit: store.MaxLimit}); err != nil {
			s.h.Host.Logger().Warn("audit: export page", "err", err)
			break
		}
	}
	s.h.Audit(r, orgID, "audit_log.export", "org:"+orgID.String(), map[string]any{"rows": rows, "action": f.Action, "actor": f.Actor})
	return nil
}

// ---- settings & retention ----

// LoadSettings reads the org's audit settings (defaults when unset).
func LoadSettings(ctx context.Context, st store.Store, orgID uuid.UUID) (Settings, error) {
	var out Settings
	raw, err := st.GetOrgSetting(ctx, orgID, settingsKey)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	} else if err != nil {
		return out, err
	}
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	orgID, _, err := s.h.RequireOrg(r, store.RoleAdmin)
	if err != nil {
		return err
	}
	st, err := LoadSettings(r.Context(), s.h.Host.Store(), orgID)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, st)
}

func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	orgID, p, err := s.h.RequireOrg(r, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := httpx.RequireUser(p); err != nil {
		return err
	}
	var in Settings
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.RetentionDays < 0 || in.RetentionDays > 3650 {
		return httpx.Invalid("retention_days must be between 0 (keep forever) and 3650")
	}
	if in.RetentionDays > 0 && in.RetentionDays < 7 {
		return httpx.Invalid("keep the audit log for at least 7 days")
	}
	b, _ := json.Marshal(in)
	if err := s.h.Host.Store().PutOrgSetting(r.Context(), orgID, settingsKey, b); err != nil {
		return err
	}
	s.h.Audit(r, orgID, "audit_settings.update", "org:"+orgID.String(), in)
	return httpx.JSON(w, http.StatusOK, in)
}

// ApplyRetention deletes audit entries past each org's retention. Returns the number deleted.
func (s *Service) ApplyRetention(ctx context.Context) (int, error) {
	all, err := s.h.Host.Store().ListOrgSettings(ctx, settingsKey)
	if err != nil {
		return 0, err
	}
	total := 0
	for orgID, raw := range all {
		var st Settings
		if json.Unmarshal(raw, &st) != nil || st.RetentionDays <= 0 {
			continue
		}
		n, err := s.h.Host.Store().DeleteAuditBefore(ctx, orgID, s.now().AddDate(0, 0, -st.RetentionDays))
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
