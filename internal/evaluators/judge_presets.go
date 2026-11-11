package evaluators

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/prat31/airval/internal/evalcore"
)

// PresetConfig configures a preset LLM judge. Presets grade on a 1–5 scale
// with written anchors; the score is normalized to 0..1 (higher is always
// better, so for hallucination, toxicity and bias 1.0 means "none").
type PresetConfig struct {
	JudgeConfig
}

func (c *PresetConfig) Validate() error { return c.JudgeConfig.validate() }

type presetDef struct {
	kind, name, group, file, version, desc string
	requires                               []string
	applyTo                                []evalcore.ApplyTo
}

var (
	applyText = []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn}
	applyConv = []evalcore.ApplyTo{evalcore.ApplyConversation}
	applyAll  = []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyConversation}
	applyTool = []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyConversation, evalcore.ApplyTrace}
)

// Preset prompt versions: bump the version whenever a template's wording
// changes, so cached grades and run comparisons stay honest.
var presets = []presetDef{
	{"correctness", "Correctness", groupQuality, "correctness.tmpl", "1",
		"Does the answer agree with the expected answer? An LLM judge compares meaning, not wording, so paraphrases and extra correct detail still pass.",
		[]string{NeedsExpected}, applyText},
	{"faithfulness", "Faithfulness", groupRAG, "faithfulness.tmpl", "1",
		"Is every claim in the answer supported by the retrieved context? Catches answers that go beyond, or contradict, what your RAG pipeline retrieved.",
		[]string{NeedsContext}, applyText},
	{"groundedness", "Groundedness", groupRAG, "faithfulness.tmpl", "1",
		"Same check as faithfulness under its other common name: is the answer grounded in the provided context?",
		[]string{NeedsContext}, applyText},
	{"answer_relevance", "Answer relevance", groupQuality, "answer_relevance.tmpl", "1",
		"Does the answer address what the user actually asked, fully and on topic? Independent of whether it is correct.",
		nil, applyText},
	{"context_relevance", "Context relevance", groupRAG, "context_relevance.tmpl", "1",
		"Are the retrieved passages relevant to the question? Grades your retriever, not the generated answer.",
		[]string{NeedsContext}, []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyTrace}},
	{"context_recall", "Context recall", groupRAG, "context_recall.tmpl", "1",
		"Does the retrieved context contain everything needed to produce the expected answer? Low scores point to retrieval misses.",
		[]string{NeedsExpected, NeedsContext}, []evalcore.ApplyTo{evalcore.ApplyFinal, evalcore.ApplyPerTurn, evalcore.ApplyTrace}},
	{"hallucination", "No hallucination", groupQuality, "hallucination.tmpl", "1",
		"Does the answer invent facts — names, numbers, citations, events? Uses the context and expected answer when available. 1.0 means no hallucinations.",
		nil, applyText},
	{"toxicity", "No toxicity", groupSafety, "toxicity.tmpl", "1",
		"Is the answer free of insults, harassment, hate, threats and other toxic content? 1.0 means not toxic.",
		nil, applyAll},
	{"bias", "No bias", groupSafety, "bias.tmpl", "1",
		"Is the answer free of stereotypes and unfair treatment of groups, and balanced on contested topics? 1.0 means unbiased.",
		nil, applyAll},
	{"conciseness", "Conciseness", groupQuality, "conciseness.tmpl", "1",
		"Does the answer say what is needed without filler, repetition or unrequested detail? Long answers can still be concise.",
		nil, applyText},
	{"helpfulness", "Helpfulness", groupQuality, "helpfulness.tmpl", "1",
		"How well does the answer help the user reach their goal — accurate, specific and actionable, like an expert assistant?",
		nil, applyAll},
	{"conversation_goal_completion", "Goal completion", groupConv, "conversation_goal_completion.tmpl", "1",
		"Did the assistant accomplish the user's goal by the end of the conversation? Uses the simulation goal or metadata.goal when set.",
		[]string{NeedsTranscript}, applyConv},
	{"conversation_coherence", "Conversation coherence", groupConv, "conversation_coherence.tmpl", "1",
		"Does the assistant stay consistent across turns, remember what was said, and respond to the latest message?",
		[]string{NeedsTranscript}, applyConv},
	{"tool_use_correctness", "Tool use correctness", groupTools, "tool_use_correctness.tmpl", "1",
		"Did the agent pick the right tools, with correct arguments, in a sensible order, and use the results properly? An LLM judge reviews the tool calls.",
		nil, applyTool},
}

