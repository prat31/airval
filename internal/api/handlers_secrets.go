package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/airval/airval/internal/api/gen"
	"github.com/airval/airval/internal/store"
)

var secretNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// Secrets: editors can see metadata (to reference names in configs); admins manage values.
// Values are write-only: never returned, never logged.

func (s *Server) orgSecret(ctx context.Context, org, id uuid.UUID) (store.Secret, error) {
	x, err := s.Store.GetSecret(ctx, id)
	if err != nil {
		return x, err
	}
	if x.OrgID != org {
		return x, store.ErrNotFound
	}
	return x, nil
}

func (s *Server) ListSecrets(ctx context.Context, req gen.ListSecretsRequestObject) (gen.ListSecretsResponseObject, error) {
	if err := s.require(ctx, req.Org, req.Params.ProjectId, store.RoleEditor); err != nil {
		return nil, err
	}
	f := store.SecretFilter{OrgID: req.Org, ProjectID: req.Params.ProjectId, AllScopes: deref(req.Params.All, false)}
	pg, err := s.Store.ListSecrets(ctx, f, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListSecrets200JSONResponse{Items: mapSlice(pg.Items, toSecret), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateSecret(ctx context.Context, req gen.CreateSecretRequestObject) (gen.CreateSecretResponseObject, error) {
	b := req.Body
	if b == nil {
		return nil, invalid("request body required")
	}
	if err := s.require(ctx, req.Org, b.ProjectId, store.RoleAdmin); err != nil {
		return nil, err
	}
	if !secretNameRe.MatchString(b.Name) {
		return nil, invalid("name must match %s", secretNameRe.String())
	}
	if b.Value == nil || *b.Value == "" {
		return nil, invalid("value is required")
	}
	if b.ProjectId != nil {
		p, err := s.Store.GetProject(ctx, *b.ProjectId)
		if err != nil || p.OrgID != req.Org {
			return nil, invalid("project_id does not belong to this organization")
		}
	}
	x := store.Secret{ID: store.NewID(), OrgID: req.Org, ProjectID: b.ProjectId, Name: b.Name,
		Description: trimmed(b.Description), CreatedBy: s.principal(ctx).UserID()}
	if err := s.Keyring.EncryptSecret(&x, *b.Value); err != nil {
		return nil, err
	}
	if err := s.Store.CreateSecret(ctx, &x); err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "secret.create", "secret:"+x.ID.String(), map[string]string{"name": x.Name})
	return gen.CreateSecret201JSONResponse(toSecret(x)), nil
}

func (s *Server) GetSecret(ctx context.Context, req gen.GetSecretRequestObject) (gen.GetSecretResponseObject, error) {
	x, err := s.orgSecret(ctx, req.Org, req.Secret)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, req.Org, x.ProjectID, store.RoleEditor); err != nil {
		return nil, err
	}
	return gen.GetSecret200JSONResponse(toSecret(x)), nil
}

func (s *Server) UpdateSecret(ctx context.Context, req gen.UpdateSecretRequestObject) (gen.UpdateSecretResponseObject, error) {
	x, err := s.orgSecret(ctx, req.Org, req.Secret)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, req.Org, x.ProjectID, store.RoleAdmin); err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Description != nil {
			x.Description = strings.TrimSpace(*b.Description)
		}
		if b.Value != nil {
			if *b.Value == "" {
				return nil, invalid("value cannot be empty")
			}
			if err := s.Keyring.EncryptSecret(&x, *b.Value); err != nil {
				return nil, err
			}
		}
	}
	if err := s.Store.UpdateSecret(ctx, &x); err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "secret.update", "secret:"+x.ID.String(), map[string]string{"name": x.Name})
	return gen.UpdateSecret200JSONResponse(toSecret(x)), nil
}

func (s *Server) DeleteSecret(ctx context.Context, req gen.DeleteSecretRequestObject) (gen.DeleteSecretResponseObject, error) {
	x, err := s.orgSecret(ctx, req.Org, req.Secret)
	if err != nil {
		return nil, err
	}
	if err := s.require(ctx, req.Org, x.ProjectID, store.RoleAdmin); err != nil {
		return nil, err
	}
	if err := s.Store.DeleteSecret(ctx, x.ID); err != nil {
		return nil, err
	}
	s.audit(ctx, req.Org, "secret.delete", "secret:"+x.ID.String(), map[string]string{"name": x.Name})
	return gen.DeleteSecret204Response{}, nil
}

// ---- notification channels (admin) ----

var channelKinds = map[string]bool{"slack": true, "webhook": true, "email": true}
var channelEvents = map[string]bool{"run.finished": true, "run.failed": true, "run.errored": true, "gate.failed": true}

