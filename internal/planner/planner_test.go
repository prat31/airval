package planner

import (
	"testing"

	"github.com/google/uuid"
)

func cands(n int) []candidate {
	out := make([]candidate, n)
	for i := range out {
		tag := "a"
		if i%4 == 0 {
			tag = "b" // 25%
		}
		out[i] = candidate{item: Item{E: uuid.New()}, tag: tag}
	}
	return out
}

func TestSampleDeterministic(t *testing.T) {
	c := cands(1000)
	s := &Sampling{Mode: "random", N: 100}
	a, b := sample(c, s, 42), sample(c, s, 42)
	if len(a) != 100 {
		t.Fatalf("n = %d", len(a))
	}
	for i := range a {
		if a[i].item.E != b[i].item.E {
			t.Fatal("same seed must give the same sample")
		}
	}
	if d := sample(c, s, 43); d[0].item.E == a[0].item.E && d[1].item.E == a[1].item.E && d[2].item.E == a[2].item.E {
		t.Fatal("different seeds should differ")
	}
	if p := sample(c, &Sampling{Mode: "random", Percent: 10}, 1); len(p) != 100 {
		t.Fatalf("percent: %d", len(p))
	}
	if all := sample(c, &Sampling{Mode: "none", N: 5}, 1); len(all) != 1000 {
		t.Fatal("mode none must not sample")
	}
}

func TestSampleStratified(t *testing.T) {
	c := cands(1000)
	got := sample(c, &Sampling{Mode: "stratified", N: 200}, 7)
	if len(got) != 200 {
		t.Fatalf("n = %d", len(got))
	}
	b := 0
	for _, x := range got {
		if x.tag == "b" {
			b++
		}
	}
	if b != 50 {
		t.Fatalf("stratum b = %d, want 50 (25%%)", b)
	}
}

func TestShardSize(t *testing.T) {
	for n, want := range map[int]int{1: 25, 1000: 25, 3200: 50, 6400: 100, 1_000_000: 100} {
		if got := ShardSize(n); got != want {
			t.Fatalf("ShardSize(%d) = %d, want %d", n, got, want)
		}
	}
}
