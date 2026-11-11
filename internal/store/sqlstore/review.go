package sqlstore

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// ---- review queues ----

const queueSelect = `SELECT q.id, q.org_id, q.project_id, q.name, q.description, q.evaluator_id, q.config,
	q.created_at, q.updated_at,
	(SELECT count(*) FROM review_items i WHERE i.queue_id = q.id AND i.status IN ('pending', 'in_progress')),
	(SELECT count(*) FROM review_items i WHERE i.queue_id = q.id AND i.status = 'done')
	FROM review_queues q`

func scanQueue(r scanner) (store.ReviewQueue, error) {
	var q store.ReviewQueue
	err := r.Scan(&q.ID, &q.OrgID, &q.ProjectID, &q.Name, &q.Description, &q.EvaluatorID, rawJSON{&q.Config},
		&q.CreatedAt, &q.UpdatedAt, &q.PendingCount, &q.DoneCount)
	return q, err
}

func (s *Store) CreateReviewQueue(ctx context.Context, q *store.ReviewQueue) error {
	if q.ID == uuid.Nil {
		q.ID = store.NewID()
	}
	stampTimes(&q.CreatedAt, &q.UpdatedAt)
	return s.exec(ctx, `INSERT INTO review_queues (id, org_id, project_id, name, description, evaluator_id, config,
		created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		q.ID, q.OrgID, q.ProjectID, q.Name, q.Description, q.EvaluatorID, jsonArg(q.Config, "{}"), q.CreatedAt, q.UpdatedAt)
}

func (s *Store) GetReviewQueue(ctx context.Context, id uuid.UUID) (store.ReviewQueue, error) {
	return one(ctx, s, queueSelect+` WHERE q.id = $1`, []any{id}, scanQueue)
}

func (s *Store) ListReviewQueues(ctx context.Context, projectID uuid.UUID, p store.ListParams) (store.Page[store.ReviewQueue], error) {
	b := &qb{}
	b.where("q.project_id = " + b.arg(projectID))
	tail, limit, err := b.paginate("q.id", p, false)
	if err != nil {
		return store.Page[store.ReviewQueue]{}, err
	}
	items, err := collect(ctx, s, queueSelect+b.whereSQL()+tail, b.args, scanQueue)
	if err != nil {
		return store.Page[store.ReviewQueue]{}, err
	}
	return finishPage(items, limit, func(q store.ReviewQueue) uuid.UUID { return q.ID }), nil
}

func (s *Store) UpdateReviewQueue(ctx context.Context, q *store.ReviewQueue) error {
	stampTimes(nil, &q.UpdatedAt)
	return s.execAffect(ctx, `UPDATE review_queues SET name = $2, description = $3, evaluator_id = $4, config = $5,
		updated_at = $6 WHERE id = $1`, q.ID, q.Name, q.Description, q.EvaluatorID, jsonArg(q.Config, "{}"), q.UpdatedAt)
}

func (s *Store) DeleteReviewQueue(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM review_queues WHERE id = $1`, id)
}

// ---- review items ----

var reviewItemCols = []string{"id", "org_id", "project_id", "queue_id", "run_id", "result_id", "status", "assigned_to",
	"completed_by", "completed_at", "created_at", "updated_at"}

func scanReviewItem(r scanner) (store.ReviewItem, error) {
	var it store.ReviewItem
	err := r.Scan(&it.ID, &it.OrgID, &it.ProjectID, &it.QueueID, &it.RunID, &it.ResultID, &it.Status, &it.AssignedTo,
		&it.CompletedBy, &it.CompletedAt, &it.CreatedAt, &it.UpdatedAt)
	return it, err
}

func (s *Store) AddReviewItems(ctx context.Context, items []store.ReviewItem) (int, error) {
	total := 0
	err := s.withTx(ctx, func(tx *Store) error {
		t := now()
		for _, c := range chunks(items, 300) {
			args := make([]any, 0, len(c)*len(reviewItemCols))
			for i := range c {
				it := &c[i]
				if it.ID == uuid.Nil {
					it.ID = store.NewID()
				}
				if it.Status == "" {
					it.Status = store.ReviewPending
				}
				it.CreatedAt, it.UpdatedAt = t, t
				args = append(args, it.ID, it.OrgID, it.ProjectID, it.QueueID, it.RunID, it.ResultID, it.Status,
					it.AssignedTo, it.CompletedBy, utcPtr(it.CompletedAt), it.CreatedAt, it.UpdatedAt)
			}
			res, err := tx.q.ExecContext(ctx, `INSERT INTO review_items (`+strings.Join(reviewItemCols, ",")+`) VALUES `+
				valuesSQL(len(c), len(reviewItemCols))+` ON CONFLICT (queue_id, result_id) DO NOTHING`, args...)
			if err != nil {
				return tx.mapErr(err)
			}
			n, _ := res.RowsAffected()
			total += int(n)
		}
		return nil
	})
	return total, err
}

