// Package store defines airval's persistence interfaces and models. Implementations live in
// internal/store/sqlstore (Postgres and SQLite share one implementation with dialect hooks).
// See README.md in this directory for how to add queries.
package store

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict is returned on unique-constraint violations.
	ErrConflict = errors.New("store: conflict")
	// ErrInvalidCursor is returned for malformed pagination cursors.
	ErrInvalidCursor = errors.New("store: invalid cursor")
)

// Page bounds.
const (
	DefaultLimit = 50
	MaxLimit     = 1000
)

// ListParams is cursor pagination input. Cursor is opaque (from Page.NextCursor).
type ListParams struct {
	Cursor string
	Limit  int
}

// Normalize returns the effective limit.
func (p ListParams) Normalize() int {
	switch {
	case p.Limit <= 0:
		return DefaultLimit
	case p.Limit > MaxLimit:
		return MaxLimit
	}
	return p.Limit
}

// Page is one page of results. NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// EncodeCursor encodes a row id into an opaque cursor.
func EncodeCursor(id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

// DecodeCursor decodes an opaque cursor. An empty cursor yields (uuid.Nil, false, nil).
func DecodeCursor(c string) (uuid.UUID, bool, error) {
	if c == "" {
		return uuid.Nil, false, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(b) != 16 {
		return uuid.Nil, false, ErrInvalidCursor
	}
	id, _ := uuid.FromBytes(b)
	return id, true, nil
}

// Filters.

type ProjectFilter struct {
	OrgIDs []uuid.UUID // required: projects in these orgs
	IDs    []uuid.UUID // optional: restrict to these projects (API keys scoped to a project)
	Slug   string
}

type SecretFilter struct {
	OrgID uuid.UUID
	// ProjectID nil = org-level secrets only (unless AllScopes); set = that project's secrets.
	ProjectID *uuid.UUID
	AllScopes bool
}

type ExampleFilter struct {
	DatasetID uuid.UUID
	VersionID *uuid.UUID // nil = draft (current examples)
	Tag       string
	Search    string // substring over input/expected/metadata
	IDs       []uuid.UUID
}

type RunFilter struct {
	ProjectID     *uuid.UUID
	OrgIDs        []uuid.UUID // restrict to orgs the caller belongs to
	SuiteID       *uuid.UUID
	TargetID      *uuid.UUID
	Statuses      []string
	TriggerKind   string
	Labels        map[string]string // all must match
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	ParentRunID   *uuid.UUID
}

type ResultFilter struct {
	RunID       uuid.UUID
	Statuses    []string
	EvaluatorID *uuid.UUID // with Passed / ScoreMin / ScoreMax
	Passed      *bool
	ScoreMin    *float64
	ScoreMax    *float64
	Tag         string
	Search      string // substring over output and input
	ExampleID   *uuid.UUID
	Trial       *int
}

type ReviewItemFilter struct {
	QueueID    uuid.UUID
	Statuses   []string
	AssignedTo *uuid.UUID
}

type AnnotationFilter struct {
	ProjectID    *uuid.UUID
	OrgIDs       []uuid.UUID
	ResultID     *uuid.UUID
	ReviewItemID *uuid.UUID
	EvaluatorID  *uuid.UUID
}

// Store is the full persistence interface. It is composed of per-area interfaces so that
// consumers can depend on the narrowest one.
type Store interface {
	OrgStore
	UserStore
	AuthStore
	ProjectStore
	SecretStore
	TargetStore
	DatasetStore
	EvaluatorStore
	SuiteStore
	RunStore
	ResultStore
	ReviewStore
	ComparisonStore
	NotificationStore
	AuditStore
	CacheStore
	EngineStore

	// WithTx runs fn in a transaction. Nested calls reuse the outer transaction.
	WithTx(ctx context.Context, fn func(tx Store) error) error
	Ping(ctx context.Context) error
	Dialect() string // "postgres" | "sqlite"
	Close() error
}

type OrgStore interface {
	CreateOrg(ctx context.Context, o *Organization) error
	GetOrg(ctx context.Context, id uuid.UUID) (Organization, error)
	GetOrgBySlug(ctx context.Context, slug string) (Organization, error)
	UpdateOrg(ctx context.Context, o *Organization) error
	DeleteOrg(ctx context.Context, id uuid.UUID) error
	ListUserOrgs(ctx context.Context, userID uuid.UUID) ([]OrgMembership, error)

	UpsertMembership(ctx context.Context, m *Membership) error
	GetMembership(ctx context.Context, orgID, userID uuid.UUID) (Membership, error)
	ListMembers(ctx context.Context, orgID uuid.UUID, p ListParams) (Page[Member], error)
	DeleteMembership(ctx context.Context, orgID, userID uuid.UUID) error
	CountOwners(ctx context.Context, orgID uuid.UUID) (int, error)
}

type UserStore interface {
	CreateUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, id uuid.UUID) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	UpdateUser(ctx context.Context, u *User) error
	CountUsers(ctx context.Context) (int, error)
}

type AuthStore interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSessionByTokenHash(ctx context.Context, hash string) (Session, error)
	TouchSession(ctx context.Context, id uuid.UUID, seen, expires time.Time) error
	DeleteSession(ctx context.Context, id uuid.UUID) error
	DeleteUserSessions(ctx context.Context, userID uuid.UUID) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error)

	CreateAPIKey(ctx context.Context, k *APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (APIKey, error)
	ListAPIKeys(ctx context.Context, orgID uuid.UUID, p ListParams) (Page[APIKey], error)
	DeleteAPIKey(ctx context.Context, orgID, id uuid.UUID) error
	TouchAPIKey(ctx context.Context, id uuid.UUID, at time.Time) error
}

