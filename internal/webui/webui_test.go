package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":        {Data: []byte("<html>app</html>")},
		"assets/app-abc.js": {Data: []byte("js")},
		"favicon.svg":       {Data: []byte("<svg/>")},
	}
	h := Handler(fsys)
	tests := []struct {
		path      string
		code      int
		body      string
		cacheLong bool
	}{
		{"/", 200, "<html>app</html>", false},
		{"/projects/123/runs", 200, "<html>app</html>", false},
		{"/assets/app-abc.js", 200, "js", true},
		{"/assets/missing.js", 404, "", false},
		{"/favicon.svg", 200, "<svg/>", false},
	}
	for _, tc := range tests {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.code {
			t.Errorf("%s: code %d want %d", tc.path, w.Code, tc.code)
		}
		if tc.body != "" && w.Body.String() != tc.body {
			t.Errorf("%s: body %q", tc.path, w.Body.String())
		}
		if long := w.Header().Get("Cache-Control") == "public, max-age=31536000, immutable"; long != tc.cacheLong {
			t.Errorf("%s: cache header %q", tc.path, w.Header().Get("Cache-Control"))
		}
	}
	if _, err := Embedded().Open("."); err != nil {
		t.Fatal(err)
	}
}

func TestManifestContentType(t *testing.T) {
	h := Handler(fstest.MapFS{
		"index.html":       {Data: []byte("<html>app</html>")},
		"site.webmanifest": {Data: []byte(`{"name":"airval"}`)},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/site.webmanifest", nil))
	if got := rec.Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Fatalf("Content-Type = %q, want application/manifest+json", got)
	}
}
