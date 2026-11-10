package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// NewID returns a new time-sortable UUIDv7.
func NewID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

// NilID is the sentinel used where a key column must not be NULL (e.g. run-level aggregates).
var NilID = uuid.Nil

// Role is an organization role. Ordered: owner > admin > editor > viewer.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
	RoleViewer Role = "viewer"
)

// Rank returns the privilege level of a role (higher = more privileges, 0 = unknown).
func (r Role) Rank() int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r.Rank() > 0 }

// AtLeast reports whether r grants at least the privileges of min.
func (r Role) AtLeast(min Role) bool { return r.Rank() >= min.Rank() && r.Rank() > 0 }

type Organization struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OrgMembership is an organization together with the caller's role in it.
type OrgMembership struct {
	Organization
	Role Role
}

type User struct {
	ID           uuid.UUID
	Email        string // always lower-case
	Name         string
	PasswordHash *string
	AvatarURL    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Membership struct {
	OrgID     uuid.UUID
	UserID    uuid.UUID
	Role      Role
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Member is a membership joined with its user.
type Member struct {
	Membership
	User User
}

type APIKey struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  *uuid.UUID
	Name       string
	Prefix     string // first characters of the key, shown in UIs
	Hash       string // hex sha256 of the full key
	Role       Role
	CreatedBy  *uuid.UUID
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TokenHash  string
	CSRFToken  string
	UserAgent  string
	IP         string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

type Project struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	Name        string
	Slug        string
	Description string
	Color       string
	Icon        string
	Settings    json.RawMessage // retention etc.
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Secret struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProjectID   *uuid.UUID
	Name        string
	Description string
	Ciphertext  []byte
	Nonce       []byte
	KeyVersion  int
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Target struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ProjectID        uuid.UUID
	Name             string
	Description      string
	Kind             string // http | openai | anthropic | bedrock | vertex | agent
	CurrentVersionID *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// Populated on reads: the current version.
	CurrentVersion *TargetVersion
}

type TargetVersion struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	TargetID  uuid.UUID
	Number    int
	Config    json.RawMessage
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

type Dataset struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	ProjectID       uuid.UUID
	Name            string
	Description     string
	Schema          json.RawMessage
	LatestVersionID *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time // bumped by any draft example change

	// Populated on reads.
	ExampleCount        int
	LatestVersionNumber *int
	LatestVersionAt     *time.Time
}

// HasDraftChanges reports whether the draft differs from the latest published version.
func (d Dataset) HasDraftChanges() bool {
	if d.LatestVersionAt == nil {
		return d.ExampleCount > 0
	}
	return d.UpdatedAt.After(*d.LatestVersionAt)
}

// ExampleData is the content of one example revision. JSON fields map to evalcore.Example.
type ExampleData struct {
	Input        json.RawMessage // object
	Expected     json.RawMessage // any JSON value, may be nil
	Context      json.RawMessage // array of strings, may be nil
	Metadata     json.RawMessage // object, may be nil
	Tags         []string
	Conversation json.RawMessage // evalcore.Conversation, may be nil
}

type Example struct {
	ID         uuid.UUID // stable logical id
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	DatasetID  uuid.UUID
	RevisionID uuid.UUID
	ExampleData
	CreatedAt time.Time
	UpdatedAt time.Time // revision time
}

// ExampleUpsert is one item of a bulk upsert. A nil ID (or an unknown ID) creates a new example
// (using ID when given); a known ID creates a new revision only when the content changed.
type ExampleUpsert struct {
	ID *uuid.UUID
	ExampleData
}

type DatasetVersion struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ProjectID    uuid.UUID
	DatasetID    uuid.UUID
	Number       int
	Note         string
	ExampleCount int
	CreatedBy    *uuid.UUID
	CreatedAt    time.Time
}

type Evaluator struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	Description string
	Kind        string // equals | contains | ... | llm_rubric | human (see evaluator registry)
	Config      json.RawMessage
	Version     int // bumped whenever kind/config changes
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Suite struct {
	ID            uuid.UUID
	OrgID         uuid.UUID
	ProjectID     uuid.UUID
	Name          string
	Slug          string
	Description   string
	Config        json.RawMessage // SuiteConfig in the API
	BaselineRunID *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Gate struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	SuiteID   uuid.UUID
	Name      string
	Expr      json.RawMessage // GateExpr AST
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Trigger struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ProjectID        uuid.UUID
	SuiteID          uuid.UUID
	Name             string
	Kind             string // manual | schedule | webhook | api
	Config           json.RawMessage
	Enabled          bool
	SecretEnc        []byte // webhook HMAC secret, encrypted (nonce||ciphertext)
	SecretKeyVersion int
	NextFireAt       *time.Time
	LastFiredAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Run statuses.
const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunSucceeded = "succeeded" // finished, gate passed or no gate
	RunFailed    = "failed"    // finished, gate failed
	RunErrored   = "errored"   // could not complete
	RunCanceled  = "canceled"
)

