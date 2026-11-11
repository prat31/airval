package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/evalcore"
	"github.com/airval/airval/internal/importers"
	"github.com/airval/airval/internal/store"
)

const (
	previewRows      = 20
	importBatchSize  = 500
	maxImportOptions = 1 << 20
)

var errStopImport = errors.New("stop import")

func (s *Server) maxUploadBytes() int64 {
	mb := 512
	if s.Config != nil && s.Config.Server.MaxUploadMB > 0 {
		mb = s.Config.Server.MaxUploadMB
	}
	return int64(mb) << 20
}

// ImportDataset reads CSV/JSONL/JSON/Parquet (multipart upload or inline text) or a public
// Hugging Face dataset in two phases: preview (columns, sample rows, suggested mapping) and
// commit (streamed in batches into the dataset draft). Uploads are spooled to a temp file so
// memory stays flat regardless of file size.
func (s *Server) ImportDataset(ctx context.Context, req gen.ImportDatasetRequestObject) (gen.ImportDatasetResponseObject, error) {
	d, err := s.dataset(ctx, req.Dataset, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	var (
		opts    gen.DatasetImportRequest
		upload  string // temp file path
		upName  string
		cleanup = func() {}
	)
	switch {
	case req.MultipartBody != nil:
		upload, upName, opts, err = s.readUpload(req.MultipartBody)
		if upload != "" {
			cleanup = func() { _ = os.Remove(upload) }
		}
		if err != nil {
			cleanup()
			return nil, err
		}
	case req.JSONBody != nil:
		opts = *req.JSONBody
	default:
		return nil, badRequest("send a multipart upload (file) or a JSON import request")
	}
	defer cleanup()

	src, err := s.importSource(opts, upload, upName)
	if err != nil {
		return nil, err
	}
	preview := deref(req.Params.Preview, false) || deref(opts.Preview, false)
	var mapping importers.Mapping
	haveMapping := opts.Mapping != nil
	if haveMapping {
		if mapping, err = convertJSON[importers.Mapping](opts.Mapping); err != nil {
			return nil, fieldError("mapping", "%v", err)
		}
		if err := mapping.Validate(); err != nil {
			return nil, fieldError("mapping", "%v", err)
		}
	}

	if preview {
		return s.importPreview(ctx, src, mapping, haveMapping)
	}
	if !haveMapping { // auto-map from the first rows
		pv, err := importers.Preview(ctx, src, previewRows)
		if err != nil {
			return nil, fieldError("source", "%v", err)
		}
		mapping = importers.SuggestMapping(pv.Columns)
		if err := mapping.Validate(); err != nil {
			return nil, fieldError("mapping", "could not infer a mapping, send one: %v", err)
		}
	}
	return s.importCommit(ctx, d, src, mapping, opts)
}

// readUpload spools the file part to a temp file (bounded by server.max_upload_mb) and parses
// the optional `options` part (a JSON DatasetImportRequest).
func (s *Server) readUpload(mr *multipart.Reader) (path, name string, opts gen.DatasetImportRequest, err error) {
	max := s.maxUploadBytes()
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			return path, name, opts, badRequest("invalid multipart body: %v", perr)
		}
		switch part.FormName() {
		case "options":
			raw, rerr := io.ReadAll(io.LimitReader(part, maxImportOptions))
			if rerr != nil {
				return path, name, opts, badRequest("reading options: %v", rerr)
			}
			if len(strings.TrimSpace(string(raw))) > 0 {
				if jerr := json.Unmarshal(raw, &opts); jerr != nil {
					return path, name, opts, fieldError("options", "options must be a JSON import request: %v", jerr)
				}
			}
		case "file":
			if path != "" {
				return path, name, opts, badRequest("send exactly one file")
			}
			name = filepath.Base(part.FileName())
			f, cerr := os.CreateTemp("", "airval-import-*"+filepath.Ext(name))
			if cerr != nil {
				return path, name, opts, cerr
			}
			path = f.Name()
			n, cerr := io.Copy(f, io.LimitReader(part, max+1))
			if closeErr := f.Close(); cerr == nil {
				cerr = closeErr
			}
			if cerr != nil {
				return path, name, opts, badRequest("reading upload: %v", cerr)
			}
			if n > max {
				return path, name, opts, errorf(http.StatusRequestEntityTooLarge, "payload_too_large",
					"upload exceeds the %d MB limit (server.max_upload_mb)", max>>20)
			}
		}
	}
	if path == "" {
		return path, name, opts, fieldError("file", "a file part named \"file\" is required")
	}
	return path, name, opts, nil
}