type ProjectStore interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id uuid.UUID) (Project, error)
	ListProjects(ctx context.Context, f ProjectFilter, p ListParams) (Page[Project], error)
	UpdateProject(ctx context.Context, p *Project) error
	DeleteProject(ctx context.Context, id uuid.UUID) error
}

type SecretStore interface {
	CreateSecret(ctx context.Context, s *Secret) error
	GetSecret(ctx context.Context, id uuid.UUID) (Secret, error)
	// ResolveSecret finds a secret by name, preferring the project-scoped one over the org-level one.
	ResolveSecret(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, name string) (Secret, error)
	ListSecrets(ctx context.Context, f SecretFilter, p ListParams) (Page[Secret], error)
	UpdateSecret(ctx context.Context, s *Secret) error
	DeleteSecret(ctx context.Context, id uuid.UUID) error
	// ListSecretsNotAtKeyVersion returns secrets encrypted under another key version (for rotation).
	ListSecretsNotAtKeyVersion(ctx context.Context, version int, limit int) ([]Secret, error)
}

type TargetStore interface {
	// CreateTarget inserts the target and its first version (config) and sets it current.
	CreateTarget(ctx context.Context, t *Target, config []byte, createdBy *uuid.UUID) error
	GetTarget(ctx context.Context, id uuid.UUID) (Target, error)
	ListTargets(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[Target], error)
	UpdateTarget(ctx context.Context, t *Target) error
	DeleteTarget(ctx context.Context, id uuid.UUID) error
	// AddTargetVersion appends a config version and makes it current.
	AddTargetVersion(ctx context.Context, targetID uuid.UUID, config []byte, createdBy *uuid.UUID) (TargetVersion, error)
	GetTargetVersion(ctx context.Context, id uuid.UUID) (TargetVersion, error)
	ListTargetVersions(ctx context.Context, targetID uuid.UUID, p ListParams) (Page[TargetVersion], error)
}

