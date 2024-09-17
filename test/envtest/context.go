package envtest

import (
	"context"
	"testing"
	"time"
)

// Context creates a context for tests with deadline if it was provided.
// It also cancels the context on test cleanup.
func Context(t *testing.T, ctx context.Context) (context.Context, context.CancelFunc) {
	t.Helper()

	if tt, ok := t.Deadline(); ok {
		t.Logf("Test %s deadline set to %s (%s)", t.Name(), tt.Format(time.RFC3339), time.Until(tt))
		return context.WithDeadline(ctx, tt)
	}
	return context.WithCancel(ctx)
}
