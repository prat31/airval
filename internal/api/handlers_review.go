package api

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/store"
)

// ---- review queues ----

func (s *Server) queue(ctx context.Context, id uuid.UUID, min store.Role) (store.ReviewQueue, error) {
	q, err := s.Store.GetReviewQueue(ctx, id)
	if err != nil {
		return q, err
	}
	return q, s.require(ctx, q.OrgID, &q.ProjectID, min)
}

func (s *Server) checkEvaluatorInProject(ctx context.Context, id *uuid.UUID, projectID uuid.UUID) error {
	if id == nil {
		return nil
	}
	e, err := s.Store.GetEvaluator(ctx, *id)
	if err != nil || e.ProjectID != projectID {
		return invalid("evaluator_id must be an evaluator in this project")
	}
	return nil
}

func (s *Server) ListReviewQueues(ctx context.Context, req gen.ListReviewQueuesRequestObject) (gen.ListReviewQueuesResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListReviewQueues(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListReviewQueues200JSONResponse{Items: mapSlice(pg.Items, toQueue), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateReviewQueue(ctx context.Context, req gen.CreateReviewQueueRequestObject) (gen.CreateReviewQueueResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	if err := s.checkEvaluatorInProject(ctx, b.EvaluatorId, p.ID); err != nil {
		return nil, err
	}
	q := store.ReviewQueue{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(b.Name), Description: trimmed(b.Description),
		EvaluatorID: b.EvaluatorId, Config: json.RawMessage(`{}`)}
	if b.Config != nil {
		if q.Config, err = jsonObject(*b.Config, "config"); err != nil {
			return nil, err
		}
	}
	if err := s.Store.CreateReviewQueue(ctx, &q); err != nil {
		return nil, err
	}
	return gen.CreateReviewQueue201JSONResponse(toQueue(q)), nil
}

func (s *Server) GetReviewQueue(ctx context.Context, req gen.GetReviewQueueRequestObject) (gen.GetReviewQueueResponseObject, error) {
	q, err := s.queue(ctx, req.Queue, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetReviewQueue200JSONResponse(toQueue(q)), nil
}

func (s *Server) UpdateReviewQueue(ctx context.Context, req gen.UpdateReviewQueueRequestObject) (gen.UpdateReviewQueueResponseObject, error) {
	q, err := s.queue(ctx, req.Queue, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			q.Name = strings.TrimSpace(*b.Name)
		}
		if b.Description != nil {
			q.Description = strings.TrimSpace(*b.Description)
		}
		if b.EvaluatorId != nil {
			if err := s.checkEvaluatorInProject(ctx, b.EvaluatorId, q.ProjectID); err != nil {
				return nil, err
			}
			q.EvaluatorID = b.EvaluatorId
		}
		if b.Config != nil {
			if q.Config, err = jsonObject(*b.Config, "config"); err != nil {
				return nil, err
			}
		}
	}
	if err := s.Store.UpdateReviewQueue(ctx, &q); err != nil {
		return nil, err
	}
	q, err = s.Store.GetReviewQueue(ctx, q.ID)
	if err != nil {
		return nil, err
	}
	return gen.UpdateReviewQueue200JSONResponse(toQueue(q)), nil
}

func (s *Server) DeleteReviewQueue(ctx context.Context, req gen.DeleteReviewQueueRequestObject) (gen.DeleteReviewQueueResponseObject, error) {
	q, err := s.queue(ctx, req.Queue, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteReviewQueue(ctx, q.ID); err != nil {
		return nil, err
	}
	return gen.DeleteReviewQueue204Response{}, nil
}

// ---- review items ----

func (s *Server) ListReviewItems(ctx context.Context, req gen.ListReviewItemsRequestObject) (gen.ListReviewItemsResponseObject, error) {
	if _, err := s.queue(ctx, req.Queue, store.RoleViewer); err != nil {
		return nil, err
	}
	f := store.ReviewItemFilter{QueueID: req.Queue, AssignedTo: req.Params.AssignedTo}
	if req.Params.Status != nil {
		for _, st := range *req.Params.Status {
			f.Statuses = append(f.Statuses, string(st))
		}
	}
	pg, err := s.Store.ListReviewItems(ctx, f, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	items := mapSlice(pg.Items, toReviewItem)
	if deref(req.Params.Expand, false) && len(pg.Items) > 0 {
		ids := mapSlice(pg.Items, func(it store.ReviewItem) uuid.UUID { return it.ResultID })
		results, err := s.Store.GetResults(ctx, ids)
		if err != nil {
			return nil, err
		}
		byID := make(map[uuid.UUID]store.Result, len(results))
		for _, r := range results {
			byID[r.ID] = r
		}
		for i := range items {
			if r, ok := byID[items[i].ResultId]; ok {
				gr := toResult(r, false)
				items[i].Result = &gr
			}
		}
	}
	return gen.ListReviewItems200JSONResponse{Items: items, NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) AddReviewItems(ctx context.Context, req gen.AddReviewItemsRequestObject) (gen.AddReviewItemsResponseObject, error) {
	q, err := s.queue(ctx, req.Queue, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || (b.ResultIds == nil && b.RunId == nil) {
		return nil, invalid("result_ids or run_id is required")
	}
	var results []store.Result
	if b.ResultIds != nil && len(*b.ResultIds) > 0 {
		if results, err = s.Store.GetResults(ctx, *b.ResultIds); err != nil {
			return nil, err
		}
	}
	if b.RunId != nil {
		f := store.ResultFilter{RunID: *b.RunId}
		if b.Select == nil || *b.Select == gen.Failures {
			f.Statuses = []string{store.ResultFailed, store.ResultErrored}
		}
		p := store.ListParams{Limit: store.MaxLimit}
		for {
			pg, err := s.Store.ListResults(ctx, f, p)
			if err != nil {
				return nil, err
			}
			results = append(results, pg.Items...)
			if pg.NextCursor == "" || len(results) > 100_000 {
				break
			}
			p.Cursor = pg.NextCursor
		}
	}
	runOK := map[uuid.UUID]bool{}
	items := make([]store.ReviewItem, 0, len(results))
	for _, r := range results {
		ok, seen := runOK[r.RunID]
		if !seen {
			run, err := s.Store.GetRun(ctx, r.RunID)
			ok = err == nil && run.ProjectID == q.ProjectID
			runOK[r.RunID] = ok
		}
		if !ok {
			return nil, invalid("result %s is not in this queue's project", r.ID)
		}
		items = append(items, store.ReviewItem{OrgID: q.OrgID, ProjectID: q.ProjectID, QueueID: q.ID, RunID: r.RunID, ResultID: r.ID})
	}
	n, err := s.Store.AddReviewItems(ctx, items)
	if err != nil {
		return nil, err
	}
	return gen.AddReviewItems200JSONResponse{Count: n}, nil
}

func (s *Server) reviewItem(ctx context.Context, id uuid.UUID, min store.Role) (store.ReviewItem, error) {
	it, err := s.Store.GetReviewItem(ctx, id)
	if err != nil {
		return it, err
	}
	return it, s.require(ctx, it.OrgID, &it.ProjectID, min)
}

func (s *Server) GetReviewItem(ctx context.Context, req gen.GetReviewItemRequestObject) (gen.GetReviewItemResponseObject, error) {
	it, err := s.reviewItem(ctx, req.Item, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	out := toReviewItem(it)
	if r, err := s.Store.GetResult(ctx, it.ResultID); err == nil {
		gr := toResult(r, true)
		out.Result = &gr
	}
	return gen.GetReviewItem200JSONResponse(out), nil
}

var reviewStatuses = set(store.ReviewPending, store.ReviewInProgress, store.ReviewDone, store.ReviewSkipped)

func (s *Server) UpdateReviewItem(ctx context.Context, req gen.UpdateReviewItemRequestObject) (gen.UpdateReviewItemResponseObject, error) {
	it, err := s.reviewItem(ctx, req.Item, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Status != nil {
			st := string(*b.Status)
			if !reviewStatuses[st] {
				return nil, invalid("unknown status %q", st)
			}
			it.Status = st
			if st == store.ReviewDone || st == store.ReviewSkipped {
				it.CompletedBy, it.CompletedAt = s.principal(ctx).UserID(), ptr(time.Now().UTC())
			} else {
				it.CompletedBy, it.CompletedAt = nil, nil
			}
		}
		if b.AssignedTo != nil {
			if _, err := s.Store.GetMembership(ctx, it.OrgID, *b.AssignedTo); err != nil {
				return nil, invalid("assigned_to must be a member of the organization")
			}
			it.AssignedTo = b.AssignedTo
		}
	}
	if err := s.Store.UpdateReviewItem(ctx, &it); err != nil {
		return nil, err
	}
	return gen.UpdateReviewItem200JSONResponse(toReviewItem(it)), nil
}

// ---- annotations ----

func (s *Server) annotation(ctx context.Context, id uuid.UUID, min store.Role) (store.Annotation, error) {
	a, err := s.Store.GetAnnotation(ctx, id)
	if err != nil {
		return a, err
	}
	return a, s.require(ctx, a.OrgID, &a.ProjectID, min)
}

func (s *Server) ListAnnotations(ctx context.Context, req gen.ListAnnotationsRequestObject) (gen.ListAnnotationsResponseObject, error) {
	p := req.Params
	f := store.AnnotationFilter{ProjectID: p.ProjectId, ResultID: p.ResultId, ReviewItemID: p.ReviewItemId, EvaluatorID: p.EvaluatorId}
	if p.ProjectId != nil {
		if _, err := s.project(ctx, *p.ProjectId, store.RoleViewer); err != nil {
			return nil, err
		}
	} else {
		orgs, onlyProject, err := s.Authz.OrgIDs(ctx, s.principal(ctx))
		if err != nil {
			return nil, err
		}
		if len(orgs) == 0 {
			return gen.ListAnnotations200JSONResponse{Items: []gen.Annotation{}}, nil
		}
		f.OrgIDs, f.ProjectID = orgs, onlyProject
	}
	pg, err := s.Store.ListAnnotations(ctx, f, listParams(p.Cursor, p.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListAnnotations200JSONResponse{Items: mapSlice(pg.Items, toAnnotation), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func validScore(v *float64) error {
	if v != nil && (*v < 0 || *v > 1) {
		return invalid("value must be between 0 and 1")
	}
	return nil
}

func (s *Server) CreateAnnotation(ctx context.Context, req gen.CreateAnnotationRequestObject) (gen.CreateAnnotationResponseObject, error) {
	b := req.Body
	if b == nil {
		return nil, invalid("result_id is required")
	}
	res, err := s.Store.GetResult(ctx, b.ResultId)
	if err != nil {
		return nil, err
	}
	run, err := s.run(ctx, res.RunID, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := validScore(b.Value); err != nil {
		return nil, err
	}
	if err := s.checkEvaluatorInProject(ctx, b.EvaluatorId, run.ProjectID); err != nil {
		return nil, err
	}
	a := store.Annotation{OrgID: run.OrgID, ProjectID: run.ProjectID, ResultID: res.ID, ReviewItemID: b.ReviewItemId,
		UserID: s.principal(ctx).UserID(), EvaluatorID: b.EvaluatorId, Value: b.Value, Label: trimmed(b.Label),
		Passed: b.Passed, Comment: strings.TrimSpace(deref(b.Comment, ""))}
	err = s.Store.WithTx(ctx, func(tx store.Store) error {
		if b.ReviewItemId != nil {
			it, err := tx.GetReviewItem(ctx, *b.ReviewItemId)
			if err != nil || it.ResultID != res.ID {
				return invalid("review_item_id does not match result_id")
			}
			if deref(b.CompleteItem, true) {
				it.Status, it.CompletedBy, it.CompletedAt = store.ReviewDone, a.UserID, ptr(time.Now().UTC())
				if err := tx.UpdateReviewItem(ctx, &it); err != nil {
					return err
				}
			}
		}
		return tx.CreateAnnotation(ctx, &a)
	})
	if err != nil {
		return nil, err
	}
	return gen.CreateAnnotation201JSONResponse(toAnnotation(a)), nil
}

func (s *Server) GetAnnotation(ctx context.Context, req gen.GetAnnotationRequestObject) (gen.GetAnnotationResponseObject, error) {
	a, err := s.annotation(ctx, req.Annotation, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetAnnotation200JSONResponse(toAnnotation(a)), nil
}

func (s *Server) UpdateAnnotation(ctx context.Context, req gen.UpdateAnnotationRequestObject) (gen.UpdateAnnotationResponseObject, error) {
	a, err := s.annotation(ctx, req.Annotation, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if err := validScore(b.Value); err != nil {
			return nil, err
		}
		if b.EvaluatorId != nil {
			if err := s.checkEvaluatorInProject(ctx, b.EvaluatorId, a.ProjectID); err != nil {
				return nil, err
			}
			a.EvaluatorID = b.EvaluatorId
		}
		if b.Value != nil {
			a.Value = b.Value
		}
		if b.Label != nil {
			a.Label = strings.TrimSpace(*b.Label)
		}
		if b.Passed != nil {
			a.Passed = b.Passed
		}
		if b.Comment != nil {
			a.Comment = strings.TrimSpace(*b.Comment)
		}
	}
	if err := s.Store.UpdateAnnotation(ctx, &a); err != nil {
		return nil, err
	}
	return gen.UpdateAnnotation200JSONResponse(toAnnotation(a)), nil
}

func (s *Server) DeleteAnnotation(ctx context.Context, req gen.DeleteAnnotationRequestObject) (gen.DeleteAnnotationResponseObject, error) {
	a, err := s.annotation(ctx, req.Annotation, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteAnnotation(ctx, a.ID); err != nil {
		return nil, err
	}
	return gen.DeleteAnnotation204Response{}, nil
}
