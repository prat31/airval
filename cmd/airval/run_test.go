package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prat31/airval/internal/enginetest"
	"github.com/prat31/airval/tools/mocktarget/mock"
)

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := runCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestRunCLI(t *testing.T) {
	h := enginetest.Start(t, enginetest.Options{Mock: mock.Config{Accuracy: 0.8}})
	f := h.NewFixture(60, 0.5, nil)
	dir := t.TempDir()
	md, junit := filepath.Join(dir, "summary.md"), filepath.Join(dir, "report.xml")
	base := []string{"--server", h.Server.URL, "--api-key", h.APIKey, "--no-progress"}

	// Passing gate, slug resolution, file reports (`--markdown <file>` form).
	out, stderr, err := runCLI(t, append(base, "--suite", "facts-suite", "--gate", "--label", "git_sha=abc",
		"--markdown", md, "--junit="+junit)...)
	if err != nil {
		t.Fatalf("run: %v\n%s\n%s", err, out, stderr)
	}
	if !strings.Contains(out, "PASSED") || !strings.Contains(out, "exact_match") {
		t.Fatalf("summary: %s", out)
	}
	mdb, _ := os.ReadFile(md)
	if !strings.Contains(string(mdb), "| Gate | Result |") || !strings.Contains(string(mdb), "failing result") {
		t.Fatalf("markdown: %s", mdb)
	}
	var js struct {
		Tests int `xml:"tests,attr"`
		Fails int `xml:"failures,attr"`
	}
	xb, _ := os.ReadFile(junit)
	if err := xml.Unmarshal(xb, &js); err != nil || js.Tests < 2 || js.Fails == 0 {
		t.Fatalf("junit: %v %+v\n%s", err, js, xb)
	}

	// Failing gate via a stricter gate: exit code 1, bare --markdown writes to stdout.
	h.Do("POST", "/v1/suites/"+f.SuiteID+"/gates", enginetest.Obj{"name": "perfect", "expr": enginetest.Obj{"op": "cmp", "cmp": ">=",
		"args": []enginetest.Obj{{"op": "call", "func": "mean", "params": []string{f.Evaluators["exact_match"]}}, {"op": "num", "value": 0.99}}}},
		201, nil)
	out, _, err = runCLI(t, append(base, "--suite", f.SuiteID, "--gate", "--markdown", "--sample", "20")...)
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	if !strings.HasPrefix(out, "#### ") || !strings.Contains(out, "FAILED") || !strings.Contains(out, "| 20/20 |") {
		t.Fatalf("stdout markdown: %s", out)
	}
}
