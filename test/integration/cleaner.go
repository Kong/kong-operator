package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// addToCleanup has to be used in tests which work with ControlPlane v2alpha1.
// The reason for using this instead of cleaner's methods is that currently,
// the deletion is being called in
// https://github.com/Kong/kubernetes-testing-framework/blob/2345c4441ce9ddfef2ded763e9148678b25cd0d1/pkg/clusters/cleanup.go#L214-L217
// where GVK information is cleared from the object, related issues:
// - https://github.com/kubernetes/kubernetes/issues/3030,
// - https://github.com/kubernetes/kubernetes/issues/80609,
//
// and this causes the deletion to somehow delete the object as if it was from
// the v1beta1 API group which clears v2alpha1 fields
// (because the v1beta1 API group does not have them).
// Since v2alpha1 ControlPlane have validation rules and objects of this type
// get finalizer assigned to clear the in process instance, objects at this
// point are blocked from being updated or deleted until the CEL validation
// rules are satisfied.
//
// Possible ways to fix this:
//   - hardcode the GVK information in the cleaner code linked above but
//     that's not a scalable an generic solution that would work for all problematic
//     types
//   - use the controller-runtime client to delete the object instead of the dynamic
//     client used in the cleaner code linked above.
//
// Until that's fixed (probably by using the second proposed solution), we
// use this function to add the object to the cleanup.
func addToCleanup(
	t *testing.T,
	cl client.Client,
	obj client.Object,
) {
	t.Helper()

	t.Cleanup(func() {
		// Don't use test's context as that might be cancelled already.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:usetesting
		defer cancel()
		require.NoError(t, cl.Delete(ctx, obj))
	})
}