// importSource builds the reader for the request.
func (s *Server) importSource(opts gen.DatasetImportRequest, upload, upName string) (importers.Source, error) {
	var in struct {
		kind, format, data, repo, cfg, split string
	}
	if o := opts.Source; o != nil {
		in.kind, in.format = string(deref(o.Kind, "")), string(deref(o.Format, ""))
		in.data, in.repo, in.cfg, in.split = deref(o.Data, ""), deref(o.HfRepo, ""), deref(o.HfConfig, ""), deref(o.HfSplit, "")
	}
	format := importers.Format(in.format)
	if format != "" && !validImportFormat(format) {
		return importers.Source{}, fieldError("source.format", "format must be csv, jsonl, json or parquet")
	}
	switch {
	case upload != "":
		if format == "" {
			format = importers.FormatFromName(upName)
		}
		src, err := importers.FromFile(upload, format)
		if err != nil {
			return src, err
		}
		src.Name = upName
		return src, nil
	case in.kind == "huggingface" || in.repo != "":
		if in.repo == "" {
			return importers.Source{}, fieldError("source.hf_repo", "hf_repo is required for huggingface sources")
		}
		if in.split == "" {
			in.split = "train"
		}
		return importers.FromHF(&importers.HFClient{}, importers.HFRef{Dataset: in.repo, Config: in.cfg, Split: in.split}), nil
	case in.kind == "inline" || in.data != "":
		if strings.TrimSpace(in.data) == "" {
			return importers.Source{}, fieldError("source.data", "data is empty")
		}
		return importers.FromBytes([]byte(in.data), format), nil
	case in.kind == "upload":
		return importers.Source{}, fieldError("source.kind", "uploads must be sent as multipart/form-data with a file part")
	}
	return importers.Source{}, fieldError("source", "choose a source: upload a file, inline data, or a Hugging Face dataset")
}

func validImportFormat(f importers.Format) bool {
	switch f {
	case importers.FormatCSV, importers.FormatJSONL, importers.FormatJSON, importers.FormatParquet:
		return true
	}
	return false
}

func (s *Server) importPreview(ctx context.Context, src importers.Source, m importers.Mapping, haveMapping bool) (gen.ImportDatasetResponseObject, error) {
	pv, err := importers.Preview(ctx, src, previewRows)
	if err != nil {
		return nil, fieldError("source", "%v", err)
	}
	sp, err := convertJSON[gen.DatasetPreview](pv)
	if err != nil {
		return nil, err
	}
	sp.RowErrors = nil
	if len(pv.RowErrors) > 0 {
		sp.RowErrors = ptr(mapSlice(pv.RowErrors, rowError))
	}
	cols := make([]string, len(pv.Columns))
	for i, c := range pv.Columns {
		cols[i] = c.Name
	}
	sm, err := convertJSON[gen.ColumnMapping](importers.SuggestMapping(pv.Columns))
	if err != nil {
		return nil, err
	}
	out := gen.DatasetImportResult{Columns: &cols, SourcePreview: &sp, SuggestedMapping: &sm}
	if haveMapping {
		var mapped []gen.ExampleData
		res, err := importers.Import(ctx, src, m, func(batch []evalcore.Example) error {
			for _, ex := range batch {
				mapped = append(mapped, toExampleData(ex))
			}
			return errStopImport
		}, previewRows)
		if err != nil && !errors.Is(err, errStopImport) {
			return nil, fieldError("mapping", "%v", err)
		}
		if len(mapped) > previewRows {
			mapped = mapped[:previewRows]
		}
		out.Preview = &mapped
		out.Total, out.Failed = res.Total, res.Failed
		if len(res.Errors) > 0 {
			out.Errors = ptr(mapSlice(res.Errors, rowError))
		}
	}
	return gen.ImportDataset200JSONResponse(out), nil
}

