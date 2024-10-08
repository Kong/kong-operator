package envtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func assertCollectObjectExistsAndHasKonnectID(
	t *testing.T,
	ctx context.Context,
	clientNamespaced client.Client,
	obj interface {
		client.Object
		GetKonnectID() string
	},
	konnectID string,
) func(c *assert.CollectT) {
	t.Helper()

	return func(c *assert.CollectT) {
		nn := client.ObjectKeyFromObject(obj)
		if !assert.NoError(c, clientNamespaced.Get(ctx, nn, obj)) {
			return
		}
		assert.Equal(t, konnectID, obj.GetKonnectID())
	}
}
