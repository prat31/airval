// Package config loads airval configuration from defaults, an optional YAML file and AIRVAL_* env vars
// (in that order of precedence, lowest first).
//
// Env mapping: strip AIRVAL_, lower-case, and the first "_" separates section from key:
// AIRVAL_DATABASE_URL → database.url, AIRVAL_SECRETS_MASTER_KEY → secrets.master_key,
// AIRVAL_BOOTSTRAP_ADMIN_EMAIL → bootstrap.admin_email.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Database  Database  `koanf:"database" yaml:"database"`
	NATS      NATS      `koanf:"nats" yaml:"nats"`
	Secrets   Secrets   `koanf:"secrets" yaml:"secrets"`
	Server    Server    `koanf:"server" yaml:"server"`
	Auth      Auth      `koanf:"auth" yaml:"auth"`
	Bootstrap Bootstrap `koanf:"bootstrap" yaml:"bootstrap"`
	Workers   Workers   `koanf:"workers" yaml:"workers"`
	Worker    Worker    `koanf:"worker" yaml:"worker"`
	Telemetry Telemetry `koanf:"telemetry" yaml:"telemetry"`
	Security  Security  `koanf:"security" yaml:"security"`
}

type Database struct {
	// URL: postgres://... or a SQLite path (sqlite://./airval-data/airval.db).
	URL          string `koanf:"url" yaml:"url"`
	MaxOpenConns int    `koanf:"max_open_conns" yaml:"max_open_conns"`
	// AutoMigrate applies migrations on startup (default true for dev and server).
	AutoMigrate bool `koanf:"auto_migrate" yaml:"auto_migrate"`
}

type NATS struct {
	// URL of an external NATS cluster. Empty = run an embedded JetStream server.
	URL string `koanf:"url" yaml:"url"`
	// StoreDir is the embedded server's JetStream directory.
	StoreDir string `koanf:"store_dir" yaml:"store_dir"`
	// Port for the embedded server (-1 = in-process only, no listener).
	Port            int    `koanf:"port" yaml:"port"`
	CredsFile       string `koanf:"creds_file" yaml:"creds_file"`
	Replicas        int    `koanf:"replicas" yaml:"replicas"`
	StreamMaxAgeHrs int    `koanf:"stream_max_age_hours" yaml:"stream_max_age_hours"`
}

type Secrets struct {
	// MasterKey is base64 of 32 bytes (AES-256). Required outside dev.
	MasterKey string `koanf:"master_key" yaml:"master_key"`
	// KeyVersion labels MasterKey; bump it when rotating.
	KeyVersion int `koanf:"key_version" yaml:"key_version"`
	// PreviousKeys maps older versions to keys ("1:base64,2:base64") so old ciphertexts stay readable.
	PreviousKeys string `koanf:"previous_keys" yaml:"previous_keys"`
}

type Server struct {
	Addr            string        `koanf:"addr" yaml:"addr"`
	BaseURL         string        `koanf:"base_url" yaml:"base_url"`
	ShutdownTimeout time.Duration `koanf:"shutdown_timeout" yaml:"shutdown_timeout"`
	// UIDir serves the UI from disk instead of the embedded build (frontend development).
	UIDir string `koanf:"ui_dir" yaml:"ui_dir"`
	// MaxUploadMB caps dataset import uploads (default 512).
	MaxUploadMB int `koanf:"max_upload_mb" yaml:"max_upload_mb"`
}

type Auth struct {
	SessionTTL   time.Duration `koanf:"session_ttl" yaml:"session_ttl"`
	CookieSecure bool          `koanf:"cookie_secure" yaml:"cookie_secure"`
	CookieDomain string        `koanf:"cookie_domain" yaml:"cookie_domain"`
	// AllowSignup lets anyone create an account (normally users are invited by admins).
	AllowSignup bool `koanf:"allow_signup" yaml:"allow_signup"`
}

// Bootstrap creates the first owner and organization at startup when no users exist (Helm installs).
type Bootstrap struct {
	AdminEmail    string `koanf:"admin_email" yaml:"admin_email"`
	AdminPassword string `koanf:"admin_password" yaml:"admin_password"`
	AdminName     string `koanf:"admin_name" yaml:"admin_name"`
	OrgName       string `koanf:"org_name" yaml:"org_name"`
}

type Workers struct {
	Concurrency int `koanf:"concurrency" yaml:"concurrency"`
	// JudgeConcurrency bounds concurrent LLM-judge calls per worker.
	JudgeConcurrency int           `koanf:"judge_concurrency" yaml:"judge_concurrency"`
	ShutdownTimeout  time.Duration `koanf:"shutdown_timeout" yaml:"shutdown_timeout"`
	// Embedded runs the worker loop inside the server process (airval dev).
	Embedded bool `koanf:"embedded" yaml:"embedded"`
}

type Telemetry struct {
	LogLevel     string  `koanf:"log_level" yaml:"log_level"`
	LogFormat    string  `koanf:"log_format" yaml:"log_format"` // json | text
	OTLPEndpoint string  `koanf:"otlp_endpoint" yaml:"otlp_endpoint"`
	ServiceName  string  `koanf:"service_name" yaml:"service_name"`
	SampleRatio  float64 `koanf:"sample_ratio" yaml:"sample_ratio"`
	Metrics      bool    `koanf:"metrics" yaml:"metrics"`
}

