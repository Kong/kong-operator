package eventually

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers/conditions"
)

// KonnectEntityGetsProgrammed waits until the given Konnect entity gets
// programmed in Konnect.
// Additional assertions can be provided via the asserts parameter.
// When all assertions pass, the latest version of the object is returned.
func KonnectEntityGetsProgrammed[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	obj TEnt,
	asserts ...func(t *assert.CollectT, obj TEnt),
) TEnt {
	t.Helper()

	var (
		e   T
		ent TEnt = &e
		nn       = client.ObjectKeyFromObject(obj)
	)
	if !assert.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, cl.Get(ctx, nn, ent))
		obj = ent

		conditions.KonnectEntityIsProgrammed(t, ent)
		for _, assertFn := range asserts {
			assertFn(t, ent)
		}
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick) {
		t.Fatalf("Timed out waiting for Konnect entity %T %s to be programmed: %+v",
			obj, nn, obj,
		)
		return nil
	}

	return obj
}