type Run struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ProjectID        uuid.UUID
	SuiteID          *uuid.UUID
	TargetID         *uuid.UUID
	TargetVersionID  *uuid.UUID
	DatasetVersionID *uuid.UUID
	Name             string
	TriggerKind      string // manual | schedule | webhook | api
	TriggerRef       string // trigger id, CI job url...
	Labels           map[string]string
	Config           json.RawMessage // resolved suite config snapshot
	Status           string
	Total            int
	Completed        int
	Failed           int
	Errored          int
	StartedAt        *time.Time
	FinishedAt       *time.Time
	BaselineRunID    *uuid.UUID
	ParentRunID      *uuid.UUID
	GatePassed       *bool
	Summary          json.RawMessage
	CostUSD          float64
	TokensIn         int64
	TokensOut        int64
	Error            string
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// RunProgress is an atomic increment of a run's counters.
type RunProgress struct {
	Completed int
	Failed    int
	Errored   int
	CostUSD   float64
	TokensIn  int64
	TokensOut int64
}

// Result statuses.
const (
	ResultPassed  = "passed"
	ResultFailed  = "failed"
	ResultErrored = "errored"
	ResultSkipped = "skipped"
)

// Result is output + metadata for one example × trial. JSON fields map to evalcore.Response.
type Result struct {
	ID                uuid.UUID
	RunID             uuid.UUID
	ExampleID         uuid.UUID
	ExampleRevisionID uuid.UUID
	Trial             int
	Status            string
	Output            string          // stored zstd-compressed above 8 KiB, transparently
	OutputJSON        json.RawMessage // structured output
	Context           json.RawMessage // retrieved context []string
	ToolCalls         json.RawMessage // []evalcore.ToolCall
	Trace             json.RawMessage // []evalcore.TraceStep
	Transcript        json.RawMessage // []evalcore.Message
	Request           json.RawMessage
	Response          json.RawMessage // raw response (truncated)
	LatencyMs         float64
	TTFTMs            *float64
	TokensIn          int
	TokensOut         int
	CostUSD           float64
	Error             string
	Cached            bool
	CreatedAt         time.Time

	// Populated on reads.
	Scores []Score
	Tags   []string // example revision tags (populated by IterateRunResults and ListResults)
}

type Score struct {
	ResultID    uuid.UUID
	EvaluatorID uuid.UUID
	RunID       uuid.UUID
	Value       *float64
	Label       string
	Passed      *bool
	Reason      string
	Meta        json.RawMessage
	LatencyMs   float64
	CostUSD     float64
	CreatedAt   time.Time
}

// Well-known run aggregate metrics.
const (
	MetricScore     = "score"      // per evaluator
	MetricLatencyMs = "latency_ms" // run-level, EvaluatorID = NilID
	MetricTTFTMs    = "ttft_ms"
	MetricCostUSD   = "cost_usd"
	MetricTokens    = "tokens"
	MetricPassRate  = "pass_rate" // run-level result pass rate
)

type RunAggregate struct {
	RunID       uuid.UUID
	EvaluatorID uuid.UUID // NilID for run-level metrics
	Metric      string
	SliceKey    string // "" = all results; "tag:<tag>" for per-tag slices
	N           int
	Mean        *float64
	Min         *float64
	Max         *float64
	P50         *float64
	P90         *float64
	P95         *float64
	P99         *float64
	Stddev      *float64
	Sum         *float64
	PassRate    *float64
	Histogram   json.RawMessage // {"edges":[...],"counts":[...]}
	UpdatedAt   time.Time
}

type ReviewQueue struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	Description string
	EvaluatorID *uuid.UUID
	Config      json.RawMessage // {"mode":"all|failures|sample","sample_pct":10,"labels":[...],"scale":5}
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Populated on reads.
	PendingCount int
	DoneCount    int
}

// Review item statuses.
const (
	ReviewPending    = "pending"
	ReviewInProgress = "in_progress"
	ReviewDone       = "done"
	ReviewSkipped    = "skipped"
)

type ReviewItem struct {
	ID          uuid.UUID
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	QueueID     uuid.UUID
	RunID       uuid.UUID
	ResultID    uuid.UUID
	Status      string
	AssignedTo  *uuid.UUID
	CompletedBy *uuid.UUID
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Annotation struct {
	ID           uuid.UUID
	OrgID        uuid.UUID
	ProjectID    uuid.UUID
	ResultID     uuid.UUID
	ReviewItemID *uuid.UUID
	UserID       *uuid.UUID
	EvaluatorID  *uuid.UUID
	Value        *float64
	Label        string
	Passed       *bool
	Comment      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Comparison struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Name      string
	RunIDs    []uuid.UUID
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type NotificationChannel struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	Name      string
	Kind      string // slack | webhook | email
	Config    json.RawMessage
	Events    []string // run.finished | run.failed | gate.failed
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AuditEntry struct {
	ID      uuid.UUID
	OrgID   uuid.UUID
	ActorID *uuid.UUID
	Actor   string // email or "apikey:<prefix>"
	Action  string // e.g. "secret.create"
	Target  string // e.g. "secret:<id>"
	Diff    json.RawMessage
	At      time.Time
}
