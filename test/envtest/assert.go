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
		if !assert.NoError(c, clientNamespaced.Get(ctx, nn, obj)) {
			return
		}
		assert.Equal(c, konnectID, obj.GetKonnectID())
	}
}
