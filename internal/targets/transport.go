package targets

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout   = 60 * time.Second
	defaultMaxBody   = 10 << 20
	rawLimit         = 64 << 10 // bytes of raw response kept in Response.Raw
	errBodyLimit     = 4 << 10
	defaultPoolConns = 64
)

// HTTPCommon holds the transport settings shared by every HTTP-based kind.
type HTTPCommon struct {
	// TimeoutMS bounds the whole call including streaming (default 60000).
	TimeoutMS int `json:"timeout_ms,omitempty"`
	// MaxResponseBytes caps the response body (default 10 MiB).
	MaxResponseBytes int64 `json:"max_response_bytes,omitempty"`
	// MaxConcurrency sizes the per-host idle connection pool; set it to the
	// target's concurrency (default 64).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// TLS configures a custom CA and/or mutual TLS.
	TLS *TLSConfig `json:"tls,omitempty"`
}

// TLSConfig configures TLS. Every PEM field is a template so it can reference
// secrets: {{ secret "CLIENT_CERT" }}.
type TLSConfig struct {
	CACert             string `json:"ca_cert,omitempty"`
	ClientCert         string `json:"client_cert,omitempty"`
	ClientKey          string `json:"client_key,omitempty"`
	ServerName         string `json:"server_name,omitempty"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

func (c *HTTPCommon) validate() error {
	if c.TimeoutMS < 0 || c.MaxResponseBytes < 0 || c.MaxConcurrency < 0 {
		return fmt.Errorf("timeout_ms, max_response_bytes and max_concurrency must be >= 0")
	}
	if c.TLS != nil && (c.TLS.ClientCert == "") != (c.TLS.ClientKey == "") {
		return fmt.Errorf("tls.client_cert and tls.client_key must be set together")
	}
	return nil
}

type tlsTemplates struct {
	ca, cert, key *Template
	serverName    string
	insecure      bool
}

// transport executes HTTP calls for one target: pooled client, timeouts, size
// limits, status classification and streaming.
type transport struct {
	clients *ClientFactory
	timeout time.Duration
	maxBody int64
	conc    int
	tls     *tlsTemplates

	tlsMu    sync.Mutex
	tlsCache map[string]*tls.Config
}

func newTransport(deps Deps, hc HTTPCommon) (*transport, error) {
	t := &transport{
		clients: deps.Clients,
		timeout: time.Duration(hc.TimeoutMS) * time.Millisecond,
		maxBody: hc.MaxResponseBytes,
		conc:    hc.MaxConcurrency,
	}
	if t.clients == nil {
		t.clients = DefaultClients()
	}
	if t.timeout == 0 {
		t.timeout = defaultTimeout
	}
	if t.maxBody == 0 {
		t.maxBody = defaultMaxBody
	}
	if t.conc == 0 {
		t.conc = deps.DefaultConcurrency
	}
	if t.conc == 0 {
		t.conc = defaultPoolConns
	}
	if hc.TLS != nil {
		tt := &tlsTemplates{serverName: hc.TLS.ServerName, insecure: hc.TLS.InsecureSkipVerify}
		var err error
		if tt.ca, err = CompileTemplate("tls.ca_cert", hc.TLS.CACert); err != nil {
			return nil, err
		}
		if tt.cert, err = CompileTemplate("tls.client_cert", hc.TLS.ClientCert); err != nil {
			return nil, err
		}
		if tt.key, err = CompileTemplate("tls.client_key", hc.TLS.ClientKey); err != nil {
			return nil, err
		}
		t.tls = tt
		t.tlsCache = map[string]*tls.Config{}
	}
	return t, nil
}

// tlsConfig renders the TLS templates and returns a cached *tls.Config + cache key.
func (t *transport) tlsConfig(env *renderEnv) (*tls.Config, string, error) {
	if t.tls == nil {
		return nil, "", nil
	}
	root := map[string]any{envKey: env}
	ca, err := t.tls.ca.Render(root)
	if err != nil {
		return nil, "", permanent("%v", err)
	}
	cert, err := t.tls.cert.Render(root)
	if err != nil {
		return nil, "", permanent("%v", err)
	}
	key, err := t.tls.key.Render(root)
	if err != nil {
		return nil, "", permanent("%v", err)
	}
	h := sha256.New()
	for _, s := range []string{ca, cert, key, t.tls.serverName} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	k := hex.EncodeToString(h.Sum(nil)[:12])
	t.tlsMu.Lock()
	defer t.tlsMu.Unlock()
	if c, ok := t.tlsCache[k]; ok {
		return c, k, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: t.tls.serverName, InsecureSkipVerify: t.tls.insecure}
	if ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, "", permanent("tls.ca_cert: no valid PEM certificates")
		}
		cfg.RootCAs = pool
	}
	if cert != "" {
		pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
		if err != nil {
			return nil, "", permanent("tls client keypair: %v", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	t.tlsCache[k] = cfg
	return cfg, k, nil
}

type wireReq struct {
	method string
	url    string
	header http.Header
	body   []byte
	ok     func(status int) bool // nil = 2xx
}

type wireResp struct {
	status  int
	header  http.Header
	body    []byte
	ttft    time.Duration
	latency time.Duration
}

// streamFn consumes a streaming body. It must call first() when the first
// content arrives (used for TTFT).
type streamFn func(body io.Reader, first func()) error

func isTyped(err error) bool {
	var rl ErrRateLimited
	var tr ErrTransient
	var pe ErrPermanent
	return errors.As(err, &rl) || errors.As(err, &tr) || errors.As(err, &pe)
}

func (t *transport) do(parent context.Context, env *renderEnv, req wireReq, stream streamFn) (wireResp, error) {
	var out wireResp
	redact := env.redactor()
	ctx, cancel := context.WithTimeout(parent, t.timeout)
	defer cancel()

	var body io.Reader
	if len(req.body) > 0 {
		body = bytes.NewReader(req.body)
	}
	hr, err := http.NewRequestWithContext(ctx, req.method, req.url, body)
	if err != nil {
		return out, permanent("build request: %s", redact(err.Error()))
	}
	if hr.URL.Scheme != "http" && hr.URL.Scheme != "https" {
		return out, permanent("unsupported URL scheme %q", hr.URL.Scheme)
	}
	if hr.URL.Host == "" {
		return out, permanent("URL has no host")
	}
	hr.Header = req.header
	tcfg, tkey, err := t.tlsConfig(env)
	if err != nil {
		return out, err
	}
	client := t.clients.Client(hostPort(hr.URL.Scheme, hr.URL.Host), ClientOptions{Concurrency: t.conc, TLS: tcfg, TLSKey: tkey})

	start := time.Now()
	resp, err := client.Do(hr)
	if err != nil {
		return out, classifyNetErr(parent, redactErr(err, redact))
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, 64<<10) // let the connection be reused
		resp.Body.Close()
	}()
	out.status, out.header = resp.StatusCode, resp.Header

	okStatus := req.ok
	if okStatus == nil {
		okStatus = func(s int) bool { return s >= 200 && s < 300 }
	}
	if !okStatus(resp.StatusCode) {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
		out.latency = time.Since(start)
		return out, classifyStatus(resp.StatusCode, resp.Header, b, redact)
	}

	if stream != nil {
		var ttft time.Duration
		var once sync.Once
		first := func() { once.Do(func() { ttft = time.Since(start) }) }
		lr := &limitedReader{r: resp.Body, n: t.maxBody}
		serr := stream(lr, first)
		out.latency = time.Since(start)
		out.ttft = ttft
		if serr != nil {
			if isTyped(serr) {
				return out, serr
			}
			return out, classifyNetErr(parent, redactErr(serr, redact))
		}
		return out, nil
	}

	b, err := io.ReadAll(&limitedReader{r: resp.Body, n: t.maxBody})
	out.latency = time.Since(start)
	if err != nil {
		if isTyped(err) {
			return out, err
		}
		return out, classifyNetErr(parent, redactErr(err, redact))
	}
	out.body = b
	return out, nil
}

type redactedErr struct {
	msg string
	err error
}

func (e redactedErr) Error() string { return e.msg }
func (e redactedErr) Unwrap() error { return e.err }

// redactErr scrubs secret values from an error message (URLs may embed them)
// while keeping the error chain for classification.
func redactErr(err error, redact func(string) string) error {
	m := err.Error()
	if r := redact(m); r != m {
		return redactedErr{msg: r, err: err}
	}
	return err
}

// limitedReader fails with a permanent error once more than n bytes are read.
type limitedReader struct {
	r io.Reader
	n int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n < 0 {
		return 0, permanent("response exceeds max_response_bytes")
	}
	if int64(len(p)) > l.n+1 {
		p = p[:l.n+1]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n < 0 {
		return n, permanent("response exceeds max_response_bytes")
	}
	return n, err
}

// ---- streaming parsers ----

// readSSE parses a text/event-stream, calling fn for every dispatched event.
// data is only valid during the call. fn returns stop=true to end early.
func readSSE(r io.Reader, fn func(event string, data []byte) (stop bool, err error)) error {
	br := bufio.NewReaderSize(r, 32<<10)
	var data []byte
	var event string
	have := false
	dispatch := func() (bool, error) {
		if !have {
			event = ""
			return false, nil
		}
		stop, err := fn(event, data)
		data, event, have = data[:0], "", false
		return stop, err
	}
	var long []byte
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = append(long[:0], line...)
			for err == bufio.ErrBufferFull {
				line, err = br.ReadSlice('\n')
				long = append(long, line...)
			}
			line = long
		}
		if len(line) > 0 || err == nil {
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case len(line) == 0:
				if stop, derr := dispatch(); derr != nil || stop {
					return derr
				}
			case line[0] == ':':
			default:
				field, value := line, []byte(nil)
				if i := bytes.IndexByte(line, ':'); i >= 0 {
					field, value = line[:i], line[i+1:]
					if len(value) > 0 && value[0] == ' ' {
						value = value[1:]
					}
				}
				switch string(field) {
				case "data":
					if have {
						data = append(data, '\n')
					}
					data = append(data, value...)
					have = true
				case "event":
					event = string(value)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				_, derr := dispatch()
				return derr
			}
			return err
		}
	}
}

// readNDJSON calls fn for each non-empty line.
func readNDJSON(r io.Reader, fn func(line []byte) (stop bool, err error)) error {
	br := bufio.NewReaderSize(r, 32<<10)
	var long []byte
	for {
		line, err := br.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = append(long[:0], line...)
			for err == bufio.ErrBufferFull {
				line, err = br.ReadSlice('\n')
				long = append(long, line...)
			}
			line = long
		}
		if t := bytes.TrimSpace(line); len(t) > 0 {
			if stop, ferr := fn(t); ferr != nil || stop {
				return ferr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// ---- request recording / raw helpers ----

// recordRequest builds the JSON stored in Response.Request with secrets redacted.
func recordRequest(method, url string, hdr http.Header, body []byte, redact func(string) string) json.RawMessage {
	type rec struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers,omitempty"`
		Body    json.RawMessage   `json:"body,omitempty"`
	}
	r := rec{Method: method, URL: redact(url)}
	if len(hdr) > 0 {
		r.Headers = make(map[string]string, len(hdr))
		for k, v := range hdr {
			val := redact(strings.Join(v, ", "))
			if sensitiveHeader(k) {
				val = redacted
			}
			r.Headers[k] = val
		}
	}
	if len(body) > 0 {
		rb := redact(string(body))
		if json.Valid([]byte(rb)) {
			r.Body = json.RawMessage(rb)
		} else {
			r.Body, _ = json.Marshal(rb)
		}
	}
	b, _ := json.Marshal(r)
	return b
}

func sensitiveHeader(k string) bool {
	switch strings.ToLower(k) {
	case "authorization", "x-api-key", "api-key", "x-goog-api-key", "proxy-authorization", "x-amz-security-token", "cookie":
		return true
	}
	return false
}

// rawJSON returns body as a valid json.RawMessage (truncating / quoting non-JSON).
func rawJSON(body []byte) json.RawMessage {
	if len(body) <= rawLimit && json.Valid(body) {
		return json.RawMessage(body)
	}
	if len(body) > rawLimit {
		body = body[:rawLimit]
	}
	b, _ := json.Marshal(string(body))
	return b
}
