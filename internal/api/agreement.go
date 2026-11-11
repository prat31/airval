package api

import (
	"math"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/api/gen"
	"github.com/prat31/airval/internal/store"
)

// computeAgreement compares an evaluator's scores with human annotations. Several
// annotations on one result are collapsed (majority verdict, mean value). Annotation values are
// expected on the same 0..1 scale as scores.
//
//   - Cohen's kappa and percent agreement use the pass/fail verdicts (pass = positive class).
//   - Pearson correlation and mean absolute error use the numeric values.
func computeAgreement(evaluatorID uuid.UUID, pairs []store.AgreementPair) gen.EvaluatorAgreement {
	out := gen.EvaluatorAgreement{EvaluatorId: evaluatorID, N: len(pairs)}
	var tp, fp, fn, tn int
	var xs, ys []float64 // human, judge
	for _, p := range pairs {
		if p.JudgePassed != nil && len(p.HumanPassed) > 0 {
			yes := 0
			for _, v := range p.HumanPassed {
				if v {
					yes++
				}
			}
			human := yes*2 > len(p.HumanPassed) // ties count as fail: be conservative
			switch judge := *p.JudgePassed; {
			case human && judge:
				tp++
			case !human && judge:
				fp++
			case human && !judge:
				fn++
			default:
				tn++
			}
		}
		if p.JudgeValue != nil && len(p.HumanValues) > 0 {
			sum := 0.0
			for _, v := range p.HumanValues {
				sum += v
			}
			xs = append(xs, sum/float64(len(p.HumanValues)))
			ys = append(ys, *p.JudgeValue)
		}
	}
	nb := tp + fp + fn + tn
	out.NBinary, out.NNumeric = ptr(nb), ptr(len(xs))
	if nb > 0 {
		n := float64(nb)
		po := float64(tp+tn) / n
		out.PercentAgreement = ptr(po)
		humanPos, judgePos := float64(tp+fn)/n, float64(tp+fp)/n
		out.HumanPassRate, out.JudgePassRate = ptr(humanPos), ptr(judgePos)
		pe := humanPos*judgePos + (1-humanPos)*(1-judgePos)
		if pe < 1 {
			out.CohensKappa = ptr(round6((po - pe) / (1 - pe)))
		}
		out.Confusion = &struct {
			Fn *int `json:"fn,omitempty"`
			Fp *int `json:"fp,omitempty"`
			Tn *int `json:"tn,omitempty"`
			Tp *int `json:"tp,omitempty"`
		}{Fn: ptr(fn), Fp: ptr(fp), Tn: ptr(tn), Tp: ptr(tp)}
	}
	if len(xs) > 0 {
		var mae float64
		for i := range xs {
			mae += math.Abs(xs[i] - ys[i])
		}
		out.MeanAbsError = ptr(mae / float64(len(xs)))
		if r, ok := pearson(xs, ys); ok {
			out.Pearson = ptr(round6(r))
		}
	}
	return out
}

func pearson(x, y []float64) (float64, bool) {
	n := float64(len(x))
	if len(x) < 2 {
		return 0, false
	}
	var mx, my float64
	for i := range x {
		mx += x[i]
		my += y[i]
	}
	mx, my = mx/n, my/n
	var sxy, sxx, syy float64
	for i := range x {
		dx, dy := x[i]-mx, y[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
