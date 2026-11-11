package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// runOpts are the flags of `airval run`.
type runOpts struct {
	server, apiKey, suite, project, format, name, sample, sampleMode string
	junit, markdown                                                  string
	targets, labels, vars                                            []string
	trials                                                           int
	wait, gate, noProgress                                           bool
	timeout                                                          time.Duration
}

// runCmd is the CI client: create runs, stream progress, print a summary, write JUnit/Markdown
// reports and exit 1 when a gate fails (--gate).
func runCmd() *cobra.Command {
	o := &runOpts{}
	cmd := &cobra.Command{
		Use:   "run --suite <id|slug> [--target <id>] [--wait] [--gate] [--junit [file]] [--markdown [file]]",
		Short: "Run a suite (CI): stream progress, print a summary and gate the exit code",
		Long: `Start a suite run and (with --wait or --gate) follow it live.

  --gate              wait, then exit 1 unless every run succeeded with its gates passing
  --junit [file]      write a JUnit XML report (stdout when no file is given)
  --markdown [file]   write a Markdown summary for PR comments (stdout when no file is given)
  --sample 50|10%     run on a sample of examples (random; --sample-mode stratified samples per tag)

The server URL and API key come from --server/--api-key or AIRVAL_URL/AIRVAL_API_KEY.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// `--markdown out.md` / `--junit out.xml`: bare report flags take the next positional arg as their file.
			for _, a := range args {
				switch {
				case o.markdown == "-" && strings.HasSuffix(strings.ToLower(a), ".md"):
					o.markdown = a
				case o.junit == "-" && strings.HasSuffix(strings.ToLower(a), ".xml"):
					o.junit = a
				case o.markdown == "-" && o.junit == "":
					o.markdown = a
				case o.junit == "-" && o.markdown == "":
					o.junit = a
				default:
					return fmt.Errorf("unexpected argument %q", a)
				}
			}
			return o.run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.server, "server", envOr("AIRVAL_URL", "http://localhost:8080"), "airval server URL (env AIRVAL_URL)")
	f.StringVar(&o.server, "url", envOr("AIRVAL_URL", "http://localhost:8080"), "alias of --server")
	_ = f.MarkHidden("url")
	f.StringVar(&o.apiKey, "api-key", os.Getenv("AIRVAL_API_KEY"), "API key (env AIRVAL_API_KEY)")
	f.StringVar(&o.suite, "suite", "", "suite id or slug")
	f.StringVar(&o.project, "project", os.Getenv("AIRVAL_PROJECT"), "project id or slug (narrows suite slug lookup; env AIRVAL_PROJECT)")
	f.StringSliceVar(&o.targets, "target", nil, "target id (repeatable; default: the suite's targets)")
	f.StringArrayVar(&o.labels, "label", nil, "run label key=value (repeatable), e.g. git_sha=abc123")
	f.StringArrayVar(&o.vars, "var", nil, "target variable override key=value (repeatable)")
	f.StringVar(&o.sample, "sample", "", "run on a sample: N examples or N% (e.g. 50 or 10%)")
	f.StringVar(&o.sampleMode, "sample-mode", "random", "sampling mode: random | stratified (by tag)")
	f.IntVar(&o.trials, "trials", 0, "repeat each example N times (1-10; default: the suite's setting)")
	f.StringVar(&o.name, "name", "", "run name")
	f.BoolVar(&o.wait, "wait", false, "wait for the run to finish and print a summary")
	f.BoolVar(&o.gate, "gate", false, "wait and exit 1 if any run fails its gates or does not succeed")
	f.StringVar(&o.junit, "junit", "", "write a JUnit XML report to this file (bare flag: stdout)")
	f.Lookup("junit").NoOptDefVal = "-"
	f.StringVar(&o.markdown, "markdown", "", "write a Markdown summary to this file (bare flag: stdout)")
	f.Lookup("markdown").NoOptDefVal = "-"
	f.StringVar(&o.format, "format", "text", "summary output: text | json")
	f.BoolVar(&o.noProgress, "no-progress", false, "don't show live progress")
	f.DurationVar(&o.timeout, "timeout", 30*time.Minute, "overall timeout")
	return cmd
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func kvList(items []string, flag string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, kv := range items {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("--%s must be key=value (got %q)", flag, kv)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

func parseSample(s, mode string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	if mode != "random" && mode != "stratified" {
		return nil, fmt.Errorf("--sample-mode must be random or stratified")
	}
	if p, ok := strings.CutSuffix(strings.TrimSpace(s), "%"); ok {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v <= 0 || v > 100 {
			return nil, fmt.Errorf("--sample %q: percent must be in (0, 100]", s)
		}
		return map[string]any{"mode": mode, "percent": v}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return nil, fmt.Errorf("--sample %q: use a count (50) or a percentage (10%%)", s)
	}
	return map[string]any{"mode": mode, "n": n}, nil
}

func (o *runOpts) run(ctx context.Context, stdout, stderr io.Writer) error {
	if o.suite == "" {
		return errors.New("--suite is required")
	}
	if o.apiKey == "" {
		fmt.Fprintln(stderr, "warning: no API key (set --api-key or AIRVAL_API_KEY)")
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	c := &apiClient{base: strings.TrimRight(o.server, "/"), key: o.apiKey, http: &http.Client{Timeout: 60 * time.Second}}
	suiteID, err := c.resolveSuite(ctx, o.suite, o.project)
	if err != nil {
		return err
	}
	body := map[string]any{"suite_id": suiteID}
	if len(o.targets) > 0 {
		body["target_ids"] = o.targets
	}
	if l, err := kvList(o.labels, "label"); err != nil {
		return err
	} else if l != nil {
		body["labels"] = l
	}
	if v, err := kvList(o.vars, "var"); err != nil {
		return err
	} else if v != nil {
		body["variables"] = v
	}
	if s, err := parseSample(o.sample, o.sampleMode); err != nil {
		return err
	} else if s != nil {
		body["sampling"] = s
	}
	if o.trials > 0 {
		body["trials"] = o.trials
	}
	if o.name != "" {
		body["name"] = o.name
	}
	if ref := ciRef(); ref != "" {
		body["trigger_ref"] = ref
	}
	var created struct {
		Runs []runView `json:"runs"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/runs", body, &created); err != nil {
		return err
	}
	for _, r := range created.Runs {
		fmt.Fprintf(stderr, "run %s queued: %d examples · %s\n", r.ID, r.Total, c.runURL(r.ID))
	}
	waiting := o.wait || o.gate || o.junit != "" || o.markdown != ""
	if !waiting {
		if o.format == "json" {
			return json.NewEncoder(stdout).Encode(created.Runs)
		}
		for _, r := range created.Runs {
			fmt.Fprintln(stdout, r.ID)
		}
		return nil
	}

	prog := newProgress(stderr, !o.noProgress, len(created.Runs))
	finals := make([]runView, len(created.Runs))
	errs := make([]error, len(created.Runs))
	var wg sync.WaitGroup
	for i, r := range created.Runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			finals[i], errs[i] = c.follow(ctx, r, prog.update)
		}()
	}
	wg.Wait()
	prog.done()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	reports := make([]runReport, len(finals))
	for i, r := range finals {
		reports[i] = c.report(ctx, r)
	}
	toStdout := o.markdown == "-" || o.junit == "-"
	switch {
	case o.format == "json" && !toStdout:
		_ = json.NewEncoder(stdout).Encode(finals)
	case !toStdout:
		for _, rp := range reports {
			printSummary(stdout, rp)
		}
	default:
		for _, rp := range reports {
			printSummary(stderr, rp)
		}
	}
	if o.markdown != "" {
		if err := writeOut(o.markdown, stdout, renderMarkdown(reports)); err != nil {
			return err
		}
	}
	if o.junit != "" {
		if err := writeOut(o.junit, stdout, renderJUnit(reports)); err != nil {
			return err
		}
	}
	if o.gate {
		for _, r := range finals {
			if !r.passed() {
				return exitError{code: 1}
			}
		}
	}
	return nil
}

