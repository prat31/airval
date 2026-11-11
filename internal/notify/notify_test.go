package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

type fakeChannels struct{ chans []store.NotificationChannel }

func (f *fakeChannels) CreateNotificationChannel(context.Context, *store.NotificationChannel) error {
	return nil
}
func (f *fakeChannels) GetNotificationChannel(context.Context, uuid.UUID) (store.NotificationChannel, error) {
	return store.NotificationChannel{}, store.ErrNotFound
}
func (f *fakeChannels) ListNotificationChannels(context.Context, uuid.UUID, store.ListParams) (store.Page[store.NotificationChannel], error) {
	return store.Page[store.NotificationChannel]{Items: f.chans}, nil
}
func (f *fakeChannels) UpdateNotificationChannel(context.Context, *store.NotificationChannel) error {
	return nil
}
func (f *fakeChannels) DeleteNotificationChannel(context.Context, uuid.UUID) error { return nil }

func TestDispatch(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]byte{}
	sigs := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = b
		sigs[r.URL.Path] = r.Header.Get("X-Airval-Signature") + "|" + r.Header.Get("X-Airval-Timestamp")
		mu.Unlock()
	}))
	defer srv.Close()
	org, proj := uuid.New(), uuid.New()
	cfg := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	var mails [][]byte
	n := &Notifier{
		Store: &fakeChannels{chans: []store.NotificationChannel{
			{OrgID: org, Name: "slack", Kind: "slack", Enabled: true, Events: []string{EventGateFailed},
				Config: cfg(map[string]any{"url": srv.URL + `/{{ secret "SLACK_PATH" }}`})},
			{OrgID: org, Name: "hook", Kind: "webhook", Enabled: true, Events: []string{EventRunFinished},
				Config: cfg(map[string]any{"url": srv.URL + "/hook", "signing_secret": "HOOK"})},
			{OrgID: org, Name: "other-project", Kind: "webhook", Enabled: true, Events: []string{EventRunFinished},
				Config: cfg(map[string]any{"url": srv.URL + "/never", "project_ids": []string{uuid.NewString()}})},
			{OrgID: org, Name: "success-only", Kind: "webhook", Enabled: true, Events: []string{"run.succeeded"},
				Config: cfg(map[string]any{"url": srv.URL + "/never2"})},
			{OrgID: org, Name: "mail", Kind: "email", Enabled: true,
				Config: cfg(map[string]any{"url": "smtp://x", "recipients": []string{"a@b.c"}, "subject_prefix": "[airval]"})},
		}},
		Secrets: func(_ context.Context, _ uuid.UUID, _ *uuid.UUID, name string) (string, error) {
			return map[string]string{"SLACK_PATH": "slack", "HOOK": "s3cret"}[name], nil
		},
		SendMail: func(_ context.Context, _ ChannelConfig, _ string, _ []string, msg []byte) error {
			mails = append(mails, msg)
			return nil
		},
	}
	f := false
	mean := 0.5
	ev := RunEvent{Run: store.Run{ID: uuid.New(), OrgID: org, ProjectID: proj, Name: "nightly", Status: store.RunFailed,
		GatePassed: &f, Labels: map[string]string{"branch": "main"}}, PassRate: 0.5, Results: 10,
		Gates:      []GateResult{{GateID: "g", Name: "quality", Text: "mean(x) >= 0.8", Passed: false}},
		Evaluators: []EvaluatorLine{{ID: "x", Name: "x", Mean: &mean, PassRate: 0.5}}, URL: "http://airval/runs/1"}
	n.RunFinished(context.Background(), ev)

	if !strings.Contains(string(got["/slack"]), `"blocks"`) || !strings.Contains(string(got["/slack"]), "quality") {
		t.Fatalf("slack body: %s", got["/slack"])
	}
	parts := strings.Split(sigs["/hook"], "|")
	if want := Sign("s3cret", parts[1], got["/hook"]); parts[0] != want {
		t.Fatalf("bad signature %q want %q", parts[0], want)
	}
	if _, ok := got["/never"]; ok {
		t.Fatal("project filter ignored")
	}
	if _, ok := got["/never2"]; ok {
		t.Fatal("event filter ignored")
	}
	if len(mails) != 1 || !strings.Contains(string(mails[0]), "Subject: [airval] Run nightly failed") {
		t.Fatalf("mail: %q", mails)
	}
}
