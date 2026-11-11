//go:build ee

package httpx

import (
	stdctx "context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

func context(r *http.Request) stdctx.Context { return stdctx.WithoutCancel(r.Context()) }

func requestID(r *http.Request) string { return middleware.GetReqID(r.Context()) }

// ClientIP returns the caller's IP (RemoteAddr after the core RealIP middleware), without the port.
func ClientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return strings.Trim(r.RemoteAddr, "[]")
}

// SafeRedirect returns target when it is a same-origin absolute path ("/x", not "//host" or "/\host"),
// else "/". It prevents open redirects after sign-in.
func SafeRedirect(target string) string {
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") ||
		strings.HasPrefix(target, `/\`) || strings.ContainsAny(target, "\r\n\t") {
		return "/"
	}
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return target
}
