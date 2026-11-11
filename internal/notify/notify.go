// Package notify delivers run notifications to an organization's notification channels:
// Slack incoming webhooks (Block Kit summary), generic outgoing webhooks (HMAC-SHA256 signed JSON)
// and SMTP email. Channel config values may reference secrets with {{ secret "NAME" }}.
//
// Events: run.finished (every finished run), run.failed (a gate failed or the run was stopped
// early), run.errored (the run could not complete) and gate.failed (at least one gate failed).
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// Notification event names (NotificationEvent in the API).
const (
	EventRunFinished = "run.finished"
	EventRunFailed   = "run.failed"
	EventRunErrored  = "run.errored"
	EventGateFailed  = "gate.failed"
)

// GateResult is one gate's verdict.
type GateResult struct {
	GateID string   `json:"gate_id"`
	Name   string   `json:"name,omitempty"`
	Text   string   `json:"text,omitempty"`
	Passed bool     `json:"passed"`
	Value  *float64 `json:"value,omitempty"`
	Detail string   `json:"detail,omitempty"`
}

// EvaluatorLine is one evaluator's headline numbers.
type EvaluatorLine struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Mean     *float64 `json:"mean,omitempty"`
	PassRate float64  `json:"pass_rate"`
}

// RunEvent describes a finished run.
type RunEvent struct {
	Run          store.Run       `json:"-"`
	SuiteName    string          `json:"suite_name,omitempty"`
	TargetName   string          `json:"target_name,omitempty"`
	PassRate     float64         `json:"pass_rate"`
	Results      int             `json:"results"`
	DurationMs   float64         `json:"duration_ms"`
	StoppedEarly bool            `json:"stopped_early,omitempty"`
	Gates        []GateResult    `json:"gates,omitempty"`
	Evaluators   []EvaluatorLine `json:"evaluators,omitempty"`
	URL          string          `json:"url,omitempty"`
}

// Events returns the notification events this run triggers.
func (e RunEvent) Events() []string {
	ev := []string{EventRunFinished}
	switch e.Run.Status {
	case store.RunFailed:
		ev = append(ev, EventRunFailed)
	case store.RunErrored:
		ev = append(ev, EventRunErrored)
	}
	if e.Run.GatePassed != nil && !*e.Run.GatePassed {
		ev = append(ev, EventGateFailed)
	}
	return ev
}

// SecretResolverFunc resolves a secret name for an org/project.
type SecretResolverFunc func(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, name string) (string, error)

// Notifier dispatches notifications.
type Notifier struct {
	Store   store.NotificationStore
	Secrets SecretResolverFunc
	HTTP    *http.Client // should carry the SSRF guard; default: 10s timeout client
	Logger  *slog.Logger
	// SendMail overrides SMTP delivery (tests).
	SendMail func(ctx context.Context, cfg ChannelConfig, from string, to []string, msg []byte) error
}

// ChannelConfig is the decoded channel config (NotificationChannelConfig).
type ChannelConfig struct {
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers"`
	SigningSecret string            `json:"signing_secret"`
	Recipients    []string          `json:"recipients"`
	SubjectPrefix string            `json:"subject_prefix"`
	From          string            `json:"from"`
	ProjectIDs    []uuid.UUID       `json:"project_ids"`
}

func (n *Notifier) log() *slog.Logger {
	if n.Logger == nil {
		return slog.Default()
	}
	return n.Logger
}

func (n *Notifier) client() *http.Client {
	if n.HTTP == nil {
		return &http.Client{Timeout: 10 * time.Second}
	}
	return n.HTTP
}

// RunFinished sends ev to every enabled channel of the run's org subscribed to one of its events.
// Delivery failures are logged, never returned (notifications must not fail runs).
func (n *Notifier) RunFinished(ctx context.Context, ev RunEvent) {
	if n == nil || n.Store == nil {
		return
	}
	events := ev.Events()
	var chans []store.NotificationChannel
	p := store.ListParams{Limit: store.MaxLimit}
	for {
		pg, err := n.Store.ListNotificationChannels(ctx, ev.Run.OrgID, p)
		if err != nil {
			n.log().Warn("notify: list channels", "err", err)
			return
		}
		chans = append(chans, pg.Items...)
		if pg.NextCursor == "" {
			break
		}
		p.Cursor = pg.NextCursor
	}
	var wg sync.WaitGroup
	for _, ch := range chans {
		if !ch.Enabled || !subscribed(ch.Events, events) {
			continue
		}
		var cfg ChannelConfig
		_ = json.Unmarshal(ch.Config, &cfg)
		if len(cfg.ProjectIDs) > 0 && !slices.Contains(cfg.ProjectIDs, ev.Run.ProjectID) {
			continue
		}
		wg.Add(1)
		go func(ch store.NotificationChannel) {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			if err := n.Send(sctx, ch, ev); err != nil {
				n.log().Warn("notify: delivery failed", "channel", ch.Name, "kind", ch.Kind, "run_id", ev.Run.ID, "err", err)
			}
		}(ch)
	}
	wg.Wait()
}

