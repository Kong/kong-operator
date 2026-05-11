package iter

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapErr(t *testing.T) {
	type result struct {
		values []string
		err    error
	}

	errTwo := errors.New("two")
	errFour := errors.New("four")

	tests := []struct {
		name     string
		call     func(t *testing.T) result
		want     []string
		wantErrs []error
	}{
		{
			name: "returns empty output for empty input",
			call: func(t *testing.T) result {
				got, err := MapErr([]int{}, func(value *int) (string, error) {
					return fmt.Sprintf("value-%d", *value), nil
				})
				return result{values: got, err: err}
			},
			want: []string{},
		},
		{
			name: "waits for all mappings and returns successful results",
			call: func(t *testing.T) result {
				input := []int{1, 2, 3}
				release := make(chan struct{})
				started := make(chan struct{}, len(input))
				done := make(chan result, 1)

				go func() {
					got, err := MapErr(input, func(value *int) (string, error) {
						started <- struct{}{}
						<-release
						return fmt.Sprintf("value-%d", *value), nil
					})
					done <- result{values: got, err: err}
				}()

				for range input {
					select {
					case <-started:
					case <-time.After(time.Second):
						t.Fatal("timed out waiting for mapper to start")
					}
				}

				close(release)

				select {
				case res := <-done:
					return res
				case <-time.After(time.Second):
					t.Fatal("MapErr did not wait for all mappers to finish")
					return result{}
				}
			},
			want: []string{"value-1", "value-2", "value-3"},
		},
		{
			name: "joins mapper errors and keeps successful results",
			call: func(t *testing.T) result {
				got, err := MapErr([]int{1, 2, 3, 4}, func(value *int) (string, error) {
					switch *value {
					case 2:
						return "", errTwo
					case 4:
						return "", errFour
					default:
						return fmt.Sprintf("value-%d", *value), nil
					}
				})
				return result{values: got, err: err}
			},
			want:     []string{"value-1", "value-3"},
			wantErrs: []error{errTwo, errFour},
		},
		{
			name: "passes a stable pointer for each input element",
			call: func(t *testing.T) result {
				input := []int{1, 2, 3}
				var (
					mu       sync.Mutex
					ptrAddrs []string
				)

				got, err := MapErr(input, func(value *int) (string, error) {
					mu.Lock()
					ptrAddrs = append(ptrAddrs, fmt.Sprintf("%p", value))
					mu.Unlock()
					return fmt.Sprintf("value-%d", *value), nil
				})

				require.NoError(t, err)
				require.Len(t, ptrAddrs, len(input))

				seen := make(map[string]struct{}, len(ptrAddrs))
				for _, ptrAddr := range ptrAddrs {
					seen[ptrAddr] = struct{}{}
				}
				require.Len(t, seen, len(input), "each mapper call should receive a pointer to a distinct input element")

				return result{values: got, err: err}
			},
			want: []string{"value-1", "value-2", "value-3"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.call(t)

			assert.ElementsMatch(t, tc.want, res.values)

			if len(tc.wantErrs) == 0 {
				require.NoError(t, res.err)
				return
			}

			require.Error(t, res.err)
			for _, wantErr := range tc.wantErrs {
				require.ErrorIs(t, res.err, wantErr)
			}
		})
	}
}

func TestMapErr_capsConcurrentMappersToGOMAXPROCS(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(previous)
	})

	input := []int{1, 2, 3, 4}
	limit := runtime.GOMAXPROCS(0)
	started := make(chan struct{}, len(input))
	release := make(chan struct{})
	done := make(chan struct {
		values []string
		err    error
	}, 1)

	go func() {
		got, err := MapErr(input, func(value *int) (string, error) {
			started <- struct{}{}
			<-release
			return fmt.Sprintf("value-%d", *value), nil
		})
		done <- struct {
			values []string
			err    error
		}{values: got, err: err}
	}()

	for range limit {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for capped workers to start")
		}
	}

	select {
	case <-started:
		t.Fatalf("mapper exceeded concurrency cap of %d before any worker was released", limit)
	default:
	}

	close(release)

	select {
	case res := <-done:
		require.NoError(t, res.err)
		assert.ElementsMatch(t, []string{"value-1", "value-2", "value-3", "value-4"}, res.values)
	case <-time.After(time.Second):
		t.Fatal("MapErr did not finish after releasing capped workers")
	}
}
