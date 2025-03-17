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

// eventuallyAssertSDKExpectations waits for the SDK to have all its expectations met.
// This is useful to ensure that all expected calls to the SDK have been made up
// to a certain point in the test.
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

	require.EventuallyWithT(t,
		func(c *assert.CollectT) {
			assert.True(c, sdk.AssertExpectations(t))
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
