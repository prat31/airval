package exttest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/prat31/airval/internal/auth"
)

// Obj is a JSON object.
type Obj = map[string]any

// Client is a browser-like client (cookie jar + CSRF) or an API-key / bearer client.
type Client struct {
	T      testing.TB
	E      *Env
	HC     *http.Client
	CSRF   string
	Bearer string
}

// Browser returns a client with a cookie jar that does not follow redirects.
func (e *Env) Browser(t testing.TB) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{T: t, E: e, HC: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

// Raw sends a request and returns the response with its body read.
func (c *Client) Raw(method, path string, body any) (*http.Response, []byte) {
	c.T.Helper()
	var r io.Reader
	if body != nil {
		if b, ok := body.([]byte); ok {
			r = bytes.NewReader(b)
		} else {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
	}
	url := path
	if len(path) > 0 && path[0] == '/' {
		url = c.E.Server.URL + path
	}
	req, _ := http.NewRequest(method, url, r)
	req.Header.Set("Content-Type", "application/json")
	if c.CSRF != "" {
		req.Header.Set(auth.CSRFHeader, c.CSRF)
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	resp, err := c.HC.Do(req)
	if err != nil {
		c.T.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

// Do sends a request, asserts the status and decodes the JSON body into out (when non-nil).
func (c *Client) Do(method, path string, body any, want int, out any) {
	c.T.Helper()
	resp, data := c.Raw(method, path, body)
	if resp.StatusCode != want {
		c.T.Fatalf("%s %s: status %d want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil {
		if s, ok := out.(*string); ok {
			*s = string(data)
			return
		}
		if err := json.Unmarshal(data, out); err != nil {
			c.T.Fatalf("%s %s: decode: %v: %s", method, path, err, data)
		}
	}
}

// Setup completes first-run setup and returns the owner's client and the org id.
func (e *Env) Setup(t testing.TB, email string) (*Client, string) {
	t.Helper()
	c := e.Browser(t)
	var me Obj
	c.Do("POST", "/v1/setup", Obj{"email": email, "password": "supersecret1", "org_name": "Acme"}, 201, &me)
	c.CSRF = me["csrf_token"].(string)
	return c, me["orgs"].([]any)[0].(Obj)["id"].(string)
}

// Member adds a member with a password (via the owner) and returns a logged-in client and the user id.
func (e *Env) Member(t testing.TB, owner *Client, org, email, role string) (*Client, string) {
	t.Helper()
	var m Obj
	owner.Do("POST", "/v1/orgs/"+org+"/members", Obj{"email": email, "role": role, "password": "supersecret1"}, 201, &m)
	c := e.Browser(t)
	var me Obj
	c.Do("POST", "/v1/auth/login", Obj{"email": email, "password": "supersecret1"}, 200, &me)
	c.CSRF = me["csrf_token"].(string)
	return c, m["user"].(Obj)["id"].(string)
}
