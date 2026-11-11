package api

import (
	"testing"

	"github.com/prat31/airval/internal/config"
)

func TestMockBaseURL(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.Config
		listen string
		want   string
	}{
		{
			name:   "explicit mock_url wins (Compose: workers reach the server by service name)",
			cfg:    config.Config{Server: config.Server{MockURL: "http://server:8080/_mock/", BaseURL: "http://localhost:8080"}, Workers: config.Workers{Embedded: true}},
			listen: "127.0.0.1:9000",
			want:   "http://server:8080/_mock",
		},
		{
			name:   "embedded worker uses the actual listener",
			cfg:    config.Config{Server: config.Server{BaseURL: "http://localhost:8080"}, Workers: config.Workers{Embedded: true}},
			listen: "0.0.0.0:9000",
			want:   "http://127.0.0.1:9000/_mock",
		},
		{
			name: "separate workers fall back to base_url",
			cfg:  config.Config{Server: config.Server{BaseURL: "https://airval.example.com/"}},
			want: "https://airval.example.com/_mock",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Config: &tc.cfg}
			if tc.listen != "" {
				s.SetListenAddr(tc.listen)
			}
			if got := s.MockBaseURL(); got != tc.want {
				t.Fatalf("MockBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
