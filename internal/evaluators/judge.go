package evaluators

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/airval/airval/internal/evalcore"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

var promptTmpl = template.Must(template.New("prompts").
	Funcs(template.FuncMap{"inc": func(i int) int { return i + 1 }}).
	ParseFS(promptFS, "prompts/*.tmpl"))

// promptSource returns the raw template text of a prompt file (shown to
// users in the catalog).
func promptSource(file string) string {
	b, err := promptFS.ReadFile("prompts/" + file)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// systemPrompt is the shared judge system prompt.
var systemPrompt = func() string {
	var b bytes.Buffer
	if err := promptTmpl.ExecuteTemplate(&b, "system", nil); err != nil {
		panic(err)
	}
	return b.String()
}()

// Logprob modes for JudgeConfig.Logprobs.
const (
	LogprobsAuto    = "auto"
	LogprobsOff     = "off"
	LogprobsTwoPass = "two_pass"
)

// JudgeConfig holds settings shared by every LLM judge.
type JudgeConfig struct {
	Provider    string   `json:"provider" title:"Provider" desc:"LLM provider of the judge model, e.g. openai or anthropic."`
	Model       string   `json:"model" title:"Model" desc:"Judge model, e.g. gpt-4.1 or claude-sonnet-4-5. Stronger models grade more reliably."`
	Secret      string   `json:"secret,omitempty" title:"API key secret" ui:"secret" desc:"Name of the secret that holds the provider API key."`
	Temperature *float64 `json:"temperature,omitempty" title:"Temperature" default:"0" min:"0" max:"2" desc:"Sampling temperature. Keep 0 for repeatable grades; raise it slightly when using several samples."`
	Samples     int      `json:"samples,omitempty" title:"Samples" default:"1" min:"1" max:"9" desc:"Ask the judge this many times and take the median score (majority for verdicts). Reduces noise at proportional cost."`
	MaxTokens   int      `json:"max_tokens,omitempty" title:"Max tokens" default:"1024" min:"64" max:"16384" desc:"Upper bound on the judge's reply length."`
	Logprobs    string   `json:"logprobs,omitempty" title:"Logprob weighting" enum:"auto|off|two_pass" default:"auto" desc:"auto: when the provider returns token probabilities for the score, use the probability-weighted score. two_pass: after the judge reasons, make one short extra call to read the score probabilities (G-Eval). off: use the integer score."`
	NoCache     bool     `json:"no_cache,omitempty" title:"Disable cache" desc:"Always call the judge, even for an identical prompt graded before."`
}

func (c *JudgeConfig) validate() error {
	if strings.TrimSpace(c.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model is required")
	}
	if c.Temperature == nil {
		c.Temperature = f64(0)
	}
	if *c.Temperature < 0 || *c.Temperature > 2 {
		return errors.New("temperature must be between 0 and 2")
	}
	if c.Samples == 0 {
		c.Samples = 1
	}
	if c.Samples < 1 || c.Samples > 9 {
		return errors.New("samples must be between 1 and 9")
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 1024
	}
	if c.MaxTokens < 64 {
		return errors.New("max_tokens must be at least 64")
	}
	switch c.Logprobs {
	case "":
		c.Logprobs = LogprobsAuto
	case LogprobsAuto, LogprobsOff, LogprobsTwoPass:
	default:
		return fmt.Errorf("logprobs: must be auto, off or two_pass (got %q)", c.Logprobs)
	}
	return nil
}

// judgeBase is the LLM plumbing shared by score and pairwise judges.
type judgeBase struct {
	kind    string
	version string
	cfg     *JudgeConfig
	llm     evalcore.LLM
	cache   Cache
}

func newJudgeBase(kind, version string, cfg *JudgeConfig, deps Deps) (judgeBase, error) {
	if deps.LLMs == nil {
		return judgeBase{}, ErrNoLLM
	}
	llm, err := deps.LLMs(cfg.Provider, cfg.Model, cfg.Secret)
	if err != nil {
		return judgeBase{}, fmt.Errorf("judge model %s/%s: %w", cfg.Provider, cfg.Model, err)
	}
	var cache Cache
	if !cfg.NoCache {
		cache = deps.Cache
	}
	return judgeBase{kind: kind, version: version, cfg: cfg, llm: llm, cache: cache}, nil
}

func (j *judgeBase) Kind() string    { return j.kind }
func (j *judgeBase) Cost() CostClass { return CostModel }

func (j *judgeBase) promptVersion() string { return j.kind + "@" + j.version }

// cacheKey = sha256(judge kind + version + model + rendered prompt + sample).
func (j *judgeBase) cacheKey(user string, variant string) string {
	h := sha256.New()
	for _, p := range []string{"judge/v1", j.kind, j.version, j.cfg.Provider, j.cfg.Model,
		strconv.FormatFloat(*j.cfg.Temperature, 'g', -1, 64), j.cfg.Logprobs, variant, systemPrompt, user} {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "judge:" + hex.EncodeToString(h.Sum(nil))
}

// cached runs compute unless a result for key is in the cache.
func cached[T any](ctx context.Context, c Cache, key string, compute func() (T, error)) (T, bool, error) {
	var zero T
	if c != nil {
		if b, ok, err := c.Get(ctx, key); err == nil && ok {
			var v T
			if json.Unmarshal(b, &v) == nil {
				return v, true, nil
			}
		}
	}
	v, err := compute()
	if err != nil {
		return zero, false, err
	}
	if c != nil {
		if b, err := json.Marshal(v); err == nil {
			_ = c.Set(ctx, key, b) // a cache write failure must not fail the score
		}
	}
	return v, false, nil
}

const repairPrompt = "Your previous reply could not be parsed as the required JSON object (%s). Reply again with ONLY the JSON object in exactly the requested format, with no other text."

// completeJSON calls the judge and extracts the JSON object containing key,
// with one repair retry when the reply is malformed or fails check.
func (j *judgeBase) completeJSON(ctx context.Context, user, key string, logprobs bool, check func(map[string]any) error) (map[string]any, evalcore.LLMResponse, []evalcore.Message, float64, error) {
	msgs := []evalcore.Message{{Role: "user", Content: user}}
	req := evalcore.LLMRequest{
		Model:       j.cfg.Model,
		System:      systemPrompt,
		Messages:    msgs,
		Temperature: *j.cfg.Temperature,
		MaxTokens:   j.cfg.MaxTokens,
		JSONMode:    true,
		Logprobs:    logprobs,
	}
	resp, err := j.llm.Complete(ctx, req)
	if err != nil {
		return nil, resp, nil, 0, fmt.Errorf("judge call failed: %w", err)
	}
	cost := resp.CostUSD
	parse := func(content string) (map[string]any, error) {
		obj, err := extractJSON(content, key)
		if err == nil && check != nil {
			err = check(obj)
		}
		return obj, err
	}
	obj, perr := parse(resp.Content)
	if perr == nil {
		return obj, resp, append(msgs, evalcore.Message{Role: "assistant", Content: resp.Content}), cost, nil
	}
	// One repair attempt.
	msgs = append(msgs,
		evalcore.Message{Role: "assistant", Content: resp.Content},
		evalcore.Message{Role: "user", Content: fmt.Sprintf(repairPrompt, perr)})
	req.Messages = msgs
	req.Logprobs = false
	resp2, err := j.llm.Complete(ctx, req)
	if err != nil {
		return nil, resp2, nil, cost, fmt.Errorf("judge repair call failed: %w", err)
	}
	cost += resp2.CostUSD
	obj, perr2 := parse(resp2.Content)
	if perr2 != nil {
		return nil, resp2, nil, cost, fmt.Errorf("judge reply is not valid JSON after one repair attempt: %v (reply: %q)", perr2, trunc(resp2.Content, 200))
	}
	return obj, resp2, append(msgs, evalcore.Message{Role: "assistant", Content: resp2.Content}), cost, nil
}

// runSamples runs n samples concurrently. It succeeds if at least one
// sample succeeds; errs holds the failures.
func runSamples[T any](ctx context.Context, n int, fn func(ctx context.Context, i int) (T, error)) ([]T, []error) {
	if n == 1 {
		v, err := fn(ctx, 0)
		if err != nil {
			return nil, []error{err}
		}
		return []T{v}, nil
	}
	res := make([]T, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], errs[i] = fn(ctx, i)
		}()
	}
	wg.Wait()
	var ok []T
	var bad []error
	for i := range res {
		if errs[i] != nil {
			bad = append(bad, errs[i])
		} else {
			ok = append(ok, res[i])
		}
	}
	return ok, bad
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n == 0 {
		return math.NaN()
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// ---------------------------------------------------------------- prompt data

const maxFieldChars = 32000

type anchor struct {
	Score int
	Text  string
}

// promptData is what judge templates see.
type promptData struct {
	Input       string
	Output      string
	Expected    string
	HasExpected bool
	Context     []string
	Transcript  string
	ToolCalls   string
	Goal        string

	Rubric   string
	Criteria []string
	Anchors  []anchor
	ScaleMin int
	ScaleMax int

	A, B string // pairwise
}

func renderInput(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	if len(m) == 1 {
		for _, v := range m {
			return toText(v)
		}
	}
	var b strings.Builder
	for i, k := range sortedKeys(m) {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(toText(m[k]))
	}
	return b.String()
}

func renderToolCalls(calls []evalcore.ToolCall) string {
	var b strings.Builder
	for i, c := range calls {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d. %s(%s)", i+1, c.Name, string(c.Arguments))
		if len(c.Result) > 0 {
			fmt.Fprintf(&b, " → %s", trunc(string(c.Result), 2000))
		}
	}
	return b.String()
}

func expectedText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return toText(v)
	}
	return string(b)
}

