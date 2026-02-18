package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
)

// deleteObjectAndWaitForDeletionFn returns a function that deletes the given object and waits for it to be gone.
// It's designed to be used with t.Cleanup() to ensure the object is properly deleted (it's not stuck with finalizers, etc.).
func deleteObjectAndWaitForDeletionFn(t *testing.T, obj client.Object) func() {
	return func() {
		t.Logf("Deleting %s and waiting for it to be gone",
			client.ObjectKeyFromObject(obj),
		)
		cl := GetClients().MgrClient
		require.NoError(t, cl.Delete(GetCtx(), obj))
		eventually.WaitForObjectToNotExist(t, ctx, cl, obj,
			testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		)
	}
}
