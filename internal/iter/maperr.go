package iter

import (
	"errors"
	"runtime"
	"sync"
)

// MapErr is a concurrent version of Map that allows the mapping function to
// return an error.
// If one or more mapping operations return an error, MapErr returns the
// successfully mapped results along with a joined error containing all mapping
// errors. Mapping work is executed by up to [runtime.GOMAXPROCS](0) worker
// goroutines. The returned results are ordered by completion time and may
// differ from the input order.
func MapErr[T, R any](
	input []T,
	f func(*T) (R, error),
) ([]R, error) {
	if len(input) == 0 {
		return []R{}, nil
	}

	workers := min(runtime.GOMAXPROCS(0), len(input))

	var wg sync.WaitGroup
	var errs []error
	var muErrs, muMembers sync.Mutex
	jobs := make(chan int)
	out := make([]R, 0, len(input))
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				res, err := f(&input[i])
				if err != nil {
					muErrs.Lock()
					errs = append(errs, err)
					muErrs.Unlock()
					continue
				}
				muMembers.Lock()
				out = append(out, res)
				muMembers.Unlock()
			}
		})
	}
	for i := range input {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out, errors.Join(errs...)
}