func writeOut(path string, stdout io.Writer, data []byte) error {
	if path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ciRef identifies the CI job that started the run (GitHub Actions, GitLab CI, Buildkite…).
func ciRef() string {
	if s, r, id := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID"); s != "" && r != "" && id != "" {
		return s + "/" + r + "/actions/runs/" + id
	}
	for _, k := range []string{"CI_JOB_URL", "BUILDKITE_BUILD_URL", "CIRCLE_BUILD_URL", "BUILD_URL"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// ---- API client ----

type apiClient struct {
	base, key string
	http      *http.Client
}

func (c *apiClient) runURL(id uuid.UUID) string { return c.base + "/runs/" + id.String() }

func (c *apiClient) req(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "airval-cli/"+version)
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	return req, nil
}

type apiError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func (c *apiClient) do(ctx context.Context, method, path string, in, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := c.req(ctx, method, path, in)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return err
			}
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 500 && resp.StatusCode != 501 && method == http.MethodGet {
			lastErr = fmt.Errorf("%s %s: %d", method, path, resp.StatusCode)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if resp.StatusCode >= 300 {
			e := &apiError{Status: resp.StatusCode}
			_ = json.Unmarshal(data, e)
			if e.Message == "" {
				e.Message = strings.TrimSpace(string(data))
			}
			return fmt.Errorf("%s %s: %w", method, path, e)
		}
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	}
	return lastErr
}