// Worker configures the `airval worker` process's operational HTTP listener.
type Worker struct {
	// Listen serves /healthz, /readyz (DB + NATS) and /metrics (default ":8081"; empty disables).
	Listen string `koanf:"listen" yaml:"listen"`
}

// Security holds outbound-request guards for http targets and webhooks.
type Security struct {
	// AllowCIDRs / DenyCIDRs are comma-separated. Metadata IPs are always denied unless allowed explicitly.
	AllowCIDRs string `koanf:"allow_cidrs" yaml:"allow_cidrs"`
	DenyCIDRs  string `koanf:"deny_cidrs" yaml:"deny_cidrs"`
}

// Mode selects mode-specific defaults.
type Mode string

const (
	ModeDev    Mode = "dev"
	ModeServer Mode = "server"
	ModeWorker Mode = "worker"
	ModeCLI    Mode = "cli"
)

// DataDir is where `airval dev` keeps its state.
const DataDir = "./airval-data"

func defaults(mode Mode) map[string]any {
	d := map[string]any{
		"database.url":              "",
		"database.max_open_conns":   0,
		"database.auto_migrate":     true,
		"nats.url":                  "",
		"nats.store_dir":            DataDir + "/nats",
		"nats.port":                 -1,
		"nats.replicas":             1,
		"nats.stream_max_age_hours": 72,
		"secrets.key_version":       1,
		"server.addr":               ":8080",
		"server.base_url":           "http://localhost:8080",
		"server.shutdown_timeout":   "20s",
		"server.max_upload_mb":      512,
		"auth.session_ttl":          "720h",
		"auth.cookie_secure":        false,
		"workers.concurrency":       256,
		"workers.judge_concurrency": 32,
		"workers.shutdown_timeout":  "30s",
		"workers.embedded":          false,
		"worker.listen":             ":8081",
		"telemetry.log_level":       "info",
		"telemetry.log_format":      "json",
		"telemetry.service_name":    "airval",
		"telemetry.sample_ratio":    0.1,
		"telemetry.metrics":         true,
		"bootstrap.org_name":        "Default",
		"bootstrap.admin_name":      "Admin",
		"security.deny_cidrs":       "",
		"security.allow_cidrs":      "",
	}
	if mode == ModeDev {
		d["database.url"] = "sqlite://" + DataDir + "/airval.db"
		d["workers.embedded"] = true
		d["telemetry.log_format"] = "text"
		d["telemetry.sample_ratio"] = 1.0
	}
	return d
}

// Load reads configuration and validates it for mode. path may be empty (then AIRVAL_CONFIG is consulted).
func Load(mode Mode, path string) (*Config, error) {
	c, err := LoadUnvalidated(mode, path)
	if err != nil {
		return nil, err
	}
	return c, c.Validate(mode)
}

// LoadUnvalidated reads configuration with mode defaults but skips validation (config print).
func LoadUnvalidated(mode Mode, path string) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults(mode), "."), nil); err != nil {
		return nil, err
	}
	if path == "" {
		path = os.Getenv("AIRVAL_CONFIG")
	}
	if path != "" {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
	}
	if err := k.Load(env.Provider(".", env.Opt{Prefix: "AIRVAL_", TransformFunc: envKey}), nil); err != nil {
		return nil, err
	}
	var c Config
	if err := k.UnmarshalWithConf("", &c, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

func envKey(k, v string) (string, any) {
	k = strings.ToLower(strings.TrimPrefix(k, "AIRVAL_"))
	if k == "config" {
		return "", nil // AIRVAL_CONFIG is the file path, not a key
	}
	if i := strings.IndexByte(k, '_'); i > 0 {
		k = k[:i] + "." + k[i+1:]
	}
	return k, v
}

// Validate checks mode-specific requirements.
func (c *Config) Validate(mode Mode) error {
	switch mode {
	case ModeServer, ModeWorker:
		if c.Database.URL == "" {
			return fmt.Errorf("config: database.url (AIRVAL_DATABASE_URL) is required")
		}
		if c.Secrets.MasterKey == "" {
			return fmt.Errorf("config: secrets.master_key (AIRVAL_SECRETS_MASTER_KEY) is required")
		}
	}
	if c.Workers.Concurrency <= 0 {
		c.Workers.Concurrency = 1
	}
	return nil
}

// Redacted returns a copy safe to print (secrets masked).
func (c Config) Redacted() Config {
	mask := func(s string) string {
		if s == "" {
			return ""
		}
		return "********"
	}
	c.Secrets.MasterKey = mask(c.Secrets.MasterKey)
	c.Secrets.PreviousKeys = mask(c.Secrets.PreviousKeys)
	c.Bootstrap.AdminPassword = mask(c.Bootstrap.AdminPassword)
	c.Database.URL = redactURL(c.Database.URL)
	c.NATS.URL = redactURL(c.NATS.URL)
	return c
}

func redactURL(u string) string {
	at := strings.LastIndexByte(u, '@')
	scheme := strings.Index(u, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return u
	}
	userinfo := u[scheme+3 : at]
	if i := strings.IndexByte(userinfo, ':'); i >= 0 {
		return u[:scheme+3] + userinfo[:i] + ":********" + u[at:]
	}
	return u
}
