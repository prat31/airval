package targets

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestJSONPath(t *testing.T) {
	var doc any
	_ = json.Unmarshal([]byte(`{
	  "a": {"b": [{"c": 1}, {"c": 2}, {"c": 3}]},
	  "items": [{"type":"text","text":"hello"},{"type":"tool","text":"x"},{"type":"text","text":"world"}],
	  "weird key": 7, "n": null,
	  "deep": {"x": {"id": "d1"}, "y": [{"id": "d2"}]}
	}`), &doc)
	tests := []struct {
		path string
		want string // JSON of Eval result
	}{
		{"$.a.b[0].c", `[1]`},
		{"$.a.b[-1].c", `[3]`},
		{"$.a.b[*].c", `[1,2,3]`},
		{"a.b[1].c", `[2]`},
		{"$.items[*].text", `["hello","x","world"]`},
		{"$.items[?(@.type=='text')].text", `["hello","world"]`},
		{"$.items[?(@.type!='text')].text", `["x"]`},
		{"$.a.b[?(@.c>1)].c", `[2,3]`},
		{"$['weird key']", `[7]`},
		{"$.a.b[1:]", `[{"c":2},{"c":3}]`},
		{"$.a.b[:2].c", `[1,2]`},
		{"$..c", `[1,2,3]`},
		{"$..id", `["d1","d2"]`},
		{"$.missing.deep", `null`},
		{"$.a.b[9]", `null`},
		{"$.n", `[null]`},
		{"$.a.*", `[[{"c":1},{"c":2},{"c":3}]]`},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			p, err := CompilePath(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(p.Eval(doc))
			if string(got) != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, bad := range []string{"", "$.", "$.a[", "$.a[x]", "$[?(@.a=='x)]"} {
		if _, err := CompilePath(bad); err == nil {
			t.Errorf("expected error compiling %q", bad)
		}
	}
	p := MustCompilePath("$.a.b[0].c")
	if v, ok := p.First(doc); !ok || v.(float64) != 1 {
		t.Fatalf("First = %v %v", v, ok)
	}
	if allocs := testing.AllocsPerRun(100, func() { p.First(doc) }); allocs != 0 {
		t.Errorf("single-path First allocates %v", allocs)
	}
}

func TestTemplateRenderAndRedaction(t *testing.T) {
	tpl, err := CompileTemplate("t", `{"q": {{ json .input.q }}, "k": "{{ secret "KEY" }}", "m": "{{ default "dflt" .example.metadata.who | upper }}", "t": {{ .trial }}, "x": "{{ .input.nope }}", "p": "{{ "KEY" | secret }}"}`)
	if err != nil {
		t.Fatal(err)
	}
	env := newEnv(mapSecrets{"KEY": "s3cr3t-value"})
	tc := &turnCtx{ex: ex("hi \"there\""), opts: InvokeOptions{Trial: 2}}
	tc.ex.Input["q"] = "hi \"there\""
	out, err := tpl.Render(tc.data(env))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"q": "hi \"there\"", "k": "s3cr3t-value", "m": "DFLT", "t": 2, "x": "", "p": "s3cr3t-value"}`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
	if got := env.redactor()(out); strings.Contains(got, "s3cr3t-value") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("not redacted: %s", got)
	}
	// Missing secret is an error, not an empty string.
	if _, err := tpl.Render(tc.data(newEnv(mapSecrets{}))); err == nil {
		t.Fatal("expected missing-secret error")
	}
	// Static templates skip execution.
	st, _ := CompileTemplate("s", "plain")
	if !st.IsStatic() {
		t.Fatal("expected static")
	}
	if _, err := CompileTemplate("bad", "{{ .a "); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTemplateMessagesAndSession(t *testing.T) {
	tpl, _ := CompileTemplate("t", `{{ range $i, $m := .messages }}{{ if $i }}|{{ end }}{{ $m.role }}={{ $m.content }}{{ end }} last={{ .message }} s={{ .session }} turn={{ .turn }} {{ secret "K" }}`)
	tc := &turnCtx{messages: msgs("user", "a", "assistant", "b", "user", "c"), session: "S1", turn: 1}
	out, err := tpl.Render(tc.data(newEnv(mapSecrets{"K": "v"})))
	if err != nil {
		t.Fatal(err)
	}
	if out != "user=a|assistant=b|user=c last=c s=S1 turn=1 v" {
		t.Fatalf("got %q", out)
	}
}

func TestSSRFGuard(t *testing.T) {
	deny := func(g *SSRFGuard, ip string) bool {
		return g.CheckIP(netip.MustParseAddr(ip)) != nil
	}
	def, _ := NewSSRFGuard(NetworkPolicy{})
	strict, _ := NewSSRFGuard(NetworkPolicy{BlockPrivate: true, AllowCIDRs: nil})
	custom, _ := NewSSRFGuard(NetworkPolicy{DenyCIDRs: []string{"203.0.113.0/24"}})
	allow, _ := NewSSRFGuard(NetworkPolicy{AllowCIDRs: []string{"10.1.0.0/16", "169.254.169.254"}})
	privAllow, _ := NewSSRFGuard(NetworkPolicy{BlockPrivate: true, DenyCIDRs: nil, AllowCIDRs: nil})
	_ = privAllow
	tests := []struct {
		name string
		g    *SSRFGuard
		ip   string
		deny bool
	}{
		{"metadata v4", def, "169.254.169.254", true},
		{"link-local v4", def, "169.254.1.1", true},
		{"metadata v4-mapped v6", def, "::ffff:169.254.169.254", true},
		{"aws v6 metadata", def, "fd00:ec2::254", true},
		{"v6 link-local", def, "fe80::1", true},
		{"alibaba", def, "100.100.100.200", true},
		{"public ok", def, "93.184.216.34", false},
		{"loopback ok by default", def, "127.0.0.1", false},
		{"private ok by default", def, "10.0.0.5", false},
		{"loopback blocked strict", strict, "127.0.0.1", true},
		{"rfc1918 blocked strict", strict, "192.168.1.1", true},
		{"ula blocked strict", strict, "fd12::1", true},
		{"cgnat blocked strict", strict, "100.64.0.1", true},
		{"public ok strict", strict, "8.8.8.8", false},
		{"custom deny", custom, "203.0.113.9", true},
		{"custom other ok", custom, "203.0.114.9", false},
		{"allowlist hit", allow, "10.1.2.3", false},
		{"allowlist miss", allow, "8.8.8.8", true},
		{"allowlist cannot allow metadata", allow, "169.254.169.254", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := deny(tc.g, tc.ip); got != tc.deny {
				t.Fatalf("deny(%s)=%v want %v", tc.ip, got, tc.deny)
			}
		})
	}
	if _, err := NewSSRFGuard(NetworkPolicy{DenyCIDRs: []string{"nope"}}); err == nil {
		t.Fatal("expected CIDR error")
	}
}

func TestSSRFGuardBlocksDial(t *testing.T) {
	srv := newMock(t, mockCfg())
	guard, _ := NewSSRFGuard(NetworkPolicy{BlockPrivate: true}) // httptest listens on 127.0.0.1
	a, err := New("http", mustJSON(HTTPConfig{Method: "GET", URL: srv.URL + "/echo"}), Deps{Clients: NewClientFactory(guard)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Invoke(context.Background(), ex("x"), InvokeOptions{})
	var pe ErrPermanent
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "ssrf guard") {
		t.Fatalf("want permanent SSRF error, got %v", err)
	}
	// Metadata IP literal is blocked even with the default guard (and never connects).
	a2 := mustNew(t, "http", HTTPConfig{Method: "GET", URL: "http://169.254.169.254/latest/meta-data/", HTTPCommon: HTTPCommon{TimeoutMS: 500}})
	_, err = a2.Invoke(context.Background(), ex("x"), InvokeOptions{})
	if Classify(err) != ClassPermanent || !strings.Contains(err.Error(), "ssrf guard") {
		t.Fatalf("metadata: %v", err)
	}
}

func TestClassifyAndRetryAfter(t *testing.T) {
	if Classify(nil) != ClassNone || Classify(errors.New("x")) != ClassUnknown || Classify(context.Canceled) != ClassCanceled {
		t.Fatal("classify basics")
	}
	e := ErrRateLimited{RetryAfter: 3 * time.Second, Err: errors.New("x")}
	if d, ok := RetryAfter(e); !ok || d != 3*time.Second {
		t.Fatal("retry after")
	}
	h := map[string][]string{"Retry-After": {"7"}}
	if parseRetryAfter(h) != 7*time.Second {
		t.Fatal("seconds")
	}
	h = map[string][]string{"Retry-After": {time.Now().Add(10 * time.Second).UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")}}
	if d := parseRetryAfter(h); d < 5*time.Second || d > 11*time.Second {
		t.Fatalf("http-date: %v", d)
	}
	if parseRetryAfter(map[string][]string{"Retry-After-Ms": {"250"}}) != 250*time.Millisecond {
		t.Fatal("ms")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		kind, cfg, wantErr string
	}{
		{"http", `{"url":"http://x"}`, ""},
		{"http", `{}`, "url is required"},
		{"http", `{"url":"http://x","bogus":1}`, "unknown field"},
		{"http", `{"url":"http://x","response":{"output":"$.a["}}`, "response.output"},
		{"http", `{"url":"http://x","body":"{{ .a "}`, "template"},
		{"http", `{"url":"http://x","stream":{"mode":"ws","delta_path":"$.a"}}`, "stream.mode"},
		{"http", `{"url":"http://x","method":"TRACE"}`, "unsupported method"},
		{"agent", `{"url":"http://x"}`, "trace"},
		{"agent", `{"url":"http://x","response":{"trace":{"path":"$.steps[*]"}}}`, ""},
		{"openai", `{"model":"gpt-4o"}`, ""},
		{"openai", `{}`, "model is required"},
		{"openai", `{"model":"m","tools":{"a":1}}`, "tools must be a JSON array"},
		{"openai", `{"model":"m","base_url":"ftp://x"}`, "base_url"},
		{"anthropic", `{"model":"claude-3-5-haiku-latest"}`, ""},
		{"bedrock", `{"model":"m","region":"us-east-1"}`, "access_key_id"},
		{"bedrock", `{"model":"m","region":"us-east-1","access_key_id":"a","secret_access_key":"b"}`, ""},
		{"vertex", `{"model":"gemini-2.0-flash"}`, "api_key or access_token"},
		{"vertex", `{"model":"gemini-2.0-flash","api_key":"k"}`, ""},
		{"nope", `{}`, "unknown target kind"},
	}
	for _, tc := range tests {
		t.Run(tc.kind+"/"+tc.cfg, func(t *testing.T) {
			err := ValidateConfig(tc.kind, json.RawMessage(tc.cfg))
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLookupPrice(t *testing.T) {
	tests := []struct {
		model string
		in    float64
		ok    bool
	}{
		{"gpt-4o", 2.5, true},
		{"gpt-4o-mini", 0.15, true},
		{"gpt-4o-2024-08-06", 2.5, true},
		{"gpt-4.1-mini", 0.4, true},
		{"us.anthropic.claude-3-5-sonnet-20241022-v2:0", 3, true},
		{"anthropic.claude-3-haiku-20240307-v1:0", 0.25, true},
		{"claude-sonnet-4-5-20250929", 3, true},
		{"gemini-2.0-flash", 0.1, true},
		{"llama3:8b", 0, false},
	}
	for _, tc := range tests {
		in, _, ok := lookupPrice(tc.model)
		if ok != tc.ok || in != tc.in {
			t.Errorf("%s: got %v,%v want %v,%v", tc.model, in, ok, tc.in, tc.ok)
		}
	}
}