func (c *apiClient) resolveSuite(ctx context.Context, suite, project string) (string, error) {
	if _, err := uuid.Parse(suite); err == nil {
		return suite, nil
	}
	var projects []string
	if project != "" {
		if _, err := uuid.Parse(project); err == nil {
			projects = []string{project}
		}
	}
	if len(projects) == 0 {
		var pl struct {
			Items []struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			} `json:"items"`
		}
		if err := c.do(ctx, http.MethodGet, "/v1/projects?limit=1000", nil, &pl); err != nil {
			return "", err
		}
		for _, p := range pl.Items {
			if project == "" || p.Slug == project {
				projects = append(projects, p.ID)
			}
		}
	}
	var found []string
	for _, p := range projects {
		var s struct {
			ID string `json:"id"`
		}
		err := c.do(ctx, http.MethodGet, "/v1/projects/"+p+"/suites/by-slug/"+url.PathEscape(suite), nil, &s)
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			continue
		} else if err != nil {
			return "", err
		}
		found = append(found, s.ID)
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no suite with slug %q (pass a suite id, or --project)", suite)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("suite slug %q exists in %d projects: pass --project", suite, len(found))
}

// runView is the subset of a Run the CLI uses.
type runView struct {
	ID         uuid.UUID         `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Total      int               `json:"total"`
	Completed  int               `json:"completed"`
	Failed     int               `json:"failed"`
	Errored    int               `json:"errored"`
	CostUSD    float64           `json:"cost_usd"`
	TokensIn   int64             `json:"tokens_in"`
	TokensOut  int64             `json:"tokens_out"`
	GatePassed *bool             `json:"gate_passed"`
	Error      string            `json:"error"`
	Labels     map[string]string `json:"labels"`
	Summary    json.RawMessage   `json:"summary,omitempty"`
}

func (r runView) terminal() bool {
	switch r.Status {
	case "succeeded", "failed", "errored", "canceled":
		return true
	}
	return false
}

func (r runView) passed() bool {
	return r.Status == "succeeded" && (r.GatePassed == nil || *r.GatePassed)
}

type progressUpdate struct {
	run                               runView
	completed, failed, errored, total int
}

// follow streams a run's events until it finishes (falling back to polling), then returns the run.
func (c *apiClient) follow(ctx context.Context, r runView, onProgress func(progressUpdate)) (runView, error) {
	lastID := ""
	for attempt := 0; ctx.Err() == nil; attempt++ {
		finished, id, err := c.stream(ctx, r, lastID, onProgress)
		if id != "" {
			lastID = id
		}
		if finished {
			break
		}
		// Stream dropped (proxy timeout, server restart): check the run, then reconnect.
		var cur runView
		if gerr := c.do(ctx, http.MethodGet, "/v1/runs/"+r.ID.String(), nil, &cur); gerr == nil {
			onProgress(progressUpdate{run: r, completed: cur.Completed, failed: cur.Failed, errored: cur.Errored, total: cur.Total})
			if cur.terminal() {
				break
			}
		}
		if err != nil && attempt > 30 {
			return r, err
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(min(attempt+1, 5)) * time.Second):
		}
	}
	if ctx.Err() != nil {
		return r, fmt.Errorf("run %s did not finish: %w", r.ID, ctx.Err())
	}
	var final runView
	for i := 0; i < 20; i++ { // the finalizer may still be writing the summary
		if err := c.do(ctx, http.MethodGet, "/v1/runs/"+r.ID.String(), nil, &final); err != nil {
			return r, err
		}
		if final.terminal() && len(final.Summary) > 0 && string(final.Summary) != "null" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return final, nil
}

func (c *apiClient) stream(ctx context.Context, r runView, lastID string, onProgress func(progressUpdate)) (bool, string, error) {
	req, err := c.req(ctx, http.MethodGet, "/v1/runs/"+r.ID.String()+"/events", nil)
	if err != nil {
		return false, "", err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := (&http.Client{}).Do(req) // no timeout: long-lived stream
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", fmt.Errorf("events: http %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var typ, id string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			typ = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			var ev struct {
				Data map[string]any `json:"data"`
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
			switch typ {
			case "run.progress":
				n := func(k string) int { f, _ := ev.Data[k].(float64); return int(f) }
				onProgress(progressUpdate{run: r, completed: n("completed"), failed: n("failed"), errored: n("errored"), total: n("total")})
			case "run.finished":
				return true, id, nil
			}
		}
	}
	return false, id, sc.Err()
}
