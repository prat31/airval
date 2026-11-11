package api

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/store"
)

func (s *Server) dataset(ctx context.Context, id uuid.UUID, min store.Role) (store.Dataset, error) {
	d, err := s.Store.GetDataset(ctx, id)
	if err != nil {
		return d, err
	}
	return d, s.require(ctx, d.OrgID, &d.ProjectID, min)
}

func (s *Server) ListDatasets(ctx context.Context, req gen.ListDatasetsRequestObject) (gen.ListDatasetsResponseObject, error) {
	if _, err := s.project(ctx, req.Project, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListDatasets(ctx, req.Project, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListDatasets200JSONResponse{Items: mapSlice(pg.Items, toDataset), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateDataset(ctx context.Context, req gen.CreateDatasetRequestObject) (gen.CreateDatasetResponseObject, error) {
	p, err := s.project(ctx, req.Project, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	d := store.Dataset{OrgID: p.OrgID, ProjectID: p.ID, Name: strings.TrimSpace(b.Name), Description: trimmed(b.Description)}
	if b.Schema != nil {
		d.Schema = *b.Schema
	}
	if err := s.Store.CreateDataset(ctx, &d); err != nil {
		return nil, err
	}
	return gen.CreateDataset201JSONResponse(toDataset(d)), nil
}

func (s *Server) GetDataset(ctx context.Context, req gen.GetDatasetRequestObject) (gen.GetDatasetResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	return gen.GetDataset200JSONResponse(toDataset(d)), nil
}

func (s *Server) UpdateDataset(ctx context.Context, req gen.UpdateDatasetRequestObject) (gen.UpdateDatasetResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			d.Name = strings.TrimSpace(*b.Name)
		}
		if b.Description != nil {
			d.Description = strings.TrimSpace(*b.Description)
		}
		if b.Schema != nil {
			d.Schema = *b.Schema
		}
	}
	if err := s.Store.UpdateDataset(ctx, &d); err != nil {
		return nil, err
	}
	return gen.UpdateDataset200JSONResponse(toDataset(d)), nil
}

func (s *Server) DeleteDataset(ctx context.Context, req gen.DeleteDatasetRequestObject) (gen.DeleteDatasetResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteDataset(ctx, d.ID); err != nil {
		return nil, err
	}
	return gen.DeleteDataset204Response{}, nil
}

// versionFilter resolves an optional version number into an example filter.
func (s *Server) versionFilter(ctx context.Context, datasetID uuid.UUID, number *int) (*uuid.UUID, error) {
	if number == nil {
		return nil, nil
	}
	v, err := s.Store.GetDatasetVersionByNumber(ctx, datasetID, *number)
	if err != nil {
		return nil, err
	}
	return &v.ID, nil
}

func (s *Server) ListExamples(ctx context.Context, req gen.ListExamplesRequestObject) (gen.ListExamplesResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	vid, err := s.versionFilter(ctx, d.ID, req.Params.Version)
	if err != nil {
		return nil, err
	}
	f := store.ExampleFilter{DatasetID: d.ID, VersionID: vid, Tag: deref(req.Params.Tag, ""), Search: deref(req.Params.Q, "")}
	pg, err := s.Store.ListExamples(ctx, f, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListExamples200JSONResponse{Items: mapSlice(pg.Items, toExample), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) UpsertExamples(ctx context.Context, req gen.UpsertExamplesRequestObject) (gen.UpsertExamplesResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || len(req.Body.Examples) == 0 {
		return nil, invalid("examples must not be empty")
	}
	if len(req.Body.Examples) > 10000 {
		return nil, invalid("at most 10000 examples per request")
	}
	items := make([]store.ExampleUpsert, len(req.Body.Examples))
	for i, e := range req.Body.Examples {
		if items[i], err = fromExampleData(e); err != nil {
			ae := toError(err)
			ae.Message = "examples[" + strconv.Itoa(i) + "]: " + ae.Message
			return nil, ae
		}
	}
	out, err := s.Store.UpsertExamples(ctx, d.ID, items)
	if err != nil {
		return nil, err
	}
	return gen.UpsertExamples200JSONResponse{Count: len(out), Examples: mapSlice(out, toExample)}, nil
}

func (s *Server) DeleteExamples(ctx context.Context, req gen.DeleteExamplesRequestObject) (gen.DeleteExamplesResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || len(req.Body.Ids) == 0 {
		return nil, invalid("ids must not be empty")
	}
	n, err := s.Store.DeleteExamples(ctx, d.ID, req.Body.Ids)
	if err != nil {
		return nil, err
	}
	return gen.DeleteExamples200JSONResponse{Count: n}, nil
}

func (s *Server) GetExample(ctx context.Context, req gen.GetExampleRequestObject) (gen.GetExampleResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleViewer)
	if err != nil {
		return nil, err
	}
	e, err := s.Store.GetExample(ctx, req.Example)
	if err != nil {
		return nil, err
	}
	if e.DatasetID != d.ID {
		return nil, notFound("example")
	}
	return gen.GetExample200JSONResponse(toExample(e)), nil
}

func (s *Server) AddExamplesFromResults(ctx context.Context, req gen.AddExamplesFromResultsRequestObject) (gen.AddExamplesFromResultsResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || len(b.ResultIds) == 0 {
		return nil, invalid("result_ids must not be empty")
	}
	results, err := s.Store.GetResults(ctx, b.ResultIds)
	if err != nil {
		return nil, err
	}
	// Results must come from runs in the dataset's project; check each run once.
	checked := map[uuid.UUID]bool{}
	items := make([]store.ExampleUpsert, 0, len(results))
	var extraTags []string
	if b.Tags != nil {
		extraTags = normalizeTags(*b.Tags)
	}
	for _, r := range results {
		if !checked[r.RunID] {
			run, err := s.Store.GetRun(ctx, r.RunID)
			if err != nil || run.ProjectID != d.ProjectID {
				return nil, invalid("result %s is not in this dataset's project", r.ID)
			}
			checked[r.RunID] = true
		}
		u := store.ExampleUpsert{ExampleData: store.ExampleData{Input: rawOr(r.Input, "{}"), Expected: r.Expected,
			Context: r.Context, Tags: normalizeTags(append(append([]string{}, r.Tags...), extraTags...)),
			Metadata: mustJSON(map[string]any{"source_run_id": r.RunID, "source_result_id": r.ID})}}
		if deref(b.UseOutputAsExpected, false) {
			u.Expected = mustJSON(r.Output)
		}
		items = append(items, u)
	}
	out, err := s.Store.UpsertExamples(ctx, d.ID, items)
	if err != nil {
		return nil, err
	}
	return gen.AddExamplesFromResults200JSONResponse{Count: len(out), Examples: mapSlice(out, toExample)}, nil
}

func (s *Server) ListDatasetVersions(ctx context.Context, req gen.ListDatasetVersionsRequestObject) (gen.ListDatasetVersionsResponseObject, error) {
	if _, err := s.dataset(ctx, req.Dataset, store.RoleViewer); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListDatasetVersions(ctx, req.Dataset, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListDatasetVersions200JSONResponse{Items: mapSlice(pg.Items, toDatasetVersion), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) PublishDatasetVersion(ctx context.Context, req gen.PublishDatasetVersionRequestObject) (gen.PublishDatasetVersionResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	note := ""
	if req.Body != nil {
		note = trimmed(req.Body.Note)
	}
	v, err := s.Store.PublishDatasetVersion(ctx, d.ID, note, s.principal(ctx).UserID())
	if err != nil {
		return nil, err
	}
	return gen.PublishDatasetVersion201JSONResponse(toDatasetVersion(v)), nil
}

func (s *Server) GetDatasetVersion(ctx context.Context, req gen.GetDatasetVersionRequestObject) (gen.GetDatasetVersionResponseObject, error) {
	if _, err := s.dataset(ctx, req.Dataset, store.RoleViewer); err != nil {
		return nil, err
	}
	v, err := s.Store.GetDatasetVersionByNumber(ctx, req.Dataset, req.Number)
	if err != nil {
		return nil, err
	}
	return gen.GetDatasetVersion200JSONResponse(toDatasetVersion(v)), nil
}

// ExportDatasetVersion streams a version as JSONL or CSV, paging through examples.
func (s *Server) ExportDatasetVersion(ctx context.Context, req gen.ExportDatasetVersionRequestObject) (gen.ExportDatasetVersionResponseObject, error) {
	if _, err := s.dataset(ctx, req.Dataset, store.RoleViewer); err != nil {
		return nil, err
	}
	v, err := s.Store.GetDatasetVersionByNumber(ctx, req.Dataset, req.Number)
	if err != nil {
		return nil, err
	}
	format := "jsonl"
	if req.Params.Format != nil {
		format = string(*req.Params.Format)
	}
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(s.writeExport(ctx, pw, req.Dataset, v.ID, format))
	}()
	if format == "csv" {
		return gen.ExportDatasetVersion200TextcsvResponse{Body: pr}, nil
	}
	return gen.ExportDatasetVersion200ApplicationxNdjsonResponse{Body: pr}, nil
}

func (s *Server) writeExport(ctx context.Context, w io.Writer, datasetID, versionID uuid.UUID, format string) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	var cw *csv.Writer
	if format == "csv" {
		cw = csv.NewWriter(bw)
		if err := cw.Write([]string{"id", "input", "expected", "context", "metadata", "tags", "conversation"}); err != nil {
			return err
		}
	}
	str := func(raw json.RawMessage) string {
		if len(raw) == 0 || string(raw) == "null" {
			return ""
		}
		return string(raw)
	}
	p := store.ListParams{Limit: store.MaxLimit}
	for {
		pg, err := s.Store.ListExamples(ctx, store.ExampleFilter{DatasetID: datasetID, VersionID: &versionID}, p)
		if err != nil {
			return err
		}
		for _, e := range pg.Items {
			if cw != nil {
				if err := cw.Write([]string{e.ID.String(), str(e.Input), str(e.Expected), str(e.Context), str(e.Metadata),
					strings.Join(e.Tags, ","), str(e.Conversation)}); err != nil {
					return err
				}
				continue
			}
			line, err := json.Marshal(toExample(e))
			if err != nil {
				return err
			}
			if _, err := bw.Write(append(line, '\n')); err != nil {
				return err
			}
		}
		if pg.NextCursor == "" {
			break
		}
		p.Cursor = pg.NextCursor
	}
	if cw != nil {
		cw.Flush()
		if err := cw.Error(); err != nil {
			return err
		}
	}
	return bw.Flush()
}
