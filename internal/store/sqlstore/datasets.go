package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/store"
)

// chunkSize bounds IN lists and multi-row statements (well below both dialects' parameter limits).
const chunkSize = 500

func chunks[T any](v []T, n int) [][]T {
	var out [][]T
	for len(v) > n {
		out = append(out, v[:n])
		v = v[n:]
	}
	if len(v) > 0 {
		out = append(out, v)
	}
	return out
}

// ---- datasets ----

const datasetSelect = `SELECT d.id, d.org_id, d.project_id, d.name, d.description, d.schema, d.latest_version_id,
	d.created_at, d.updated_at,
	(SELECT count(*) FROM examples e WHERE e.dataset_id = d.id AND e.deleted_at IS NULL),
	v.number, v.created_at
	FROM datasets d LEFT JOIN dataset_versions v ON v.id = d.latest_version_id`

func scanDataset(r scanner) (store.Dataset, error) {
	var d store.Dataset
	err := r.Scan(&d.ID, &d.OrgID, &d.ProjectID, &d.Name, &d.Description, rawJSON{&d.Schema}, &d.LatestVersionID,
		&d.CreatedAt, &d.UpdatedAt, &d.ExampleCount, &d.LatestVersionNumber, &d.LatestVersionAt)
	return d, err
}

func (s *Store) CreateDataset(ctx context.Context, d *store.Dataset) error {
	if d.ID == uuid.Nil {
		d.ID = store.NewID()
	}
	stampTimes(&d.CreatedAt, &d.UpdatedAt)
	return s.exec(ctx, `INSERT INTO datasets (id, org_id, project_id, name, description, schema, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		d.ID, d.OrgID, d.ProjectID, d.Name, d.Description, jsonArg(d.Schema, ""), d.CreatedAt, d.UpdatedAt)
}

func (s *Store) GetDataset(ctx context.Context, id uuid.UUID) (store.Dataset, error) {
	return one(ctx, s, datasetSelect+` WHERE d.id = $1`, []any{id}, scanDataset)
}

func (s *Store) ListDatasets(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.Dataset], error) {
	b := &qb{}
	b.where("d.project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("d.id", p, true)
	if err != nil {
		return store.Page[store.Dataset]{}, err
	}
	items, err := collect(ctx, s, datasetSelect+b.whereSQL()+tail, b.args, scanDataset)
	if err != nil {
		return store.Page[store.Dataset]{}, err
	}
	return finishPage(items, limit, func(d store.Dataset) uuid.UUID { return d.ID }), nil
}

// UpdateDataset saves name/description/schema. It does not bump UpdatedAt, which tracks draft
// content changes (see Dataset.HasDraftChanges).
func (s *Store) UpdateDataset(ctx context.Context, d *store.Dataset) error {
	return s.execAffect(ctx, `UPDATE datasets SET name = $2, description = $3, schema = $4 WHERE id = $1`,
		d.ID, d.Name, d.Description, jsonArg(d.Schema, ""))
}

func (s *Store) DeleteDataset(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM datasets WHERE id = $1`, id)
}

// ---- examples ----

const exampleCols = `e.id, e.org_id, e.project_id, e.dataset_id, r.id, r.input, r.expected, r.context, r.metadata,
	r.tags, r.conversation, e.created_at, r.created_at`

func scanExample(r scanner) (store.Example, error) {
	var e store.Example
	err := r.Scan(&e.ID, &e.OrgID, &e.ProjectID, &e.DatasetID, &e.RevisionID, rawJSON{&e.Input}, rawJSON{&e.Expected},
		rawJSON{&e.Context}, rawJSON{&e.Metadata}, jsonCol(&e.Tags), rawJSON{&e.Conversation}, &e.CreatedAt, &e.UpdatedAt)
	if e.Tags == nil {
		e.Tags = []string{}
	}
	return e, err
}

// canon returns a canonical encoding of a JSON value for change detection.
func canon(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	b, _ := json.Marshal(v)
	return b
}

func sameData(a, b store.ExampleData) bool {
	return bytes.Equal(canon(a.Input), canon(b.Input)) && bytes.Equal(canon(a.Expected), canon(b.Expected)) &&
		bytes.Equal(canon(a.Context), canon(b.Context)) && bytes.Equal(canon(a.Metadata), canon(b.Metadata)) &&
		bytes.Equal(canon(a.Conversation), canon(b.Conversation)) && slices.Equal(strs(a.Tags), strs(b.Tags))
}

