package configuration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// TestEventGatewayStoredKonnectIDParentRef covers an object stored with a
// konnectID parent reference before the CRD rejected one: it can still be
// updated, e.g. its finalizer removed, as long as it keeps that reference.
//
// The API server can't store such an object with the installed CRD, so the
// test installs a copy of the EventGatewayVirtualClusterProducePolicy CRD in a
// group of its own, first without the rule, creates the object, then adds the
// rule. The copy doesn't change the CRD the other tests use.
func TestEventGatewayStoredKonnectIDParentRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	const (
		group        = "konnectid-ratcheting.test.konghq.com"
		mustUseNsRef = "spec.eventGatewayVirtualClusterRef must use namespacedRef"
		konnectID    = "8ae65120-cdec-4310-84c1-4b19caf67967"
	)

	crd := installCRDCopy(t, cfg, "configuration.konghq.com_eventgatewayvirtualclusterproducepolicies.yaml", group,
		func(r apiextensionsv1.ValidationRule) bool { return r.Message == mustUseNsRef })
	cl := crd.Client

	policy := func(parentRef map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": crd.APIVersion,
			"kind":       crd.Kind,
			"metadata": map[string]any{
				"namespace":    ns.Name,
				"generateName": "stored-konnectid-",
			},
			"spec": map[string]any{
				"eventGatewayVirtualClusterRef": parentRef,
				"apiSpec": map[string]any{
					"type": "modifyHeaders",
					"modifyHeaders": map[string]any{
						"name": "add-header",
						"config": map[string]any{
							"actions": []any{map[string]any{
								"op":  "set",
								"set": map[string]any{"key": "x-header", "value": "value"},
							}},
						},
					},
				},
			},
		}}
	}
	konnectIDRef := func(id string) map[string]any {
		return map[string]any{"type": "konnectID", "konnectID": id}
	}

	// The CRD may take a moment to be served once established.
	stored := policy(konnectIDRef(konnectID))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, cl.Create(ctx, stored))
	}, time.Minute, 100*time.Millisecond)
	// The object is stored without a status: the rule keeping the parent
	// reference of a Programmed entity must still evaluate on its updates.
	_, hasStatus, err := unstructured.NestedMap(stored.Object, "status")
	require.NoError(t, err)
	require.False(t, hasStatus)

	// Add the rule, and wait for the API server to enforce it.
	crd.AddRules(t)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		probe := policy(konnectIDRef(konnectID))
		err := cl.Create(ctx, probe)
		if err == nil {
			assert.NoError(c, cl.Delete(ctx, probe))
			assert.Fail(c, "a konnectID parent reference is still accepted")
			return
		}
		assert.True(c, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
		assert.ErrorContains(c, err, mustUseNsRef)
	}, time.Minute, 100*time.Millisecond)

	// update applies change to the latest stored object.
	update := func(change func(*unstructured.Unstructured)) error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(stored.GroupVersionKind())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(stored), obj); err != nil {
			return err
		}
		change(obj)
		return cl.Update(ctx, obj)
	}

	t.Run("a finalizer can be added and removed", func(t *testing.T) {
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			obj.SetFinalizers([]string{"test.konghq.com/finalizer"})
		}))
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			obj.SetFinalizers(nil)
		}))
	})
	t.Run("the rest of the spec can change", func(t *testing.T) {
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedField(obj.Object, "renamed", "spec", "apiSpec", "modifyHeaders", "name"))
		}))
	})
	t.Run("the konnectID can't change", func(t *testing.T) {
		err := update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedMap(obj.Object,
				konnectIDRef("00000000-0000-4000-8000-000000000000"), "spec", "eventGatewayVirtualClusterRef"))
		})
		require.True(t, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
		require.ErrorContains(t, err, mustUseNsRef)
	})
	t.Run("the reference can be moved to namespacedRef", func(t *testing.T) {
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedMap(obj.Object, map[string]any{
				"type":          "namespacedRef",
				"namespacedRef": map[string]any{"name": "virtual-cluster"},
			}, "spec", "eventGatewayVirtualClusterRef"))
		}))
	})
	t.Run("once moved, it can't go back to a konnectID", func(t *testing.T) {
		err := update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedMap(obj.Object, konnectIDRef(konnectID), "spec", "eventGatewayVirtualClusterRef"))
		})
		require.True(t, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
		require.ErrorContains(t, err, mustUseNsRef)
	})
}