func channelEventsIn(in *[]gen.NotificationEvent) ([]string, error) {
	if in == nil {
		return []string{"run.failed", "gate.failed"}, nil
	}
	out := make([]string, 0, len(*in))
	for _, e := range *in {
		if !channelEvents[string(e)] {
			return nil, invalid("unknown event %q", e)
		}
		out = append(out, string(e))
	}
	return out, nil
}

func (s *Server) orgChannel(ctx context.Context, org, id uuid.UUID) (store.NotificationChannel, error) {
	if err := s.require(ctx, org, nil, store.RoleAdmin); err != nil {
		return store.NotificationChannel{}, err
	}
	c, err := s.Store.GetNotificationChannel(ctx, id)
	if err == nil && c.OrgID != org {
		err = store.ErrNotFound
	}
	return c, err
}

func (s *Server) ListNotificationChannels(ctx context.Context, req gen.ListNotificationChannelsRequestObject) (gen.ListNotificationChannelsResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	pg, err := s.Store.ListNotificationChannels(ctx, req.Org, listParams(req.Params.Cursor, req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return gen.ListNotificationChannels200JSONResponse{Items: mapSlice(pg.Items, toChannel), NextCursor: nextCursor(pg.NextCursor)}, nil
}

func (s *Server) CreateNotificationChannel(ctx context.Context, req gen.CreateNotificationChannelRequestObject) (gen.CreateNotificationChannelResponseObject, error) {
	if err := s.require(ctx, req.Org, nil, store.RoleAdmin); err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil || strings.TrimSpace(b.Name) == "" {
		return nil, invalid("name is required")
	}
	if !channelKinds[string(b.Kind)] {
		return nil, invalid("kind must be slack, webhook or email")
	}
	cfg, err := jsonObject(b.Config, "config")
	if err != nil {
		return nil, err
	}
	events, err := channelEventsIn(b.Events)
	if err != nil {
		return nil, err
	}
	c := store.NotificationChannel{OrgID: req.Org, Name: strings.TrimSpace(b.Name), Kind: string(b.Kind), Config: cfg,
		Events: events, Enabled: deref(b.Enabled, true)}
	if err := s.Store.CreateNotificationChannel(ctx, &c); err != nil {
		return nil, err
	}
	return gen.CreateNotificationChannel201JSONResponse(toChannel(c)), nil
}

func (s *Server) GetNotificationChannel(ctx context.Context, req gen.GetNotificationChannelRequestObject) (gen.GetNotificationChannelResponseObject, error) {
	c, err := s.orgChannel(ctx, req.Org, req.Channel)
	if err != nil {
		return nil, err
	}
	return gen.GetNotificationChannel200JSONResponse(toChannel(c)), nil
}

func (s *Server) UpdateNotificationChannel(ctx context.Context, req gen.UpdateNotificationChannelRequestObject) (gen.UpdateNotificationChannelResponseObject, error) {
	c, err := s.orgChannel(ctx, req.Org, req.Channel)
	if err != nil {
		return nil, err
	}
	if b := req.Body; b != nil {
		if b.Name != nil && strings.TrimSpace(*b.Name) != "" {
			c.Name = strings.TrimSpace(*b.Name)
		}
		if b.Config != nil {
			if c.Config, err = jsonObject(*b.Config, "config"); err != nil {
				return nil, err
			}
		}
		if b.Events != nil {
			if c.Events, err = channelEventsIn(b.Events); err != nil {
				return nil, err
			}
		}
		if b.Enabled != nil {
			c.Enabled = *b.Enabled
		}
	}
	if err := s.Store.UpdateNotificationChannel(ctx, &c); err != nil {
		return nil, err
	}
	return gen.UpdateNotificationChannel200JSONResponse(toChannel(c)), nil
}

func (s *Server) DeleteNotificationChannel(ctx context.Context, req gen.DeleteNotificationChannelRequestObject) (gen.DeleteNotificationChannelResponseObject, error) {
	c, err := s.orgChannel(ctx, req.Org, req.Channel)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteNotificationChannel(ctx, c.ID); err != nil {
		return nil, err
	}
	return gen.DeleteNotificationChannel204Response{}, nil
}

func (s *Server) TestNotificationChannel(ctx context.Context, req gen.TestNotificationChannelRequestObject) (gen.TestNotificationChannelResponseObject, error) {
	if _, err := s.orgChannel(ctx, req.Org, req.Channel); err != nil {
		return nil, err
	}
	// TODO(wave): send through internal/webhooks outbound notifier (milestone 8).
	return nil, errorf(http.StatusNotImplemented, "not_implemented", "notification delivery is not implemented yet")
}