// presetThreshold: a score of 4 of 5 (0.75) passes.
const presetThreshold = 0.75

func init() {
	for _, p := range presets {
		register(KindInfo{
			Kind: p.kind, Name: p.name, Category: CategoryModel, Group: p.group,
			Cost: CostModel, DefaultThreshold: presetThreshold, Description: p.desc,
			Requires: p.requires, ApplyTo: p.applyTo,
			PromptVersion: p.version, Prompt: promptSource(p.file),
		}, func(c *PresetConfig, deps Deps) (Evaluator, error) {
			base, err := newJudgeBase(p.kind, p.version, &c.JudgeConfig, deps)
			if err != nil {
				return nil, err
			}
			return &scoreJudge{judgeBase: base, tmpl: p.file, requires: p.requires, threshold: presetThreshold}, nil
		})
	}

	register(KindInfo{
		Kind: "llm_rubric", Name: "Custom rubric (LLM judge)", Category: CategoryModel, Group: groupQuality,
		Cost: CostModel, DefaultThreshold: 0.75, PromptVersion: "1", Prompt: promptSource("llm_rubric.tmpl"),
		Description: "Write your own grading rubric in plain language. An LLM judge reasons step by step (G-Eval style), scores on your scale, and explains its grade.",
	}, func(c *RubricConfig, deps Deps) (Evaluator, error) {
		base, err := newJudgeBase("llm_rubric", "1", &c.JudgeConfig, deps)
		if err != nil {
			return nil, err
		}
		anchors := c.anchors()
		return &scoreJudge{judgeBase: base, tmpl: "llm_rubric.tmpl", threshold: 0.75, fill: func(d *promptData) {
			d.Rubric, d.Criteria, d.Anchors = c.Rubric, c.Criteria, anchors
			d.ScaleMin, d.ScaleMax = *c.ScaleMin, c.ScaleMax
		}}, nil
	})
}

// RubricConfig configures llm_rubric.
type RubricConfig struct {
	JudgeConfig
	Rubric   string            `json:"rubric" title:"Rubric" ui:"textarea" desc:"What a good output looks like, in plain language. Be specific: \"Mentions the refund window and links to the policy page\" grades better than \"Is helpful\"."`
	Criteria []string          `json:"criteria,omitempty" title:"Criteria" ui:"list" desc:"Optional checklist the judge verifies one by one."`
	ScaleMin *int              `json:"scale_min,omitempty" title:"Scale minimum" default:"1" min:"0" max:"9" desc:"Lowest score on the scale."`
	ScaleMax int               `json:"scale_max,omitempty" title:"Scale maximum" default:"5" min:"1" max:"10" desc:"Highest score on the scale. Scores are normalized to 0–1."`
	Anchors  map[string]string `json:"anchors,omitempty" title:"Score anchors" ui:"json" desc:"Optional description of each score, e.g. {\"5\": \"Fully correct and cites the source\", \"1\": \"Wrong or missing\"}. Anchors make grades far more consistent."`
}

func (c *RubricConfig) Validate() error {
	if err := c.JudgeConfig.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Rubric) == "" {
		return errors.New("rubric is required")
	}
	if c.ScaleMin == nil {
		one := 1
		c.ScaleMin = &one
	}
	if c.ScaleMax == 0 {
		c.ScaleMax = 5
	}
	lo := *c.ScaleMin
	if lo < 0 || c.ScaleMax > 10 || lo >= c.ScaleMax {
		return fmt.Errorf("scale must satisfy 0 <= scale_min < scale_max <= 10 (got %d..%d)", lo, c.ScaleMax)
	}
	for k := range c.Anchors {
		n, err := strconv.Atoi(k)
		if err != nil || n < lo || n > c.ScaleMax {
			return fmt.Errorf("anchors: key %q is not a score on the %d-%d scale", k, lo, c.ScaleMax)
		}
	}
	return nil
}

func (c *RubricConfig) anchors() []anchor {
	out := make([]anchor, 0, len(c.Anchors))
	for k, v := range c.Anchors {
		n, _ := strconv.Atoi(k)
		out = append(out, anchor{Score: n, Text: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
