package main

import (
	"errors"
	"sync"
)

// forEachLimited runs fn for every item, with at most limit goroutines running
// concurrently. It returns all errors joined.
func forEachLimited[E any](items []E, limit int, fn func(item E) error) error {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []error
	)
	sem := make(chan struct{}, limit)
	for _, item := range items {
		// Acquire the semaphore before spawning, so that at most limit
		// goroutines exist at any time.
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			if err := fn(item); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}
