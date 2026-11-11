package mock

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestAccuracyAndVariantOverrides(t *testing.T) {
	h := NewServer(DefaultConfig())
	ask := func(q, query string) string {
		rec := post(t, h, "/rag?"+query, `{"question":`+strconv.Quote(q)+`}`)
		var b map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &b)
		if _, ok := b["cost_usd"].(float64); !ok {
			t.Fatalf("rag response has no cost_usd: %v", b)
		}
		return b["answer"].(string)
	}
	diff, right := 0, 0
	for i := range 200 {
		q := "question " + strconv.Itoa(i)
		a1, a2 := ask(q, "mock_variant=v1&mock_accuracy=0.8"), ask(q, "mock_variant=v2&mock_accuracy=0.8")
		if (a1 == Truth(q)) != IsCorrectVariant(q, 0.8, "v1") {
			t.Fatal("IsCorrectVariant disagrees with the server")
		}
		if a1 != a2 {
			diff++
		}
		if ask(q, "mock_accuracy=1") == Truth(q) {
			right++
		}
	}
	if diff == 0 || right != 200 {
		t.Fatalf("variants differ on %d questions, accuracy=1 right on %d/200", diff, right)
	}
}
