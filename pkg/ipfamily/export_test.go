package ipfamily

import "k8s.io/apimachinery/pkg/util/wait"

// SetDetectBackoff replaces the detection retry policy and returns a func
// restoring the previous one. Tests that exercise a failing detection use it
// to skip the real backoff.
func SetDetectBackoff(b wait.Backoff) (restore func()) {
	prev := detectBackoff
	detectBackoff = b
	return func() { detectBackoff = prev }
}