func lastAssistant(ms []evalcore.Message) string {
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].Role == "assistant" {
			return ms[i].Content
		}
	}
	return ""
}

func buildPromptData(in Input) promptData {
	ex, r := in.example(), in.response()
	d := promptData{
		Input:    trunc(renderInput(ex.Input), maxFieldChars),
		Output:   r.Output,
		Expected: trunc(expectedText(ex.Expected), maxFieldChars),
		ScaleMin: 1, ScaleMax: 5,
	}
	if d.Output == "" && len(r.Transcript) > 0 {
		d.Output = lastAssistant(r.Transcript)
	}
	d.Output = trunc(d.Output, maxFieldChars)
	d.HasExpected = strings.TrimSpace(d.Expected) != ""
	d.Context = r.Context
	if len(d.Context) == 0 {
		d.Context = ex.Context
	}
	if len(r.Transcript) > 0 {
		d.Transcript = trunc(renderTranscript(r), maxFieldChars*2)
	} else if ex.Conversation != nil && len(ex.Conversation.Turns) > 0 && d.Output != "" {
		// Scripted conversation without a recorded transcript: approximate.
		var b strings.Builder
		for _, t := range ex.Conversation.Turns {
			fmt.Fprintf(&b, "%s: %s\n", t.Role, t.Content)
		}
		fmt.Fprintf(&b, "assistant: %s", d.Output)
		d.Transcript = b.String()
	}
	d.ToolCalls = trunc(renderToolCalls(toolCalls(r)), maxFieldChars)
	if ex.Conversation != nil && ex.Conversation.Simulation != nil {
		d.Goal = ex.Conversation.Simulation.Goal
	}
	if d.Goal == "" {
		if g, ok := ex.Metadata["goal"].(string); ok {
			d.Goal = g
		}
	}
	if d.Input == "" && ex.Conversation != nil && ex.Conversation.Simulation != nil && ex.Conversation.Simulation.FirstMessage != "" {
		d.Input = ex.Conversation.Simulation.FirstMessage
	}
	return d
}