func rowError(e importers.RowError) gen.ImportRowError {
	return gen.ImportRowError{Row: e.Row, Message: e.Reason}
}

func toExampleData(ex evalcore.Example) gen.ExampleData {
	out := gen.ExampleData{Input: rawPtr(mustJSON(nonNilMap(ex.Input)))}
	if ex.Expected != nil {
		out.Expected = rawPtr(mustJSON(ex.Expected))
	}
	if len(ex.Context) > 0 {
		out.Context = &ex.Context
	}
	if len(ex.Metadata) > 0 {
		out.Metadata = rawPtr(mustJSON(ex.Metadata))
	}
	if len(ex.Tags) > 0 {
		out.Tags = &ex.Tags
	}
	if ex.Conversation != nil {
		out.Conversation = rawPtr(mustJSON(ex.Conversation))
	}
	return out
}

func (s *Server) importCommit(ctx context.Context, d store.Dataset, src importers.Source, m importers.Mapping,
	opts gen.DatasetImportRequest) (gen.ImportDatasetResponseObject, error) {
	extra := []string{}
	if opts.Tags != nil {
		extra = normalizeTags(*opts.Tags)
	}
	limit := 0
	if opts.Source != nil {
		limit = deref(opts.Source.Limit, 0)
	}
	replace := deref(opts.Mode, "append") == "replace"
	var oldIDs []uuid.UUID
	if replace {
		var err error
		if oldIDs, err = s.draftExampleIDs(ctx, d.ID); err != nil {
			return nil, err
		}
	}
	written := 0
	kept := map[uuid.UUID]bool{} // replace mode: re-imported ids must survive the cleanup
	res, err := importers.Import(ctx, src, m, func(batch []evalcore.Example) error {
		if limit > 0 && written+len(batch) > limit {
			batch = batch[:limit-written]
		}
		if len(batch) == 0 {
			return errStopImport
		}
		items := make([]store.ExampleUpsert, len(batch))
		for i, ex := range batch {
			items[i] = fromEvalExample(ex, extra)
		}
		saved, err := s.Store.UpsertExamples(ctx, d.ID, items)
		if err != nil {
			return err
		}
		if replace {
			for _, e := range saved {
				kept[e.ID] = true
			}
		}
		written += len(batch)
		if limit > 0 && written >= limit {
			return errStopImport
		}
		return nil
	}, importBatchSize)
	if err != nil && !errors.Is(err, errStopImport) {
		var ae *Error
		if errors.As(err, &ae) || errors.Is(err, context.Canceled) || errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, fieldError("source", "import failed after %d examples: %v", written, err)
	}
	if replace && written > 0 {
		oldIDs = slices.DeleteFunc(oldIDs, func(id uuid.UUID) bool { return kept[id] })
		for i := 0; i < len(oldIDs); i += store.MaxLimit {
			end := min(i+store.MaxLimit, len(oldIDs))
			if _, err := s.Store.DeleteExamples(ctx, d.ID, oldIDs[i:end]); err != nil {
				return nil, err
			}
		}
	}
	out := gen.DatasetImportResult{Count: written, Total: res.Total, Failed: res.Failed}
	if len(res.Errors) > 0 {
		out.Errors = ptr(mapSlice(res.Errors, rowError))
		out.ErrorsTruncated = ptr(res.ErrorsTruncated)
	}
	if deref(opts.Publish, false) && written > 0 {
		v, err := s.Store.PublishDatasetVersion(ctx, d.ID, "Imported "+src.Name, s.principal(ctx).UserID())
		if err != nil {
			return nil, err
		}
		dv := toDatasetVersion(v)
		out.Version = &dv
	}
	return gen.ImportDataset200JSONResponse(out), nil
}

// draftExampleIDs lists the ids of every example currently in the draft.
func (s *Server) draftExampleIDs(ctx context.Context, datasetID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	p := store.ListParams{Limit: store.MaxLimit}
	for {
		pg, err := s.Store.ListExamples(ctx, store.ExampleFilter{DatasetID: datasetID}, p)
		if err != nil {
			return nil, err
		}
		for _, e := range pg.Items {
			ids = append(ids, e.ID)
		}
		if pg.NextCursor == "" {
			return ids, nil
		}
		p.Cursor = pg.NextCursor
	}
}
