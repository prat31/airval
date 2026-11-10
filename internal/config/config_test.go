package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "airval.yaml")
	if err := os.WriteFile(path, []byte("server:\n  addr: \":9000\"\nworkers:\n  concurrency: 8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIRVAL_WORKERS_CONCURRENCY", "16")
	t.Setenv("AIRVAL_SECRETS_MASTER_KEY", "k")
	t.Setenv("AIRVAL_BOOTSTRAP_ADMIN_EMAIL", "a@b.c")
	t.Setenv("AIRVAL_AUTH_SESSION_TTL", "1h")
	c, err := Load(ModeDev, path)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"file overrides default", c.Server.Addr, ":9000"},
		{"env overrides file", c.Workers.Concurrency, 16},
		{"env nested key", c.Secrets.MasterKey, "k"},
		{"bootstrap", c.Bootstrap.AdminEmail, "a@b.c"},
		{"duration", c.Auth.SessionTTL, time.Hour},
		{"dev sqlite default", c.Database.URL, "sqlite://./airval-data/airval.db"},
		{"dev embedded worker", c.Workers.Embedded, true},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestValidateServer(t *testing.T) {
	t.Setenv("AIRVAL_DATABASE_URL", "")
	if _, err := Load(ModeServer, ""); err == nil {
		t.Fatal("server mode without database.url must fail")
	}
}

func TestRedacted(t *testing.T) {
	c := Config{Database: Database{URL: "postgres://u:secret@h/db"}, Secrets: Secrets{MasterKey: "k"}}
	r := c.Redacted()
	if r.Database.URL != "postgres://u:********@h/db" || r.Secrets.MasterKey != "********" {
		t.Fatalf("not redacted: %+v", r)
	}
}
