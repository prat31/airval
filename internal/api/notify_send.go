package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/secrets"
	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/targets"
)

// ChannelMessage is a notification handed to a ChannelSender.
type ChannelMessage struct {
	Event  string            `json:"event"` // run.finished | run.failed | run.errored | gate.failed | test
	Title  string            `json:"title"`
	Text   string            `json:"text"`
	URL    string            `json:"url,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
	At     time.Time         `json:"at"`
}

// ChannelSender delivers a message through a notification channel. Secrets resolves
// `{{ secret "NAME" }}` references of the channel config (org-level secrets).
//
// The run engine's notifier (internal/notify) plugs in by setting Deps.SendNotification; when
// that is nil the Server uses the minimal built-in senders below (slack and webhook).
type ChannelSender func(ctx context.Context, ch store.NotificationChannel, secrets evalcore.SecretResolver, msg ChannelMessage) error

var secretRef = regexp.MustCompile(`\{\{\s*secret\s+"([^"]+)"\s*\}\}`)

func expandSecrets(s string, r evalcore.SecretResolver) (string, error) {
	var firstErr error
	out := secretRef.ReplaceAllStringFunc(s, func(m string) string {
		name := secretRef.FindStringSubmatch(m)[1]
		v, err := r.Resolve(name)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	})
	return out, firstErr
}

// sendNotification delivers msg through the configured sender.
func (s *Server) sendNotification(ctx context.Context, ch store.NotificationChannel, msg ChannelMessage) error {
	res := &safeResolver{r: &secrets.Resolver{Ctx: ctx, Store: s.Store, Keyring: s.Keyring, OrgID: ch.OrgID}}
	if s.SendNotification != nil {
		return s.SendNotification(ctx, ch, res, msg)
	}
	return s.builtinSend(ctx, ch, res, msg)
}

func (s *Server) builtinSend(ctx context.Context, ch store.NotificationChannel, sec evalcore.SecretResolver, msg ChannelMessage) error {
	var cfg struct {
		URL           string            `json:"url"`
		Headers       map[string]string `json:"headers"`
		SigningSecret string            `json:"signing_secret"`
	}
	if err := json.Unmarshal(rawOr(ch.Config, "{}"), &cfg); err != nil {
		return fieldError("config", "invalid channel config: %v", err)
	}
	switch ch.Kind {
	case "slack", "webhook":
	case "email":
		return errorf(http.StatusNotImplemented, "not_implemented", "email delivery needs SMTP settings, which this server does not have configured")
	default:
		return fieldError("kind", "unknown channel kind %q", ch.Kind)
	}
	dest, err := expandSecrets(cfg.URL, sec)
	if err != nil {
		return fieldError("config.url", "%v", err)
	}
	u, err := url.Parse(dest)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fieldError("config.url", "url must be an absolute http(s) URL")
	}
	var body []byte
	if ch.Kind == "slack" {
		text := "*" + msg.Title + "*\n" + msg.Text
		if msg.URL != "" {
			text += "\n<" + msg.URL + "|Open in airval>"
		}
		body = mustJSON(map[string]string{"text": text})
	} else {
		body = mustJSON(msg)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, bytes.NewReader(body))
	if err != nil {
		return fieldError("config.url", "%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "airval-notifier")
	for k, v := range cfg.Headers {
		if v, err = expandSecrets(v, sec); err != nil {
			return fieldError("config.headers", "%v", err)
		}
		req.Header.Set(k, v)
	}
	if ch.Kind == "webhook" && cfg.SigningSecret != "" {
		key, err := sec.Resolve(cfg.SigningSecret)
		if err != nil {
			return fieldError("config.signing_secret", "%v", err)
		}
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		req.Header.Set("X-Airval-Timestamp", ts)
		req.Header.Set("X-Airval-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	host := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "https" {
			port = "443"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	client := s.targetDeps().Clients.Client(host, targets.ClientOptions{Concurrency: 2, ResponseHeaderTimeout: 15 * time.Second})
	resp, err := client.Do(req)
	if err != nil {
		return errorf(http.StatusBadGateway, "delivery_failed", "could not reach %s: %v", u.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return errorf(http.StatusBadGateway, "delivery_failed", "%s answered %d: %s", u.Host, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

func sampleMessage(baseURL, channel string) ChannelMessage {
	return ChannelMessage{Event: "test", Title: "airval test notification",
		Text: fmt.Sprintf("This is a test message for the channel %q. If you can read it, delivery works.", channel),
		URL:  baseURL, Fields: map[string]string{"suite": "Smoke test", "pass_rate": "92%", "gate": "passed"}, At: time.Now().UTC()}
}
