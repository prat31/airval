package evaluators

import (
	"context"
	"fmt"
	"strconv"

	"github.com/airval/airval/internal/evalcore"
)

// PairwiseConfig configures pairwise.
type PairwiseConfig struct {
	JudgeConfig
	Rubric   string   `json:"rubric,omitempty" title:"What makes a response better" ui:"textarea" desc:"Optional criteria for the comparison. Empty means overall quality: correct, helpful, and following the request."`
	Criteria []string `json:"criteria,omitempty" title:"Criteria" ui:"list" desc:"Optional bullet-point criteria."`
	NoSwap   bool     `json:"no_swap,omitempty" title:"Disable position swap" desc:"By default each comparison runs twice with the responses in both orders to cancel the judge's position bias. Turn off to halve cost."`
}

func (c *PairwiseConfig) Validate() error { return c.JudgeConfig.validate() }

// Pairwise outcome labels.
const (
	LabelWin        = "win"
	LabelTie        = "tie"
	LabelLoss       = "loss"
	LabelNoBaseline = "no_baseline"
)

const pairwiseVersion = "1"

type pairwiseJudge struct {
	judgeBase
	c *PairwiseConfig
}

type verdictSample struct {
	Verdict string  `json:"verdict"`
	Reason  string  `json:"reason"`
	CostUSD float64 `json:"cost_usd"`
	Cached  bool    `json:"-"`
	order   int
}

func init() {
	register(KindInfo{
		Kind: "pairwise", Name: "Pairwise vs baseline", Category: CategoryModel, Group: groupCompare,
		Cost: CostModel, DefaultThreshold: 0.5, Requires: []string{NeedsBaseline},
		ApplyTo:       []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyConversation},
		PromptVersion: pairwiseVersion, Prompt: promptSource("pairwise.tmpl"),
		Description: "An LLM judge compares each output with the baseline run's output for the same example: win = 1, tie = 0.5, loss = 0. Runs both orderings to remove position bias. Passes when not worse than the baseline.",
	}, func(c *PairwiseConfig, deps Deps) (Evaluator, error) {
		base, err := newJudgeBase("pairwise", pairwiseVersion, &c.JudgeConfig, deps)
		if err != nil {
			return nil, err
		}
		return &pairwiseJudge{judgeBase: base, c: c}, nil
	})
}

// candidateOutcome maps a verdict in one ordering to the candidate's
// result: 1 win, 0.5 tie, 0 loss. candIsA says whether the candidate was
// shown as response A.
func candidateOutcome(verdict string, candIsA bool) float64 {
	switch {
	case verdict == "tie":
		return 0.5
	case (verdict == "A") == candIsA:
		return 1
	default:
		return 0
	}
}

// majority returns the most common verdict; a split vote between A and B
// with no clear winner is a tie.
func majority(vs []string) string {
	counts := map[string]int{}
	for _, v := range vs {
		counts[v]++
	}
	best, bestN, tied := "tie", 0, false
	for _, k := range []string{"A", "B", "tie"} {
		switch n := counts[k]; {
		case n > bestN:
			best, bestN, tied = k, n, false
		case n == bestN && n > 0:
			tied = true
		}
	}
	if tied {
		return "tie"
	}
	return best
}