// subscribed reports whether a channel listens to one of events. A channel with no events
// configured gets failures only (gate.failed, run.failed, run.errored).
func subscribed(chEvents, events []string) bool {
	if len(chEvents) == 0 {
		chEvents = []string{EventGateFailed, EventRunFailed, EventRunErrored}
	}
	for _, e := range events {
		if slices.Contains(chEvents, e) {
			return true
		}
	}
	return false
}

var secretRef = regexp.MustCompile(`\{\{\s*secret\s+"([^"]+)"\s*\}\}`)

func (n *Notifier) resolve(ctx context.Context, ch store.NotificationChannel, projectID *uuid.UUID, s string) (string, error) {
	var firstErr error
	out := secretRef.ReplaceAllStringFunc(s, func(m string) string {
		name := secretRef.FindStringSubmatch(m)[1]
		if n.Secrets == nil {
			firstErr = fmt.Errorf("secret %q: no resolver", name)
			return ""
		}
		v, err := n.Secrets(ctx, ch.OrgID, projectID, name)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	})
	return out, firstErr
}

// Send delivers ev to one channel.
func (n *Notifier) Send(ctx context.Context, ch store.NotificationChannel, ev RunEvent) error {
	var cfg ChannelConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("channel config: %w", err)
	}
	pid := &ev.Run.ProjectID
	var err error
	if cfg.URL, err = n.resolve(ctx, ch, pid, cfg.URL); err != nil {
		return err
	}
	if cfg.From, err = n.resolve(ctx, ch, pid, cfg.From); err != nil {
		return err
	}
	for k, v := range cfg.Headers {
		if cfg.Headers[k], err = n.resolve(ctx, ch, pid, v); err != nil {
			return err
		}
	}
	switch ch.Kind {
	case "slack":
		return n.postJSON(ctx, cfg.URL, nil, SlackMessage(ev), "")
	case "webhook":
		secret := ""
		if cfg.SigningSecret != "" {
			if n.Secrets == nil {
				return errors.New("signing secret: no resolver")
			}
			if secret, err = n.Secrets(ctx, ch.OrgID, pid, cfg.SigningSecret); err != nil {
				return err
			}
		}
		return n.postJSON(ctx, cfg.URL, cfg.Headers, WebhookPayload(ev), secret)
	case "email":
		return n.email(ctx, cfg, ev)
	}
	return fmt.Errorf("unknown channel kind %q", ch.Kind)
}

// Sign computes the X-Airval-Signature value for a body: sha256=hex(hmac(secret, ts + "." + body)).
func Sign(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts))
	m.Write([]byte("."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

func (n *Notifier) postJSON(ctx context.Context, u string, headers map[string]string, payload any, secret string) error {
	if u == "" {
		return errors.New("url is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "airval-notify/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set("X-Airval-Timestamp", ts)
		req.Header.Set("X-Airval-Signature", Sign(secret, ts, body))
	}
	resp, err := n.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// ---- message rendering ----

func statusWord(ev RunEvent) string {
	switch ev.Run.Status {
	case store.RunSucceeded:
		return "passed"
	case store.RunFailed:
		if ev.StoppedEarly {
			return "failed (stopped early)"
		}
		return "failed"
	case store.RunErrored:
		return "errored"
	case store.RunCanceled:
		return "was canceled"
	}
	return ev.Run.Status
}

func title(ev RunEvent) string {
	name := ev.Run.Name
	if name == "" {
		name = ev.SuiteName
	}
	return fmt.Sprintf("Run %s %s", name, statusWord(ev))
}

func pct(v float64) string { return strconv.FormatFloat(v*100, 'f', 1, 64) + "%" }

func summaryLine(ev RunEvent) string {
	r := ev.Run
	parts := []string{fmt.Sprintf("Pass rate %s", pct(ev.PassRate)),
		fmt.Sprintf("%d results (%d failed, %d errored)", ev.Results, r.Failed, r.Errored)}
	if ev.DurationMs > 0 {
		parts = append(parts, (time.Duration(ev.DurationMs) * time.Millisecond).Round(100*time.Millisecond).String())
	}
	if r.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", r.CostUSD))
	}
	return strings.Join(parts, " · ")
}

func gateLine(g GateResult) string {
	mark := "PASS"
	if !g.Passed {
		mark = "FAIL"
	}
	label := g.Name
	if label == "" {
		label = g.Text
	} else if g.Text != "" {
		label += " (" + g.Text + ")"
	}
	if g.Detail != "" {
		label += " — " + g.Detail
	}
	return mark + "  " + label
}

// SlackMessage renders a Block Kit message.
func SlackMessage(ev RunEvent) map[string]any {
	blocks := []any{
		map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": truncate(title(ev), 150)}},
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": summaryLine(ev)}},
	}
	var ctxParts []string
	if ev.SuiteName != "" {
		ctxParts = append(ctxParts, "*Suite* "+ev.SuiteName)
	}
	if ev.TargetName != "" {
		ctxParts = append(ctxParts, "*Target* "+ev.TargetName)
	}
	for _, k := range []string{"branch", "git_sha"} {
		if v := ev.Run.Labels[k]; v != "" {
			ctxParts = append(ctxParts, "*"+k+"* `"+v+"`")
		}
	}
	if len(ctxParts) > 0 {
		blocks = append(blocks, map[string]any{"type": "context", "elements": []any{
			map[string]any{"type": "mrkdwn", "text": strings.Join(ctxParts, "  ·  ")}}})
	}
	if len(ev.Gates) > 0 {
		lines := make([]string, len(ev.Gates))
		for i, g := range ev.Gates {
			lines[i] = gateLine(g)
		}
		blocks = append(blocks, map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn",
			"text": "*Gates*\n```" + truncate(strings.Join(lines, "\n"), 2800) + "```"}})
	}
	if len(ev.Evaluators) > 0 {
		var fields []any
		for i, e := range ev.Evaluators {
			if i == 10 {
				break
			}
			v := "pass " + pct(e.PassRate)
			if e.Mean != nil {
				v = fmt.Sprintf("mean %.3f · %s", *e.Mean, v)
			}
			fields = append(fields, map[string]any{"type": "mrkdwn", "text": "*" + e.Name + "*\n" + v})
		}
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	if ev.URL != "" {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": []any{map[string]any{
			"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open run"}, "url": ev.URL}}})
	}
	return map[string]any{"text": title(ev) + " — " + summaryLine(ev), "blocks": blocks}
}

