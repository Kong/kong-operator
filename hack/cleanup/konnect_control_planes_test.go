package main

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestUserRolesRequestURL(t *testing.T) {
	u, err := userRolesRequestURL("https://global.api.konghq.com", "63da16fc-94e7-4cbf-bf12-11111111111111", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := u.String(),
		"https://global.api.konghq.com/v3/users/63da16fc-94e7-4cbf-bf12-11111111111111/assigned-roles?page%5Bnumber%5D=3&page%5Bsize%5D=100"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	q := u.Query()
	if got, want := q.Get("page[size]"), "100"; got != want {
		t.Errorf("page[size] = %q, want %q", got, want)
	}
	if got, want := q.Get("page[number]"), "3"; got != want {
		t.Errorf("page[number] = %q, want %q", got, want)
	}
}

func TestForEachLimited(t *testing.T) {
	var (
		mu          sync.Mutex
		processed   []int
		current     atomic.Int64
		maxObserved atomic.Int64
	)
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}

	err := forEachLimited(items, 4, func(item int) error {
		if cur := current.Add(1); cur > maxObserved.Load() {
			maxObserved.Store(cur)
		}
		defer current.Add(-1)
		mu.Lock()
		defer mu.Unlock()
		processed = append(processed, item)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(processed)
	if !slices.Equal(processed, items) {
		t.Errorf("not all items processed exactly once: got %d items", len(processed))
	}
	if got := maxObserved.Load(); got > 4 {
		t.Errorf("concurrency limit exceeded: %d goroutines ran concurrently, limit is 4", got)
	}

	wantErr := errors.New("boom")
	err = forEachLimited(items[:3], 2, func(item int) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("errors not propagated: got %v, want %v", err, wantErr)
	}
}
