package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/airval/airval/internal/api/gen"
)

// runCmd is the CI client. TODO(wave): JUnit/Markdown output, live event streaming, slug resolution
// across projects (milestone 8). The basic create → wait → gate flow works against the API.
func runCmd() *cobra.Command {
	var (
		server, apiKey, suite, project, format string
		targets, labels                        []string
		wait, gate                             bool
		timeout                                time.Duration
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start a suite run (CI): --suite <id|slug> [--target <id>] [--wait] [--gate]",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if suite == "" {
				return fmt.Errorf("--suite is required")
			}
			c := &apiClient{base: strings.TrimRight(server, "/"), key: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			suiteID, err := uuid.Parse(suite)
			if err != nil {
				if project == "" {
					return fmt.Errorf("--project is required when --suite is a slug")
				}
				var s gen.Suite
				if err := c.do(ctx, http.MethodGet, "/v1/projects/"+project+"/suites/by-slug/"+suite, nil, &s); err != nil {
					return err
				}
				suiteID = s.Id
			}
			body := gen.RunCreate{SuiteId: suiteID}
			if len(targets) > 0 {
				ids := make([]uuid.UUID, 0, len(targets))
				for _, t := range targets {
					id, err := uuid.Parse(t)
					if err != nil {
						return fmt.Errorf("--target must be a target id: %w", err)
					}
					ids = append(ids, id)
				}
				body.TargetIds = &ids
			}
			if len(labels) > 0 {
				l := gen.Labels{}
				for _, kv := range labels {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("--label must be key=value")
					}
					l[k] = v
				}
				body.Labels = &l
			}
			var created gen.RunCreateResponse
			if err := c.do(ctx, http.MethodPost, "/v1/runs", body, &created); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, r := range created.Runs {
				fmt.Fprintf(out, "run %s queued\n", r.Id)
			}
			if !wait && !gate {
				return nil
			}
			failed := false
			for _, r := range created.Runs {
				final, err := c.waitRun(ctx, r.Id)
				if err != nil {
					return err
				}
				if format == "json" {
					_ = json.NewEncoder(out).Encode(final)
				} else {
					fmt.Fprintf(out, "run %s %s: %d/%d completed, %d failed, %d errored, $%.4f\n", final.Id, final.Status,
						final.Completed, final.Total, final.Failed, final.Errored, final.CostUsd)
				}
				if final.Status != gen.RunStatus("succeeded") || (final.GatePassed != nil && !*final.GatePassed) {
					failed = true
				}
			}
			if gate && failed {
				return exitError{code: 1}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&server, "server", envOr("AIRVAL_URL", "http://localhost:8080"), "airval server URL (env AIRVAL_URL)")
	f.StringVar(&apiKey, "api-key", os.Getenv("AIRVAL_API_KEY"), "API key (env AIRVAL_API_KEY)")
	f.StringVar(&suite, "suite", "", "suite id or slug")
	f.StringVar(&project, "project", os.Getenv("AIRVAL_PROJECT"), "project id (needed for suite slugs)")
	f.StringSliceVar(&targets, "target", nil, "target id (repeatable; default: the suite's targets)")
	f.StringSliceVar(&labels, "label", nil, "run label key=value (repeatable), e.g. git_sha=abc123")
	f.BoolVar(&wait, "wait", false, "wait for the run to finish")
	f.BoolVar(&gate, "gate", false, "wait and exit 1 if the gate fails")
	f.StringVar(&format, "format", "text", "output: text|json")
	f.DurationVar(&timeout, "timeout", 30*time.Minute, "overall timeout")
	return cmd
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type apiClient struct {
	base, key string
	http      *http.Client
}

func (c *apiClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e gen.Error
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s %s: %d %s: %s", method, path, resp.StatusCode, e.Code, e.Message)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *apiClient) waitRun(ctx context.Context, id uuid.UUID) (gen.Run, error) {
	for {
		var r gen.Run
		if err := c.do(ctx, http.MethodGet, "/v1/runs/"+id.String(), nil, &r); err != nil {
			return r, err
		}
		switch r.Status {
		case "succeeded", "failed", "errored", "canceled":
			return r, nil
		}
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