type DatasetStore interface {
	CreateDataset(ctx context.Context, d *Dataset) error
	GetDataset(ctx context.Context, id uuid.UUID) (Dataset, error)
	ListDatasets(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[Dataset], error)
	UpdateDataset(ctx context.Context, d *Dataset) error
	DeleteDataset(ctx context.Context, id uuid.UUID) error

	// UpsertExamples applies a bulk upsert to the dataset draft in one transaction.
	UpsertExamples(ctx context.Context, datasetID uuid.UUID, items []ExampleUpsert) ([]Example, error)
	DeleteExamples(ctx context.Context, datasetID uuid.UUID, ids []uuid.UUID) (int, error)
	GetExample(ctx context.Context, id uuid.UUID) (Example, error)
	ListExamples(ctx context.Context, f ExampleFilter, p ListParams) (Page[Example], error)
	// GetExampleRevisions loads revisions by id (used by workers to resolve shard examples).
	GetExampleRevisions(ctx context.Context, revisionIDs []uuid.UUID) ([]Example, error)

	// PublishDatasetVersion freezes the draft into a new immutable version (copy-on-write).
	PublishDatasetVersion(ctx context.Context, datasetID uuid.UUID, note string, createdBy *uuid.UUID) (DatasetVersion, error)
	GetDatasetVersion(ctx context.Context, id uuid.UUID) (DatasetVersion, error)
	GetDatasetVersionByNumber(ctx context.Context, datasetID uuid.UUID, number int) (DatasetVersion, error)
	ListDatasetVersions(ctx context.Context, datasetID uuid.UUID, p ListParams) (Page[DatasetVersion], error)
	// ListDatasetVersionItems returns (example_id, revision_id) pairs of a version in stable order.
	ListDatasetVersionItems(ctx context.Context, versionID uuid.UUID) ([]VersionItem, error)
}

// VersionItem is one member of a dataset version.
type VersionItem struct {
	ExampleID  uuid.UUID
	RevisionID uuid.UUID
}

type EvaluatorStore interface {
	CreateEvaluator(ctx context.Context, e *Evaluator) error
	GetEvaluator(ctx context.Context, id uuid.UUID) (Evaluator, error)
	GetEvaluators(ctx context.Context, ids []uuid.UUID) ([]Evaluator, error)
	ListEvaluators(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[Evaluator], error)
	// UpdateEvaluator saves e; Version is bumped by the caller when kind/config changed.
	UpdateEvaluator(ctx context.Context, e *Evaluator) error
	DeleteEvaluator(ctx context.Context, id uuid.UUID) error
}

type SuiteStore interface {
	CreateSuite(ctx context.Context, s *Suite) error
	GetSuite(ctx context.Context, id uuid.UUID) (Suite, error)
	GetSuiteBySlug(ctx context.Context, projectID uuid.UUID, slug string) (Suite, error)
	ListSuites(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[Suite], error)
	UpdateSuite(ctx context.Context, s *Suite) error
	DeleteSuite(ctx context.Context, id uuid.UUID) error

	CreateGate(ctx context.Context, g *Gate) error
	GetGate(ctx context.Context, id uuid.UUID) (Gate, error)
	ListGates(ctx context.Context, suiteID uuid.UUID) ([]Gate, error)
	UpdateGate(ctx context.Context, g *Gate) error
	DeleteGate(ctx context.Context, id uuid.UUID) error

	CreateTrigger(ctx context.Context, t *Trigger) error
	GetTrigger(ctx context.Context, id uuid.UUID) (Trigger, error)
	ListTriggers(ctx context.Context, suiteID uuid.UUID) ([]Trigger, error)
	UpdateTrigger(ctx context.Context, t *Trigger) error
	DeleteTrigger(ctx context.Context, id uuid.UUID) error
	// ListDueTriggers returns enabled schedule triggers with next_fire_at <= now, oldest first.
	ListDueTriggers(ctx context.Context, now time.Time, limit int) ([]Trigger, error)
	// MarkTriggerFired records a fire and the next fire time. It only succeeds (true) if the
	// trigger's next_fire_at still equals expectNext (optimistic, safe across replicas).
	MarkTriggerFired(ctx context.Context, id uuid.UUID, expectNext time.Time, firedAt time.Time, next *time.Time) (bool, error)
}

type RunStore interface {
	CreateRun(ctx context.Context, r *Run) error
	GetRun(ctx context.Context, id uuid.UUID) (Run, error)
	ListRuns(ctx context.Context, f RunFilter, p ListParams) (Page[Run], error)
	// UpdateRun saves all mutable fields of r.
	UpdateRun(ctx context.Context, r *Run) error
	// TransitionRun sets status when the current status is one of from; returns false otherwise.
	TransitionRun(ctx context.Context, id uuid.UUID, from []string, to string, at time.Time) (bool, error)
	// AddRunProgress atomically increments run counters.
	AddRunProgress(ctx context.Context, id uuid.UUID, d RunProgress) error
	DeleteRun(ctx context.Context, id uuid.UUID) error
	// LatestRun returns the most recent run matching f (e.g. last passing run on a target).
	LatestRun(ctx context.Context, f RunFilter) (Run, error)
}

