package cache

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/airval/airval/internal/store/sqlstore"
)

func TestTwoLevel(t *testing.T) {
	ctx := context.Background()
	st, err := sqlstore.Open(ctx, filepath.Join(t.TempDir(), "c.db"), sqlstore.Options{Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a := New(st, Options{MaxEntries: 2})
	k := Key("judge", []byte("model"), []byte("prompt"))
	if _, ok, _ := a.Get(ctx, k); ok {
		t.Fatal("unexpected hit")
	}
	_ = a.Set(ctx, k, []byte("v1"))
	if v, ok, _ := a.Get(ctx, k); !ok || string(v) != "v1" {
		t.Fatalf("local hit: %q %v", v, ok)
	}
	a.Close() // flush to the store

	// A second worker sees the value through the store.
	b := New(st, Options{})
	defer b.Close()
	if v, ok, err := b.Get(ctx, k); err != nil || !ok || string(v) != "v1" {
		t.Fatalf("store hit: %q %v %v", v, ok, err)
	}
	if _, _, sh := b.Stats(); sh != 1 {
		t.Fatalf("store hits = %d", sh)
	}
	// Writes after Close are memory-only and must not panic.
	_ = a.Set(ctx, "x", []byte("y"))
}

func TestLRUEviction(t *testing.T) {
	c := New(nil, Options{MaxEntries: 3})
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = c.Set(ctx, fmt.Sprint(i), []byte("v"))
	}
	if c.Len() != 3 {
		t.Fatalf("len = %d", c.Len())
	}
	if _, ok, _ := c.Get(ctx, "9"); !ok {
		t.Fatal("newest evicted")
	}
	if _, ok, _ := c.Get(ctx, "0"); ok {
		t.Fatal("oldest kept")
	}
}

func TestKeyLengthPrefix(t *testing.T) {
	if Key("n", []byte("ab"), []byte("c")) == Key("n", []byte("a"), []byte("bc")) {
		t.Fatal("ambiguous key")
	}
}

func TestConcurrent(t *testing.T) {
	c := New(nil, Options{MaxEntries: 100})
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				k := fmt.Sprint(i % 150)
				_ = c.Set(ctx, k, []byte(k))
				_, _, _ = c.Get(ctx, k)
			}
		}()
	}
	wg.Wait()
}