func (s *Store) UpsertExamples(ctx context.Context, datasetID uuid.UUID, items []store.ExampleUpsert) ([]store.Example, error) {
	out := make([]store.Example, len(items))
	err := s.withTx(ctx, func(tx *Store) error {
		var orgID, projectID uuid.UUID
		if err := tx.q.QueryRowContext(ctx, `SELECT org_id, project_id FROM datasets WHERE id = $1`, datasetID).
			Scan(&orgID, &projectID); err != nil {
			return tx.mapErr(err)
		}
		// Load existing examples (including soft-deleted, which get revived).
		var ids []uuid.UUID
		for _, it := range items {
			if it.ID != nil {
				ids = append(ids, *it.ID)
			}
		}
		existing := map[uuid.UUID]existingExample{}
		for _, c := range chunks(ids, chunkSize) {
			b := &qb{}
			b.where("e.dataset_id = " + b.arg(datasetID))
			b.where(inList(b, "e.id", c))
			rows, err := collect(ctx, tx, `SELECT `+exampleCols+`, e.deleted_at IS NOT NULL
				FROM examples e JOIN example_revisions r ON r.id = e.revision_id`+b.whereSQL(), b.args,
				func(r scanner) (existingExample, error) {
					var x existingExample
					e := &x.Example
					err := r.Scan(&e.ID, &e.OrgID, &e.ProjectID, &e.DatasetID, &e.RevisionID, rawJSON{&e.Input},
						rawJSON{&e.Expected}, rawJSON{&e.Context}, rawJSON{&e.Metadata}, jsonCol(&e.Tags),
						rawJSON{&e.Conversation}, &e.CreatedAt, &e.UpdatedAt, &x.deleted)
					return x, err
				})
			if err != nil {
				return err
			}
			for _, x := range rows {
				existing[x.ID] = x
			}
		}

		insEx, err := tx.q.PrepareContext(ctx, `INSERT INTO examples (id, org_id, project_id, dataset_id, revision_id, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$6)`)
		if err != nil {
			return err
		}
		insRev, err := tx.q.PrepareContext(ctx, `INSERT INTO example_revisions (id, example_id, dataset_id, input, expected,
			context, metadata, tags, conversation, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`)
		if err != nil {
			closeStmts(insEx)
			return err
		}
		updEx, err := tx.q.PrepareContext(ctx, `UPDATE examples SET revision_id = $2, deleted_at = NULL, updated_at = $3 WHERE id = $1`)
		if err != nil {
			closeStmts(insEx, insRev)
			return err
		}
		defer closeStmts(insEx, insRev, updEx)

		t := now()
		changed := false
		for i, it := range items {
			if len(it.Input) == 0 {
				it.Input = json.RawMessage(`{}`)
			}
			it.Tags = strs(it.Tags)
			if it.ID != nil {
				if x, ok := existing[*it.ID]; ok {
					if sameData(x.ExampleData, it.ExampleData) {
						if x.deleted {
							if _, err := updEx.ExecContext(ctx, x.ID, x.RevisionID, t); err != nil {
								return tx.mapErr(err)
							}
							changed = true
						}
						out[i] = x.Example
						continue
					}
					rev := store.NewID()
					if err := insertRevision(ctx, insRev, rev, x.ID, datasetID, it.ExampleData, t); err != nil {
						return tx.mapErr(err)
					}
					if _, err := updEx.ExecContext(ctx, x.ID, rev, t); err != nil {
						return tx.mapErr(err)
					}
					changed = true
					out[i] = store.Example{ID: x.ID, OrgID: orgID, ProjectID: projectID, DatasetID: datasetID,
						RevisionID: rev, ExampleData: it.ExampleData, CreatedAt: x.CreatedAt, UpdatedAt: t}
					existing[x.ID] = existingExample{Example: out[i]}
					continue
				}
			}
			id := store.NewID()
			if it.ID != nil {
				id = *it.ID
			}
			rev := store.NewID()
			if _, err := insEx.ExecContext(ctx, id, orgID, projectID, datasetID, rev, t); err != nil {
				return tx.mapErr(err)
			}
			if err := insertRevision(ctx, insRev, rev, id, datasetID, it.ExampleData, t); err != nil {
				return tx.mapErr(err)
			}
			changed = true
			out[i] = store.Example{ID: id, OrgID: orgID, ProjectID: projectID, DatasetID: datasetID, RevisionID: rev,
				ExampleData: it.ExampleData, CreatedAt: t, UpdatedAt: t}
			existing[id] = existingExample{Example: out[i]}
		}
		if changed {
			return tx.exec(ctx, `UPDATE datasets SET updated_at = $2 WHERE id = $1`, datasetID, t)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type existingExample struct {
	store.Example
	deleted bool
}

func insertRevision(ctx context.Context, st *sql.Stmt, rev, exampleID, datasetID uuid.UUID, d store.ExampleData, t time.Time) error {
	_, err := st.ExecContext(ctx, rev, exampleID, datasetID, jsonArg(d.Input, "{}"), jsonArg(d.Expected, ""),
		jsonArg(d.Context, ""), jsonArg(d.Metadata, ""), jsonValue(strs(d.Tags)), jsonArg(d.Conversation, ""), t)
	return err
}

func (s *Store) DeleteExamples(ctx context.Context, datasetID uuid.UUID, ids []uuid.UUID) (int, error) {
	total := 0
	err := s.withTx(ctx, func(tx *Store) error {
		t := now()
		for _, c := range chunks(ids, chunkSize) {
			b := &qb{}
			b.where("dataset_id = " + b.arg(datasetID))
			b.where("deleted_at IS NULL")
			b.where(inList(b, "id", c))
			ta := b.arg(t)
			res, err := tx.q.ExecContext(ctx, `UPDATE examples SET deleted_at = `+ta+`, updated_at = `+ta+b.whereSQL(), b.args...)
			if err != nil {
				return tx.mapErr(err)
			}
			n, _ := res.RowsAffected()
			total += int(n)
		}
		if total > 0 {
			return tx.exec(ctx, `UPDATE datasets SET updated_at = $2 WHERE id = $1`, datasetID, t)
		}
		return nil
	})
	return total, err
}

func (s *Store) GetExample(ctx context.Context, id uuid.UUID) (store.Example, error) {
	return one(ctx, s, `SELECT `+exampleCols+` FROM examples e JOIN example_revisions r ON r.id = e.revision_id
		WHERE e.id = $1 AND e.deleted_at IS NULL`, []any{id}, scanExample)
}

func (s *Store) ListExamples(ctx context.Context, f store.ExampleFilter, p store.ListParams) (store.Page[store.Example], error) {
	b := &qb{}
	var from string
	if f.VersionID != nil {
		from = ` FROM dataset_version_items vi JOIN examples e ON e.id = vi.example_id
			JOIN example_revisions r ON r.id = vi.revision_id`
		b.where("vi.version_id = " + b.arg(*f.VersionID))
	} else {
		from = ` FROM examples e JOIN example_revisions r ON r.id = e.revision_id`
		b.where("e.dataset_id = " + b.arg(f.DatasetID))
		b.where("e.deleted_at IS NULL")
	}
	if f.Tag != "" {
		b.where(s.d.jsonArrayContains("r.tags", b.arg(f.Tag)))
	}
	if f.Search != "" {
		ph := b.arg("%" + f.Search + "%")
		b.where("(" + s.d.jsonText("r.input") + " " + s.d.like() + " " + ph + " OR " +
			s.d.jsonText("r.expected") + " " + s.d.like() + " " + ph + " OR " +
			s.d.jsonText("r.metadata") + " " + s.d.like() + " " + ph + ")")
	}
	if len(f.IDs) > 0 {
		b.where(inList(b, "e.id", f.IDs))
	}
	tail, limit, err := b.paginate("e.id", p, false)
	if err != nil {
		return store.Page[store.Example]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+exampleCols+from+b.whereSQL()+tail, b.args, scanExample)
	if err != nil {
		return store.Page[store.Example]{}, err
	}
	return finishPage(items, limit, func(e store.Example) uuid.UUID { return e.ID }), nil
}

func (s *Store) GetExampleRevisions(ctx context.Context, revisionIDs []uuid.UUID) ([]store.Example, error) {
	out := make([]store.Example, 0, len(revisionIDs))
	for _, c := range chunks(revisionIDs, chunkSize) {
		b := &qb{}
		b.where(inList(b, "r.id", c))
		rows, err := collect(ctx, s, `SELECT `+exampleCols+` FROM example_revisions r JOIN examples e ON e.id = r.example_id`+
			b.whereSQL(), b.args, scanExample)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// ---- dataset versions ----

const versionCols = "id, org_id, project_id, dataset_id, number, note, example_count, created_by, created_at"

func scanVersion(r scanner) (store.DatasetVersion, error) {
	var v store.DatasetVersion
	err := r.Scan(&v.ID, &v.OrgID, &v.ProjectID, &v.DatasetID, &v.Number, &v.Note, &v.ExampleCount, &v.CreatedBy, &v.CreatedAt)
	return v, err
}

func (s *Store) PublishDatasetVersion(ctx context.Context, datasetID uuid.UUID, note string, createdBy *uuid.UUID) (store.DatasetVersion, error) {
	var v store.DatasetVersion
	err := s.withTx(ctx, func(tx *Store) error {
		// Row-lock the dataset on Postgres (no-op update), serializing concurrent publishes.
		if err := tx.execAffect(ctx, `UPDATE datasets SET latest_version_id = latest_version_id WHERE id = $1`, datasetID); err != nil {
			return err
		}
		if err := tx.q.QueryRowContext(ctx, `SELECT org_id, project_id,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM dataset_versions WHERE dataset_id = $1)
			FROM datasets WHERE id = $1`, datasetID).Scan(&v.OrgID, &v.ProjectID, &v.Number); err != nil {
			return tx.mapErr(err)
		}
		v.ID, v.DatasetID, v.Note, v.CreatedBy, v.CreatedAt = store.NewID(), datasetID, note, createdBy, now()
		if err := tx.exec(ctx, `INSERT INTO dataset_versions (`+versionCols+`) VALUES ($1,$2,$3,$4,$5,$6,0,$7,$8)`,
			v.ID, v.OrgID, v.ProjectID, v.DatasetID, v.Number, v.Note, v.CreatedBy, v.CreatedAt); err != nil {
			return err
		}
		res, err := tx.q.ExecContext(ctx, `INSERT INTO dataset_version_items (version_id, example_id, revision_id)
			SELECT $1, id, revision_id FROM examples WHERE dataset_id = $2 AND deleted_at IS NULL`, v.ID, datasetID)
		if err != nil {
			return tx.mapErr(err)
		}
		n, _ := res.RowsAffected()
		v.ExampleCount = int(n)
		if err := tx.exec(ctx, `UPDATE dataset_versions SET example_count = $2 WHERE id = $1`, v.ID, v.ExampleCount); err != nil {
			return err
		}
		return tx.exec(ctx, `UPDATE datasets SET latest_version_id = $2 WHERE id = $1`, datasetID, v.ID)
	})
	return v, err
}

func (s *Store) GetDatasetVersion(ctx context.Context, id uuid.UUID) (store.DatasetVersion, error) {
	return one(ctx, s, `SELECT `+versionCols+` FROM dataset_versions WHERE id = $1`, []any{id}, scanVersion)
}

func (s *Store) GetDatasetVersionByNumber(ctx context.Context, datasetID uuid.UUID, number int) (store.DatasetVersion, error) {
	return one(ctx, s, `SELECT `+versionCols+` FROM dataset_versions WHERE dataset_id = $1 AND number = $2`,
		[]any{datasetID, number}, scanVersion)
}

func (s *Store) ListDatasetVersions(ctx context.Context, datasetID uuid.UUID, p store.ListParams) (store.Page[store.DatasetVersion], error) {
	b := &qb{}
	b.where("dataset_id = " + b.arg(datasetID))
	tail, limit, err := b.paginate("id", p, true)
	if err != nil {
		return store.Page[store.DatasetVersion]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+versionCols+` FROM dataset_versions`+b.whereSQL()+tail, b.args, scanVersion)
	if err != nil {
		return store.Page[store.DatasetVersion]{}, err
	}
	return finishPage(items, limit, func(v store.DatasetVersion) uuid.UUID { return v.ID }), nil
}

func (s *Store) ListDatasetVersionItems(ctx context.Context, versionID uuid.UUID) ([]store.VersionItem, error) {
	return collect(ctx, s, `SELECT example_id, revision_id FROM dataset_version_items WHERE version_id = $1 ORDER BY example_id`,
		[]any{versionID}, func(r scanner) (store.VersionItem, error) {
			var it store.VersionItem
			err := r.Scan(&it.ExampleID, &it.RevisionID)
			return it, err
		})
}