type ResultStore interface {
	// InsertResults writes a batch of results and their scores (hot path: COPY on Postgres,
	// one batched transaction on SQLite). Results are upserted by (run_id, example_id, trial);
	// a re-delivered result replaces the previous one and its scores.
	InsertResults(ctx context.Context, results []Result, scores []Score) error
	// UpsertScores writes scores for existing results (re-score).
	UpsertScores(ctx context.Context, scores []Score) error
	GetResult(ctx context.Context, id uuid.UUID) (Result, error)
	// GetResults loads several results (with Scores) by id, in id order; unknown ids are skipped.
	GetResults(ctx context.Context, ids []uuid.UUID) ([]Result, error)
	// ListResults returns results (with Scores and Tags) ordered by id ascending.
	ListResults(ctx context.Context, f ResultFilter, p ListParams) (Page[Result], error)
	// IterateRunResults streams every result of a run with Scores and Tags, in id order.
	IterateRunResults(ctx context.Context, runID uuid.UUID, fn func(Result) error) error
	CountResults(ctx context.Context, runID uuid.UUID) (map[string]int, error)

	UpsertRunAggregates(ctx context.Context, aggs []RunAggregate) error
	ListRunAggregates(ctx context.Context, runID uuid.UUID) ([]RunAggregate, error)
	// ListRunAggregatesForRuns returns aggregates of several runs (trends, comparisons), filtered by
	// metric/slice when non-empty.
	ListRunAggregatesForRuns(ctx context.Context, runIDs []uuid.UUID, metric, sliceKey string) ([]RunAggregate, error)
}

type ReviewStore interface {
	CreateReviewQueue(ctx context.Context, q *ReviewQueue) error
	GetReviewQueue(ctx context.Context, id uuid.UUID) (ReviewQueue, error)
	ListReviewQueues(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[ReviewQueue], error)
	UpdateReviewQueue(ctx context.Context, q *ReviewQueue) error
	DeleteReviewQueue(ctx context.Context, id uuid.UUID) error

	// AddReviewItems inserts items, skipping results already in the queue. Returns inserted count.
	AddReviewItems(ctx context.Context, items []ReviewItem) (int, error)
	GetReviewItem(ctx context.Context, id uuid.UUID) (ReviewItem, error)
	ListReviewItems(ctx context.Context, f ReviewItemFilter, p ListParams) (Page[ReviewItem], error)
	UpdateReviewItem(ctx context.Context, it *ReviewItem) error

	CreateAnnotation(ctx context.Context, a *Annotation) error
	GetAnnotation(ctx context.Context, id uuid.UUID) (Annotation, error)
	ListAnnotations(ctx context.Context, f AnnotationFilter, p ListParams) (Page[Annotation], error)
	UpdateAnnotation(ctx context.Context, a *Annotation) error
	DeleteAnnotation(ctx context.Context, id uuid.UUID) error
}

type ComparisonStore interface {
	CreateComparison(ctx context.Context, c *Comparison) error
	GetComparison(ctx context.Context, id uuid.UUID) (Comparison, error)
	ListComparisons(ctx context.Context, projectID uuid.UUID, p ListParams) (Page[Comparison], error)
	DeleteComparison(ctx context.Context, id uuid.UUID) error
}

type NotificationStore interface {
	CreateNotificationChannel(ctx context.Context, c *NotificationChannel) error
	GetNotificationChannel(ctx context.Context, id uuid.UUID) (NotificationChannel, error)
	ListNotificationChannels(ctx context.Context, orgID uuid.UUID, p ListParams) (Page[NotificationChannel], error)
	UpdateNotificationChannel(ctx context.Context, c *NotificationChannel) error
	DeleteNotificationChannel(ctx context.Context, id uuid.UUID) error
}

type AuditStore interface {
	AppendAudit(ctx context.Context, e *AuditEntry) error
	ListAudit(ctx context.Context, orgID uuid.UUID, p ListParams) (Page[AuditEntry], error)
}

type CacheStore interface {
	CacheGet(ctx context.Context, key string) ([]byte, bool, error)
	// CachePut stores value; ttl <= 0 means no expiry.
	CachePut(ctx context.Context, key string, value []byte, ttl time.Duration) error
	CachePurgeExpired(ctx context.Context, now time.Time) (int, error)
}
