package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- live progress ----

type progress struct {
	mu      sync.Mutex
	w       io.Writer
	enabled bool
	tty     bool
	runs    map[string]progressUpdate
	order   []string
	start   time.Time
	last    time.Time
	lastPct int
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0 && os.Getenv("CI") == "" && os.Getenv("TERM") != "dumb"
}

func newProgress(w io.Writer, enabled bool, _ int) *progress {
	return &progress{w: w, enabled: enabled, tty: isTTY(w), runs: map[string]progressUpdate{}, start: time.Now(), lastPct: -1}
}

func (p *progress) update(u progressUpdate) {
	if !p.enabled {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	id := u.run.ID.String()
	if _, ok := p.runs[id]; !ok {
		p.order = append(p.order, id)
	}
	if prev, ok := p.runs[id]; ok && prev.completed > u.completed {
		return // out-of-order event
	}
	p.runs[id] = u
	var done, total, failed, errored int
	for _, r := range p.runs {
		done += r.completed
		total += max(r.total, r.run.Total)
		failed += r.failed
		errored += r.errored
	}
	if total == 0 {
		return
	}
	pct := min(100, done*100/total)
	el := time.Since(p.start).Round(100 * time.Millisecond)
	rate := float64(done) / max(time.Since(p.start).Minutes(), 1e-9)
	if p.tty {
		const width = 30
		fill := pct * width / 100
		bar := strings.Repeat("█", fill) + strings.Repeat("░", width-fill)
		fmt.Fprintf(p.w, "\r\033[K%s %3d%%  %d/%d  %d failed  %d errored  %s  %.0f/min", bar, pct, done, total, failed, errored, el, rate)
		return
	}
	// Plain output (CI logs): every 10% or 10 seconds.
	if pct/10 != p.lastPct/10 || time.Since(p.last) > 10*time.Second || pct == 100 {
		if pct == p.lastPct && pct == 100 {
			return
		}
		p.lastPct, p.last = pct, time.Now()
		fmt.Fprintf(p.w, "progress: %d%% (%d/%d) · %d failed · %d errored · %s\n", pct, done, total, failed, errored, el)
	}
}

func (p *progress) done() {
	if p.enabled && p.tty {
		fmt.Fprintln(p.w)
	}
}

// ---- summary ----

type gateResult struct {
	GateID string   `json:"gate_id"`
	Name   string   `json:"name"`
	Text   string   `json:"text"`
	Passed bool     `json:"passed"`
	Value  *float64 `json:"value"`
	Detail string   `json:"detail"`
}

type evalSummary struct {
	ID       string  `json:"id"`
	PassRate float64 `json:"pass_rate"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Errored  int     `json:"errored"`
	Values   struct {
		N    int     `json:"n"`
		Mean float64 `json:"mean"`
	} `json:"values"`
}

type summaryView struct {
	PassRate       float64           `json:"pass_rate"`
	MeanScore      *float64          `json:"mean_score"`
	DurationMs     float64           `json:"duration_ms"`
	Throughput     float64           `json:"throughput_per_min"`
	Gates          []gateResult      `json:"gates"`
	Evaluators     []evalSummary     `json:"evaluators"`
	EvaluatorNames map[string]string `json:"evaluator_names"`
	StoppedEarly   bool              `json:"stopped_early"`
	StopReason     string            `json:"stop_reason"`
	LatencyP50     float64           `json:"latency_p50_ms"`
	LatencyP95     float64           `json:"latency_p95_ms"`
	CacheHitRatio  float64           `json:"cache_hit_ratio"`
}

type failedResult struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
	Output   string          `json:"output"`
	Error    string          `json:"error"`
	Scores   []struct {
		EvaluatorID string   `json:"evaluator_id"`
		Value       *float64 `json:"value"`
		Passed      *bool    `json:"passed"`
		Reason      string   `json:"reason"`
	} `json:"scores"`
}

type runReport struct {
	Run      runView
	Summary  summaryView
	Failures []failedResult
	URL      string
}

func (c *apiClient) report(ctx context.Context, r runView) runReport {
	rp := runReport{Run: r, URL: c.runURL(r.ID)}
	if len(r.Summary) > 0 {
		_ = json.Unmarshal(r.Summary, &rp.Summary)
	}
	if r.Failed+r.Errored > 0 {
		var res struct {
			Items []failedResult `json:"items"`
		}
		if err := c.do(ctx, http.MethodGet, "/v1/runs/"+r.ID.String()+"/results?status=failed,errored&limit=100", nil, &res); err == nil {
			rp.Failures = res.Items
		}
	}
	return rp
}

func (rp runReport) evalName(id string) string {
	if n := rp.Summary.EvaluatorNames[id]; n != "" {
		return n
	}
	return id
}

func verdict(r runView) string {
	switch {
	case r.Status == "succeeded" && r.GatePassed != nil && *r.GatePassed:
		return "PASSED"
	case r.Status == "succeeded":
		return "SUCCEEDED"
	case r.Status == "failed":
		return "FAILED"
	}
	return strings.ToUpper(r.Status)
}

func fmtDur(ms float64) string {
	return (time.Duration(ms) * time.Millisecond).Round(100 * time.Millisecond).String()
}

func printSummary(w io.Writer, rp runReport) {
	r, s := rp.Run, rp.Summary
	fmt.Fprintf(w, "\n%s  %s\n", verdict(r), r.Name)
	fmt.Fprintf(w, "  run       %s\n", rp.URL)
	fmt.Fprintf(w, "  results   %d/%d · %d failed · %d errored · pass rate %.1f%%\n", r.Completed, r.Total, r.Failed, r.Errored, s.PassRate*100)
	if s.DurationMs > 0 {
		fmt.Fprintf(w, "  duration  %s · %.0f examples/min · latency p50 %.0f ms, p95 %.0f ms\n", fmtDur(s.DurationMs), s.Throughput, s.LatencyP50, s.LatencyP95)
	}
	if r.CostUSD > 0 || r.TokensIn > 0 {
		fmt.Fprintf(w, "  cost      $%.4f · %d tokens in, %d out\n", r.CostUSD, r.TokensIn, r.TokensOut)
	}
	if s.StoppedEarly {
		fmt.Fprintf(w, "  stopped   early: %s\n", s.StopReason)
	} else if r.Error != "" {
		fmt.Fprintf(w, "  error     %s\n", r.Error)
	}
	if len(s.Evaluators) > 0 {
		fmt.Fprintf(w, "\n  %-28s %8s %10s %8s\n", "EVALUATOR", "MEAN", "PASS RATE", "FAILED")
		for _, e := range s.Evaluators {
			mean := "-"
			if e.Values.N > 0 {
				mean = fmt.Sprintf("%.3f", e.Values.Mean)
			}
			fmt.Fprintf(w, "  %-28s %8s %9.1f%% %8d\n", trunc(rp.evalName(e.ID), 28), mean, e.PassRate*100, e.Failed)
		}
	}
	if len(s.Gates) > 0 {
		fmt.Fprintln(w, "\n  GATES")
		for _, g := range s.Gates {
			mark := "pass"
			if !g.Passed {
				mark = "FAIL"
			}
			line := fmt.Sprintf("  [%s] %s", mark, gateLabel(g))
			if g.Detail != "" && !g.Passed {
				line += " — " + g.Detail
			}
			fmt.Fprintln(w, line)
		}
	}
	fmt.Fprintln(w)
}

func gateLabel(g gateResult) string {
	switch {
	case g.Name != "" && g.Text != "":
		return g.Name + ": " + g.Text
	case g.Name != "":
		return g.Name
	}
	return g.Text
}

func trunc(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return trunc(s, n)
}

func mdEscape(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "<", "&lt;", ">", "&gt;").Replace(s)
}

// renderMarkdown renders a PR-comment summary of the runs.
func renderMarkdown(reports []runReport) []byte {
	var b bytes.Buffer
	for _, rp := range reports {
		r, s := rp.Run, rp.Summary
		icon := map[bool]string{true: "🟢", false: "🔴"}[r.passed()]
		fmt.Fprintf(&b, "#### %s %s — [%s](%s)\n\n", icon, verdict(r), mdEscape(r.Name), rp.URL)
		fmt.Fprintf(&b, "| Results | Pass rate | Failed | Errored | Duration | Cost |\n|---:|---:|---:|---:|---:|---:|\n")
		fmt.Fprintf(&b, "| %d/%d | %.1f%% | %d | %d | %s | $%.4f |\n\n", r.Completed, r.Total, s.PassRate*100, r.Failed, r.Errored,
			fmtDur(s.DurationMs), r.CostUSD)
		if s.StoppedEarly {
			fmt.Fprintf(&b, "> Stopped early: %s\n\n", mdEscape(s.StopReason))
		} else if r.Error != "" {
			fmt.Fprintf(&b, "> %s\n\n", mdEscape(r.Error))
		}
		if len(s.Gates) > 0 {
			b.WriteString("| Gate | Result | Detail |\n|---|---|---|\n")
			for _, g := range s.Gates {
				res := "✅ pass"
				if !g.Passed {
					res = "❌ fail"
				}
				fmt.Fprintf(&b, "| %s | %s | %s |\n", mdEscape(gateLabel(g)), res, mdEscape(g.Detail))
			}
			b.WriteString("\n")
		}
		if len(s.Evaluators) > 0 {
			b.WriteString("| Evaluator | Mean | Pass rate | Failed |\n|---|---:|---:|---:|\n")
			for _, e := range s.Evaluators {
				mean := "–"
				if e.Values.N > 0 {
					mean = fmt.Sprintf("%.3f", e.Values.Mean)
				}
				fmt.Fprintf(&b, "| %s | %s | %.1f%% | %d |\n", mdEscape(rp.evalName(e.ID)), mean, e.PassRate*100, e.Failed)
			}
			b.WriteString("\n")
		}
		if len(rp.Failures) > 0 {
			n := min(len(rp.Failures), 10)
			fmt.Fprintf(&b, "<details><summary>%d failing result(s)", r.Failed+r.Errored)
			if r.Failed+r.Errored > n {
				fmt.Fprintf(&b, " — first %d", n)
			}
			b.WriteString("</summary>\n\n| Input | Output | Why |\n|---|---|---|\n")
			for _, f := range rp.Failures[:n] {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", mdEscape(oneLine(string(f.Input), 80)), mdEscape(oneLine(f.Output, 80)),
					mdEscape(oneLine(rp.why(f), 120)))
			}
			b.WriteString("\n</details>\n\n")
		}
	}
	return b.Bytes()
}

// why explains a failing result: the target error or the failing evaluators.
func (rp runReport) why(f failedResult) string {
	if f.Error != "" {
		return "error: " + f.Error
	}
	var parts []string
	for _, s := range f.Scores {
		if s.Passed != nil && !*s.Passed {
			p := rp.evalName(s.EvaluatorID)
			if s.Value != nil {
				p += fmt.Sprintf(" %.2f", *s.Value)
			}
			if s.Reason != "" {
				p += ": " + s.Reason
			}
			parts = append(parts, p)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

// ---- JUnit ----

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Name    string       `xml:"name,attr"`
	Tests   int          `xml:"tests,attr"`
	Fails   int          `xml:"failures,attr"`
	Errors  int          `xml:"errors,attr"`
	Time    float64      `xml:"time,attr"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name   string      `xml:"name,attr"`
	Tests  int         `xml:"tests,attr"`
	Fails  int         `xml:"failures,attr"`
	Errors int         `xml:"errors,attr"`
	Time   float64     `xml:"time,attr"`
	Props  []junitProp `xml:"properties>property,omitempty"`
	Cases  []junitCase `xml:"testcase"`
}

type junitProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitFailure `xml:"error,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// renderJUnit: one testsuite per run; a testcase for the run itself, one per gate and one per
// failing result (up to 100).
func renderJUnit(reports []runReport) []byte {
	all := junitSuites{Name: "airval"}
	for _, rp := range reports {
		r, s := rp.Run, rp.Summary
		js := junitSuite{Name: r.Name, Time: s.DurationMs / 1000, Props: []junitProp{{"run_id", r.ID.String()}, {"url", rp.URL},
			{"status", r.Status}, {"pass_rate", fmt.Sprintf("%.4f", s.PassRate)}}}
		runCase := junitCase{Name: "run completes", Classname: "airval.run", Time: s.DurationMs / 1000}
		if r.Status == "errored" || r.Status == "canceled" {
			runCase.Error = &junitFailure{Message: "run " + r.Status, Body: r.Error}
		}
		js.Cases = append(js.Cases, runCase)
		for _, g := range s.Gates {
			c := junitCase{Name: gateLabel(g), Classname: "airval.gates"}
			if !g.Passed {
				c.Failure = &junitFailure{Message: "gate failed", Body: g.Detail}
			}
			js.Cases = append(js.Cases, c)
		}
		for _, f := range rp.Failures {
			c := junitCase{Name: oneLine(string(f.Input), 120), Classname: "airval.results"}
			msg := rp.why(f)
			body := fmt.Sprintf("result %s\ninput: %s\nexpected: %s\noutput: %s\n%s", f.ID, f.Input, f.Expected, f.Output, msg)
			if f.Status == "errored" {
				c.Error = &junitFailure{Message: oneLine(msg, 200), Body: body}
			} else {
				c.Failure = &junitFailure{Message: oneLine(msg, 200), Body: body}
			}
			js.Cases = append(js.Cases, c)
		}
		for _, c := range js.Cases {
			js.Tests++
			if c.Failure != nil {
				js.Fails++
			}
			if c.Error != nil {
				js.Errors++
			}
		}
		all.Tests += js.Tests
		all.Fails += js.Fails
		all.Errors += js.Errors
		all.Time += js.Time
		all.Suites = append(all.Suites, js)
	}
	out, _ := xml.MarshalIndent(all, "", "  ")
	return append([]byte(xml.Header), append(out, '\n')...)
}
