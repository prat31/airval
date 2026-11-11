package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/planner"
	"github.com/airval/airval/internal/webhooks"
)

// ---- inbound webhooks (raw route; HMAC-authenticated) ----

func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "trigger"))
	if err != nil {
		s.writeError(w, r, notFound("trigger"))
		return
	}
	ctx := r.Context()
	t, err := s.Store.GetTrigger(ctx, id)
	if err != nil || t.Kind != "webhook" || !t.Enabled || len(t.SecretEnc) == 0 {
		s.writeError(w, r, notFound("trigger"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, webhooks.MaxBody+1))
	if err != nil {
		s.writeError(w, r, badRequest("could not read body"))
		return
	}
	if len(body) > webhooks.MaxBody {
		s.writeError(w, r, errorf(http.StatusRequestEntityTooLarge, "payload_too_large", "payloads are limited to 1 MiB"))
		return
	}
	secret, err := s.Keyring.Unseal(t.SecretEnc, t.SecretKeyVersion, t.ID[:])
	if err != nil {
		s.Logger.Error("webhook: unseal secret", "trigger_id", t.ID, "err", err)
		s.writeError(w, r, errorf(http.StatusInternalServerError, "internal", "internal error"))
		return
	}
	if err := webhooks.Verify(secret, r.Header.Get(webhooks.HeaderTimestamp), r.Header.Get(webhooks.HeaderSignature), body, time.Now()); err != nil {
		s.writeError(w, r, errorf(http.StatusUnauthorized, "invalid_signature", "%v", err))
		return
	}
	cfg := planner.ParseTriggerConfig(t.Config)
	vars, labels, err := webhooks.Map(body, cfg.PayloadVariables, cfg.PayloadLabels)
	if err != nil {
		s.writeError(w, r, badRequest("%v", err))
		return
	}
	if err := s.requireQueue(); err != nil {
		s.writeError(w, r, err)
		return
	}
	sr, targets := planner.FromTrigger(t, cfg, labels, vars)
	runs, err := s.planner().StartSuite(ctx, sr, targets)
	if err != nil {
		s.writeError(w, r, engineErr(err))
		return
	}
	now := time.Now().UTC()
	t.LastFiredAt = &now
	if err := s.Store.UpdateTrigger(ctx, &t); err != nil {
		s.Logger.Warn("webhook: record fire", "trigger_id", t.ID, "err", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(gen.RunCreateResponse{Runs: mapSlice(runs, toRun)})
}
