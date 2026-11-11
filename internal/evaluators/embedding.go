package evaluators

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/airval/airval/internal/evalcore"
)

// EmbeddingConfig configures embedding_similarity.
type EmbeddingConfig struct {
	Provider string `json:"provider" title:"Provider" desc:"Embedding provider, e.g. openai."`
	Model    string `json:"model" title:"Model" desc:"Embedding model, e.g. text-embedding-3-small."`
	Secret   string `json:"secret,omitempty" title:"API key secret" ui:"secret" desc:"Name of the secret that holds the provider API key."`
	FieldSelector
	ExpectedSource
	NoCache bool `json:"no_cache,omitempty" title:"Disable cache" desc:"Always re-embed instead of reusing cached vectors."`
}

func (c *EmbeddingConfig) Validate() error {
	if strings.TrimSpace(c.Provider) == "" {
		return errors.New("provider is required")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model is required")
	}
	if err := c.FieldSelector.validate(); err != nil {
		return err
	}
	return c.ExpectedSource.validate()
}

type embeddingEval struct {
	c     *EmbeddingConfig
	emb   Embedder
	cache Cache
}

func init() {
	register(KindInfo{
		Kind: "embedding_similarity", Name: "Semantic similarity", Category: CategoryModel, Group: groupSimilarity,
		Cost: CostModel, DefaultThreshold: 0.8, Requires: []string{NeedsExpected},
		Description: "Cosine similarity between embeddings of the output and the expected output: catches answers that mean the same thing in different words. Cheaper and faster than an LLM judge; vectors are cached.",
	}, func(c *EmbeddingConfig, deps Deps) (Evaluator, error) {
		if deps.Embedders == nil {
			return nil, ErrNoEmbedder
		}
		e, err := deps.Embedders(c.Provider, c.Model, c.Secret)
		if err != nil {
			return nil, fmt.Errorf("embedding model %s/%s: %w", c.Provider, c.Model, err)
		}
		ev := &embeddingEval{c: c, emb: e}
		if !c.NoCache {
			ev.cache = deps.Cache
		}
		return ev, nil
	})
}

func (e *embeddingEval) Kind() string    { return "embedding_similarity" }
func (e *embeddingEval) Cost() CostClass { return CostModel }

func (e *embeddingEval) key(text string) string {
	h := sha256.New()
	h.Write([]byte("emb/v1\x00" + e.c.Provider + "\x00" + e.c.Model + "\x00"))
	h.Write([]byte(text))
	return "emb:" + hex.EncodeToString(h.Sum(nil))
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeVec(b []byte) ([]float32, bool) {
	if len(b)%4 != 0 {
		return nil, false
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, true
}

// embed returns vectors for texts, serving what it can from the cache and
// embedding the rest in one batch.
func (e *embeddingEval) embed(ctx context.Context, texts []string) ([][]float32, int, error) {
	out := make([][]float32, len(texts))
	var missIdx []int
	var miss []string
	for i, t := range texts {
		if e.cache != nil {
			if b, ok, err := e.cache.Get(ctx, e.key(t)); err == nil && ok {
				if v, ok := decodeVec(b); ok {
					out[i] = v
					continue
				}
			}
		}
		missIdx = append(missIdx, i)
		miss = append(miss, t)
	}
	if len(miss) > 0 {
		vs, err := e.emb.Embed(ctx, miss)
		if err != nil {
			return nil, 0, fmt.Errorf("embedding call failed: %w", err)
		}
		if len(vs) != len(miss) {
			return nil, 0, fmt.Errorf("embedder returned %d vectors for %d texts", len(vs), len(miss))
		}
		for k, i := range missIdx {
			out[i] = vs[k]
			if e.cache != nil {
				_ = e.cache.Set(ctx, e.key(texts[i]), encodeVec(vs[k]))
			}
		}
	}
	return out, len(miss), nil
}

func cosine(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("vector sizes differ (%d vs %d)", len(a), len(b))
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0, nil
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb)), nil
}

func (e *embeddingEval) Evaluate(ctx context.Context, in Input) (evalcore.Score, error) {
	refs, err := e.c.ExpectedSource.resolveList(in)
	if err != nil {
		return evalcore.Score{}, err
	}
	text, err := e.c.FieldSelector.text(in)
	if err != nil {
		return valueScore(0, 0.8, err.Error()), nil
	}
	vecs, embedded, err := e.embed(ctx, append([]string{text}, refs...))
	if err != nil {
		return evalcore.Score{}, err
	}
	best := -1.0
	for _, rv := range vecs[1:] {
		c, err := cosine(vecs[0], rv)
		if err != nil {
			return evalcore.Score{}, err
		}
		best = math.Max(best, c)
	}
	s := valueScore(best, 0.8, fmt.Sprintf("cosine similarity %.3f", best))
	s.Meta = map[string]any{"cosine": best, "embedded": embedded}
	if embedded == 0 {
		s.Meta[MetaCached] = true
	}
	return s, nil
}
