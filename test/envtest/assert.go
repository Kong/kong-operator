package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func assertCollectObjectExistsAndHasKonnectID(
	t *testing.T,
	ctx context.Context,
	clientNamespaced client.Client,
	obj interface {
		client.Object
		GetKonnectID() string
		GetTypeName() string
	},
	konnectID string,
) func(c *assert.CollectT) {
	t.Helper()

	t.Logf("wait for the %s %s to get Konnect ID (%s) assigned",
		obj.GetTypeName(), client.ObjectKeyFromObject(obj), konnectID,
	)

	return func(c *assert.CollectT) {
		nn := client.ObjectKeyFromObject(obj)
		require.NoError(c, clientNamespaced.Get(ctx, nn, obj))
		assert.Equal(c, konnectID, obj.GetKonnectID())
	}
}

// MockTestingTAdapter allows to use assert.CollectT where a mock.TestingT is
// required. This is useful when in functions like AssertExpectations.
//
// Ref: https://pkg.go.dev/github.com/stretchr/testify/mock#TestingT
type MockTestingTAdapter struct {
	*assert.CollectT
	t *testing.T
}

// Logf is a method that allows to log messages in the context of a test.
func (a MockTestingTAdapter) Logf(format string, args ...any) {
	a.t.Logf(format, args...)
}

// eventuallyAssertSDKExpectations waits for the SDK to have all its expectations met.
// This is useful to ensure that all expected calls to the SDK have been made up
// to a certain point in the test.
//
// Thanks to using the require.EventuallyWithT and assert.CollectT, the test is
// not marked as failed on first assertion failure.
//
// This function uses an adapter for assert.CollectT to allow it to be used with
// AssertExpectations, which requires a mock.TestingT interface.
func eventuallyAssertSDKExpectations(
	t *testing.T,
	sdk interface {
		AssertExpectations(mock.TestingT) bool
	},
	waitTime time.Duration, //nolint:unparam
	tickTime time.Duration, //nolint:unparam
) {
	t.Helper()
	t.Logf("Checking %T SDK expectations", sdk)

	// TODO: After bumping testify to 1.11.0 in https://github.com/Kong/kong-operator/pull/2126
	// The behavior of eventual checks changes to make the first assertion immediately rather
	// than waiting the tick interval.
	// This broke tests. Select below makes sure that we retain the old behavior here.
	select {
	case <-t.Context().Done():
	case <-time.After(tickTime):
	}

	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			// We're not passing t to AssertExpectations to prevent failing the test
			// on the first assertion failure.
			// MockTestingTAdapter allows us to pass the assert.CollectT to the
			// AssertExpectations method, which requires a mock.TestingT interface.
			assert.True(c, sdk.AssertExpectations(MockTestingTAdapter{c, t}))
		},
		waitTime, tickTime,
	)
}

// assertsAnd returns a function that performs a logical AND of the given asserts.
func assertsAnd[
	T client.Object,
](
	asserts ...func(T) bool,
) func(objToMatch T) bool {
	return func(objToMatch T) bool {
		for _, f := range asserts {
			if !f(objToMatch) {
				return false
			}
		}

		return true
	}
}

func assertNot[
	T client.Object,
](
	assert func(T) bool,
) func(objToMatch T) bool {
	return func(objToMatch T) bool {
		return !assert(objToMatch)
	}
}