func (s *Store) GetReviewItem(ctx context.Context, id uuid.UUID) (store.ReviewItem, error) {
	return one(ctx, s, `SELECT `+strings.Join(reviewItemCols, ",")+` FROM review_items WHERE id = $1`, []any{id}, scanReviewItem)
}

func (s *Store) ListReviewItems(ctx context.Context, f store.ReviewItemFilter, p store.ListParams) (store.Page[store.ReviewItem], error) {
	b := &qb{}
	b.where("queue_id = " + b.arg(f.QueueID))
	if len(f.Statuses) > 0 {
		b.where(inList(b, "status", f.Statuses))
	}
	if f.AssignedTo != nil {
		b.where("assigned_to = " + b.arg(*f.AssignedTo))
	}
	tail, limit, err := b.paginate("id", p, false)
	if err != nil {
		return store.Page[store.ReviewItem]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+strings.Join(reviewItemCols, ",")+` FROM review_items`+b.whereSQL()+tail,
		b.args, scanReviewItem)
	if err != nil {
		return store.Page[store.ReviewItem]{}, err
	}
	return finishPage(items, limit, func(it store.ReviewItem) uuid.UUID { return it.ID }), nil
}

func (s *Store) UpdateReviewItem(ctx context.Context, it *store.ReviewItem) error {
	stampTimes(nil, &it.UpdatedAt)
	return s.execAffect(ctx, `UPDATE review_items SET status = $2, assigned_to = $3, completed_by = $4, completed_at = $5,
		updated_at = $6 WHERE id = $1`, it.ID, it.Status, it.AssignedTo, it.CompletedBy, utcPtr(it.CompletedAt), it.UpdatedAt)
}

// ---- annotations ----

const annotationCols = `id, org_id, project_id, result_id, review_item_id, user_id, evaluator_id, value, label, passed,
	comment, created_at, updated_at`

func scanAnnotation(r scanner) (store.Annotation, error) {
	var a store.Annotation
	err := r.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.ResultID, &a.ReviewItemID, &a.UserID, &a.EvaluatorID, &a.Value,
		&a.Label, &a.Passed, &a.Comment, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (s *Store) CreateAnnotation(ctx context.Context, a *store.Annotation) error {
	if a.ID == uuid.Nil {
		a.ID = store.NewID()
	}
	stampTimes(&a.CreatedAt, &a.UpdatedAt)
	return s.exec(ctx, `INSERT INTO annotations (`+annotationCols+`) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.OrgID, a.ProjectID, a.ResultID, a.ReviewItemID, a.UserID, a.EvaluatorID, a.Value, a.Label, a.Passed,
		a.Comment, a.CreatedAt, a.UpdatedAt)
}

func (s *Store) GetAnnotation(ctx context.Context, id uuid.UUID) (store.Annotation, error) {
	return one(ctx, s, `SELECT `+annotationCols+` FROM annotations WHERE id = $1`, []any{id}, scanAnnotation)
}

func (s *Store) ListAnnotations(ctx context.Context, f store.AnnotationFilter, p store.ListParams) (store.Page[store.Annotation], error) {
	b := &qb{}
	if f.ProjectID != nil {
		b.where("project_id = " + b.arg(*f.ProjectID))
	}
	if len(f.OrgIDs) > 0 {
		b.where(inList(b, "org_id", f.OrgIDs))
	}
	if f.ResultID != nil {
		b.where("result_id = " + b.arg(*f.ResultID))
	}
	if f.ReviewItemID != nil {
		b.where("review_item_id = " + b.arg(*f.ReviewItemID))
	}
	if f.EvaluatorID != nil {
		b.where("evaluator_id = " + b.arg(*f.EvaluatorID))
	}
	tail, limit, err := b.paginate("id", p, false)
	if err != nil {
		return store.Page[store.Annotation]{}, err
	}
	items, err := collect(ctx, s, `SELECT `+annotationCols+` FROM annotations`+b.whereSQL()+tail, b.args, scanAnnotation)
	if err != nil {
		return store.Page[store.Annotation]{}, err
	}
	return finishPage(items, limit, func(a store.Annotation) uuid.UUID { return a.ID }), nil
}

func (s *Store) UpdateAnnotation(ctx context.Context, a *store.Annotation) error {
	stampTimes(nil, &a.UpdatedAt)
	return s.execAffect(ctx, `UPDATE annotations SET evaluator_id = $2, value = $3, label = $4, passed = $5, comment = $6,
		updated_at = $7 WHERE id = $1`, a.ID, a.EvaluatorID, a.Value, a.Label, a.Passed, a.Comment, a.UpdatedAt)
}

func (s *Store) DeleteAnnotation(ctx context.Context, id uuid.UUID) error {
	return s.execAffect(ctx, `DELETE FROM annotations WHERE id = $1`, id)
}
