package targets

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// ClientOptions tunes the transport created for one target host.
type ClientOptions struct {
	// Concurrency sizes MaxIdleConnsPerHost so each in-flight request can reuse a
	// warm connection. Defaults to 64.
	Concurrency int
	// TLS is an optional TLS config (custom CA, mTLS). TLSKey must change whenever
	// its content changes; it is part of the cache key.
	TLS    *tls.Config
	TLSKey string
	// ResponseHeaderTimeout bounds the wait for response headers (0 = none; the
	// per-request context timeout still applies).
	ResponseHeaderTimeout time.Duration
}

type clientKey struct {
	host   string
	conc   int
	tlsKey string
	rht    time.Duration
}

// ClientFactory hands out one tuned *http.Client (and transport) per target
// host so connections, HTTP/2 sessions and TLS sessions are shared across all
// concurrent calls to that host. Every connection passes the SSRF guard.
type ClientFactory struct {
	guard   *SSRFGuard
	mu      sync.RWMutex
	clients map[clientKey]*http.Client
	rt      http.RoundTripper // test hook: replaces the transport (benchmarks measure pure adapter overhead)
}

// NewClientFactory creates a factory. A nil guard applies the default policy.
func NewClientFactory(guard *SSRFGuard) *ClientFactory {
	return &ClientFactory{guard: guard, clients: map[clientKey]*http.Client{}}
}

var (
	sharedOnce    sync.Once
	sharedFactory *ClientFactory
)

// DefaultClients returns the process-wide factory with the default SSRF policy.
func DefaultClients() *ClientFactory {
	sharedOnce.Do(func() { sharedFactory = NewClientFactory(nil) })
	return sharedFactory
}

// Client returns the shared client for hostport (e.g. "api.openai.com:443").
func (f *ClientFactory) Client(hostport string, o ClientOptions) *http.Client {
	if o.Concurrency <= 0 {
		o.Concurrency = 64
	}
	k := clientKey{hostport, o.Concurrency, o.TLSKey, o.ResponseHeaderTimeout}
	f.mu.RLock()
	c := f.clients[k]
	f.mu.RUnlock()
	if c != nil {
		return c
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c = f.clients[k]; c != nil {
		return c
	}
	if f.rt != nil {
		c = &http.Client{Transport: f.rt}
		f.clients[k] = c
		return c
	}
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   f.guard.Control,
	}
	tr := &http.Transport{
		Proxy:                 nil, // never route through env proxies: they would bypass the SSRF guard
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          o.Concurrency * 2,
		MaxIdleConnsPerHost:   o.Concurrency,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: o.ResponseHeaderTimeout,
		TLSClientConfig:       o.TLS,
		WriteBufferSize:       16 << 10,
		ReadBufferSize:        16 << 10,
	}
	c = &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}
	f.clients[k] = c
	return c
}

// CloseIdle closes idle connections on every client.
func (f *ClientFactory) CloseIdle() {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, c := range f.clients {
		c.CloseIdleConnections()
	}
}

func hostPort(scheme, host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	p := 80
	if scheme == "https" {
		p = 443
	}
	return net.JoinHostPort(host, strconv.Itoa(p))
}
