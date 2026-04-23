package iter

import (
	"errors"
	"sync"
)

// MapErr is a concurrent version of Map that allows the mapping function to
// return an error.
// If one or more mapping operations return an error, MapErr returns the
// successfully mapped results along with a joined error containing all mapping
// errors. The returned results are ordered by completion time and may differ
// from the input order.
func MapErr[T, R any](
	input []T,
	f func(*T) (R, error),
) ([]R, error) {
	var wg sync.WaitGroup
	var errs []error
	var muErrs, muMembers sync.Mutex
	out := make([]R, 0, len(input))
	for i := range input {
		entry := &input[i]
		wg.Go(func() {
			res, err := f(entry)
			if err != nil {
				muErrs.Lock()
				errs = append(errs, err)
				muErrs.Unlock()
				return
			}
			muMembers.Lock()
			out = append(out, res)
			muMembers.Unlock()
		})
	}
	wg.Wait()
	return out, errors.Join(errs...)
}
