package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/evalcore"
	"github.com/prat31/airval/internal/store"
)

// Message is a free-form notification (e.g. "Send test" from the channel settings page).
type Message struct {
	Event  string
	Title  string
	Text   string
	URL    string
	Fields map[string]string
	At     time.Time
}

// SendMessage delivers a free-form message through ch, resolving config secrets with sec.
func (n *Notifier) SendMessage(ctx context.Context, ch store.NotificationChannel, sec evalcore.SecretResolver, m Message) error {
	cp := *n
	cp.Secrets = func(_ context.Context, _ uuid.UUID, _ *uuid.UUID, name string) (string, error) {
		if sec == nil {
			return "", fmt.Errorf("secret %q: no resolver", name)
		}
		return sec.Resolve(name)
	}
	var cfg ChannelConfig
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		return fmt.Errorf("channel config: %w", err)
	}
	var err error
	if cfg.URL, err = cp.resolve(ctx, ch, nil, cfg.URL); err != nil {
		return err
	}
	if cfg.From, err = cp.resolve(ctx, ch, nil, cfg.From); err != nil {
		return err
	}
	for k, v := range cfg.Headers {
		if cfg.Headers[k], err = cp.resolve(ctx, ch, nil, v); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(m.Fields))
	for k := range m.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	switch ch.Kind {
	case "slack":
		blocks := []any{
			map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": truncate(m.Title, 150)}},
		}
		if m.Text != "" {
			blocks = append(blocks, map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": m.Text}})
		}
		if len(keys) > 0 {
			var fields []any
			for _, k := range keys[:min(len(keys), 10)] {
				fields = append(fields, map[string]any{"type": "mrkdwn", "text": "*" + k + "*\n" + m.Fields[k]})
			}
			blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
		}
		if m.URL != "" {
			blocks = append(blocks, map[string]any{"type": "actions", "elements": []any{map[string]any{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open airval"}, "url": m.URL}}})
		}
		return cp.postJSON(ctx, cfg.URL, nil, map[string]any{"text": m.Title, "blocks": blocks}, "")
	case "webhook":
		secret := ""
		if cfg.SigningSecret != "" {
			if secret, err = cp.Secrets(ctx, ch.OrgID, nil, cfg.SigningSecret); err != nil {
				return err
			}
		}
		at := m.At
		if at.IsZero() {
			at = time.Now().UTC()
		}
		return cp.postJSON(ctx, cfg.URL, cfg.Headers, map[string]any{"events": []string{m.Event}, "title": m.Title,
			"text": m.Text, "url": m.URL, "fields": m.Fields, "at": at}, secret)
	case "email":
		subject := m.Title
		if cfg.SubjectPrefix != "" {
			subject = cfg.SubjectPrefix + " " + subject
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\r\n\r\n%s\r\n", m.Title, m.Text)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s: %s\r\n", k, m.Fields[k])
		}
		if m.URL != "" {
			fmt.Fprintf(&b, "\r\n%s\r\n", m.URL)
		}
		return cp.mail(ctx, cfg, subject, b.String())
	}
	return fmt.Errorf("unknown channel kind %q", ch.Kind)
}