// WebhookPayload renders the generic webhook body.
func WebhookPayload(ev RunEvent) map[string]any {
	r := ev.Run
	return map[string]any{
		"events": ev.Events(),
		"run": map[string]any{
			"id": r.ID, "name": r.Name, "project_id": r.ProjectID, "suite_id": r.SuiteID, "target_id": r.TargetID,
			"status": r.Status, "gate_passed": r.GatePassed, "total": r.Total, "completed": r.Completed,
			"failed": r.Failed, "errored": r.Errored, "cost_usd": r.CostUSD, "labels": r.Labels,
			"trigger_kind": r.TriggerKind, "trigger_ref": r.TriggerRef, "started_at": r.StartedAt, "finished_at": r.FinishedAt,
		},
		"summary": ev,
		"url":     ev.URL,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ---- email ----

func (n *Notifier) email(ctx context.Context, cfg ChannelConfig, ev RunEvent) error {
	if len(cfg.Recipients) == 0 {
		return errors.New("recipients are required")
	}
	from := cfg.From
	if from == "" {
		from = "airval@localhost"
	}
	subject := title(ev)
	if cfg.SubjectPrefix != "" {
		subject = cfg.SubjectPrefix + " " + subject
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\r\n\r\n%s\r\n", title(ev), summaryLine(ev))
	if ev.SuiteName != "" {
		fmt.Fprintf(&b, "Suite: %s\r\n", ev.SuiteName)
	}
	if ev.TargetName != "" {
		fmt.Fprintf(&b, "Target: %s\r\n", ev.TargetName)
	}
	if len(ev.Gates) > 0 {
		b.WriteString("\r\nGates:\r\n")
		for _, g := range ev.Gates {
			b.WriteString("  " + gateLine(g) + "\r\n")
		}
	}
	if len(ev.Evaluators) > 0 {
		b.WriteString("\r\nEvaluators:\r\n")
		for _, e := range ev.Evaluators {
			m := "-"
			if e.Mean != nil {
				m = fmt.Sprintf("%.3f", *e.Mean)
			}
			fmt.Fprintf(&b, "  %-30s mean %s  pass %s\r\n", e.Name, m, pct(e.PassRate))
		}
	}
	if ev.URL != "" {
		fmt.Fprintf(&b, "\r\nOpen the run: %s\r\n", ev.URL)
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nDate: %s\r\n\r\n%s",
		from, strings.Join(cfg.Recipients, ", "), mimeHeader(subject), time.Now().UTC().Format(time.RFC1123Z), b.String())
	if n.SendMail != nil {
		return n.SendMail(ctx, cfg, from, cfg.Recipients, []byte(msg))
	}
	return sendSMTP(ctx, cfg.URL, from, cfg.Recipients, []byte(msg))
}

func mimeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?utf-8?q?" + strings.NewReplacer("=", "=3D", "?", "=3F", "_", "=5F", " ", "_").Replace(s) + "?="
		}
	}
	return s
}

// sendSMTP delivers via smtp://[user:pass@]host[:port] (STARTTLS when offered) or smtps:// (implicit TLS).
func sendSMTP(ctx context.Context, rawURL, from string, to []string, msg []byte) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
		return errors.New("email channels need url = smtp://user:pass@host:port or smtps://…")
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = map[string]string{"smtp": "587", "smtps": "465"}[u.Scheme]
	}
	addr := net.JoinHostPort(host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if u.Scheme == "smtps" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if u.Scheme == "smtp" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if u.User != nil {
		pw, _ := u.User.Password()
		if err := c.Auth(smtp.PlainAuth("", u.User.Username(), pw, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
