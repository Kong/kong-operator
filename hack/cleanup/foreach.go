package main

import (
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"
)

// forEachLimited runs fn for every item, with at most limit goroutines running
// concurrently. It returns all errors joined.
func forEachLimited[E any](items []E, limit int, fn func(item E) error) error {
	var (
		mu   sync.Mutex
		errs []error
		g    errgroup.Group
	)

	g.SetLimit(limit)
	sem := make(chan struct{}, limit)
	for _, item := range items {
		// Acquire the semaphore before spawning, so that at most limit
		// goroutines exist at any time.
		sem <- struct{}{}
		g.Go(func() error {
			defer func() { <-sem }()
			if err := fn(item); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
			return nil
		})
	}
	g.Wait()
	return errors.Join(errs...)
}