func (j *pairwiseJudge) Evaluate(ctx context.Context, in Input) (evalcore.Score, error) {
	if in.Baseline == nil {
		return evalcore.Score{
			Label:  LabelNoBaseline,
			Reason: "no baseline response for this example; nothing to compare against",
			Meta:   map[string]any{MetaSkipped: true},
		}, nil
	}
	d := buildPromptData(in)
	d.Rubric, d.Criteria = j.c.Rubric, j.c.Criteria
	cand := d.Output
	baseText := in.Baseline.Output
	if baseText == "" && len(in.Baseline.Transcript) > 0 {
		baseText = renderTranscript(in.Baseline)
		cand = d.Transcript
	}
	baseText = trunc(baseText, maxFieldChars)

	type order struct{ candIsA bool }
	orders := []order{{true}}
	if !j.c.NoSwap {
		orders = append(orders, order{false})
	}
	prompts := make([]string, len(orders))
	for i, o := range orders {
		dd := d
		if o.candIsA {
			dd.A, dd.B = cand, baseText
		} else {
			dd.A, dd.B = baseText, cand
		}
		p, err := renderPrompt("pairwise.tmpl", dd)
		if err != nil {
			return evalcore.Score{}, err
		}
		prompts[i] = p
	}
	n := j.cfg.Samples
	all, errs := runSamples(ctx, n*len(orders), func(ctx context.Context, k int) (verdictSample, error) {
		oi, si := k/n, k%n
		user := prompts[oi]
		v, hit, err := cached(ctx, j.cache, j.cacheKey(user, "p"+strconv.Itoa(si)), func() (verdictSample, error) {
			var verdict string
			obj, _, _, cost, err := j.completeJSON(ctx, user, "verdict", false, func(obj map[string]any) (err error) {
				verdict, err = normalizeVerdict(obj["verdict"])
				return err
			})
			if err != nil {
				return verdictSample{CostUSD: cost}, err
			}
			return verdictSample{Verdict: verdict, Reason: reasonOf(obj), CostUSD: cost}, nil
		})
		v.Cached, v.order = hit, oi
		return v, err
	})
	if len(all) == 0 {
		return evalcore.Score{}, errs[0]
	}
	byOrder := make([][]string, len(orders))
	reasons := make([]string, len(orders))
	cost, allCached := 0.0, true
	for _, s := range all {
		oi := s.order
		byOrder[oi] = append(byOrder[oi], s.Verdict)
		if !s.Cached {
			cost += s.CostUSD
			allCached = false
		}
	}
	total, used := 0.0, 0
	verdicts := make([]string, len(orders))
	outcomes := make([]float64, len(orders))
	for oi, o := range orders {
		if len(byOrder[oi]) == 0 {
			verdicts[oi] = ""
			continue
		}
		verdicts[oi] = majority(byOrder[oi])
		outcomes[oi] = candidateOutcome(verdicts[oi], o.candIsA)
		total += outcomes[oi]
		used++
		for _, s := range all {
			if s.order == oi && s.Verdict == verdicts[oi] {
				reasons[oi] = s.Reason
				break
			}
		}
		if reasons[oi] == "" { // split vote resolved to tie
			for _, s := range all {
				if s.order == oi {
					reasons[oi] = s.Reason
					break
				}
			}
		}
	}
	v := total / float64(used)
	sc := valueScore(v, 0.5, "")
	switch {
	case v > 0.5:
		sc.Label = LabelWin
	case v < 0.5:
		sc.Label = LabelLoss
	default:
		sc.Label = LabelTie
	}
	reason := reasons[0]
	if reason == "" && len(reasons) > 1 {
		reason = reasons[1]
	}
	consistent := used == 2 && outcomes[0] == outcomes[1]
	if used == 2 && !consistent {
		reason = fmt.Sprintf("The judge's preference changed when the order was swapped, so this counts as %s. %s", sc.Label, reason)
	}
	sc.Reason = reason
	sc.CostUSD = cost
	sc.Meta = map[string]any{
		"verdicts":            verdicts, // per ordering, as the judge saw it (A/B/tie)
		"candidate_first":     []bool{true, false}[:len(orders)],
		"position_consistent": consistent || used == 1,
		MetaPromptVersion:     j.promptVersion(),
		"judge_model":         j.cfg.Provider + "/" + j.cfg.Model,
	}
	if allCached {
		sc.Meta[MetaCached] = true
	}
	if len(errs) > 0 {
		sc.Meta["sample_errors"] = len(errs)
	}
	return sc, nil
}