func renderPrompt(name string, d promptData) (string, error) {
	var b bytes.Buffer
	if err := promptTmpl.ExecuteTemplate(&b, name, d); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", name, err)
	}
	return strings.TrimSpace(b.String()), nil
}

func checkRequires(kind string, req []string, d promptData) error {
	for _, r := range req {
		switch r {
		case NeedsExpected:
			if !d.HasExpected {
				return fmt.Errorf("%s needs an expected output: add one to the example", kind)
			}
		case NeedsContext:
			if len(d.Context) == 0 {
				return fmt.Errorf("%s needs context: the target response has no retrieved context and the example has none", kind)
			}
		case NeedsTranscript:
			if d.Transcript == "" {
				return fmt.Errorf("%s needs a multi-turn conversation transcript", kind)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- score judge

// scoreJudge grades on an integer scale and normalizes to 0..1.
type scoreJudge struct {
	judgeBase
	tmpl      string
	requires  []string
	threshold float64
	fill      func(*promptData)
}

type scoreSample struct {
	Raw      float64 `json:"raw"` // on the judge scale, possibly logprob-weighted
	Int      float64 `json:"int"` // the score the judge wrote
	Reason   string  `json:"reason"`
	Weighted bool    `json:"weighted"`
	CostUSD  float64 `json:"cost_usd"`
	Cached   bool    `json:"-"`
}

func (j *scoreJudge) Evaluate(ctx context.Context, in Input) (evalcore.Score, error) {
	d := buildPromptData(in)
	if j.fill != nil {
		j.fill(&d)
	}
	if err := checkRequires(j.kind, j.requires, d); err != nil {
		return evalcore.Score{}, err
	}
	user, err := renderPrompt(j.tmpl, d)
	if err != nil {
		return evalcore.Score{}, err
	}
	lo, hi := d.ScaleMin, d.ScaleMax
	samples, errs := runSamples(ctx, j.cfg.Samples, func(ctx context.Context, i int) (scoreSample, error) {
		s, hit, err := cached(ctx, j.cache, j.cacheKey(user, "s"+strconv.Itoa(i)), func() (scoreSample, error) {
			return j.sample(ctx, user, lo, hi)
		})
		s.Cached = hit
		return s, err
	})
	if len(samples) == 0 {
		return evalcore.Score{}, errs[0]
	}
	raws := make([]float64, len(samples))
	cost := 0.0
	allCached, anyWeighted := true, false
	for i, s := range samples {
		raws[i] = s.Raw
		if !s.Cached {
			cost += s.CostUSD
			allCached = false
		}
		anyWeighted = anyWeighted || s.Weighted
	}
	med := median(raws)
	// The reason of the sample closest to the median explains the grade.
	best := 0
	for i := range samples {
		if math.Abs(raws[i]-med) < math.Abs(raws[best]-med) {
			best = i
		}
	}
	v := (med - float64(lo)) / float64(hi-lo)
	sc := valueScore(v, j.threshold, samples[best].Reason)
	sc.CostUSD = cost
	sc.Meta = map[string]any{
		"judge_score":      med,
		"scale":            []int{lo, hi},
		MetaPromptVersion:  j.promptVersion(),
		"judge_model":      j.cfg.Provider + "/" + j.cfg.Model,
		"logprob_weighted": anyWeighted,
	}
	if allCached {
		sc.Meta[MetaCached] = true
	}
	if len(samples) > 1 || len(errs) > 0 {
		sc.Meta["samples"] = raws
		if len(errs) > 0 {
			sc.Meta["sample_errors"] = len(errs)
		}
	}
	return sc, nil
}

func (j *scoreJudge) sample(ctx context.Context, user string, lo, hi int) (scoreSample, error) {
	wantLP := j.cfg.Logprobs != LogprobsOff
	var n float64
	obj, resp, convo, cost, err := j.completeJSON(ctx, user, "score", wantLP && j.cfg.Logprobs == LogprobsAuto,
		func(obj map[string]any) (err error) {
			n, err = parseScore(obj["score"], lo, hi)
			return err
		})
	if err != nil {
		return scoreSample{CostUSD: cost}, err
	}
	s := scoreSample{Raw: n, Int: n, Reason: reasonOf(obj), CostUSD: cost}
	switch j.cfg.Logprobs {
	case LogprobsAuto:
		if w, ok := weightedScore(resp.TopLogprobs, lo, hi); ok {
			s.Raw, s.Weighted = w, true
		}
	case LogprobsTwoPass:
		follow := append(convo, evalcore.Message{Role: "user", Content: fmt.Sprintf(
			"Output only your final score as a single integer from %d to %d, nothing else.", lo, hi)})
		r2, err := j.llm.Complete(ctx, evalcore.LLMRequest{
			Model: j.cfg.Model, System: systemPrompt, Messages: follow,
			Temperature: 0, MaxTokens: 1, Logprobs: true,
		})
		if err == nil {
			s.CostUSD += r2.CostUSD
			if w, ok := weightedScore(r2.TopLogprobs, lo, hi); ok {
				s.Raw, s.Weighted = w, true
			}
		}
	}
	return s, nil
}
