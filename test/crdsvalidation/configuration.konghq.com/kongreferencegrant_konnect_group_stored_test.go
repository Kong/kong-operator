package configuration_test

import (
	"strings"
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

// TestKongReferenceGrantStoredKonnectGroupTo covers a grant stored with an
// Event Gateway entity under konnect.konghq.com in to, before the CRD
// rejected one: it can still be updated as long as its to list is unchanged.
//
// The API server can't store such a grant with the installed CRD, so the test
// installs a copy of the KongReferenceGrant CRD in a group of its own, first
// without the konnect.konghq.com rule of spec.to, creates the grant, then adds
// the rule. The copy doesn't change the CRD the other tests use.
func TestKongReferenceGrantStoredKonnectGroupTo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get())

	const (
		group    = "kongreferencegrant-ratcheting.test.konghq.com"
		rejected = "Event Gateway entities are in the 'configuration.konghq.com' group"
	)

	crd := installCRDCopyAt(t, cfg, "configuration.konghq.com_kongreferencegrants.yaml", group,
		[]string{"spec", "to", "items"},
		func(r apiextensionsv1.ValidationRule) bool {
			return strings.HasPrefix(r.Rule, ".self.group != 'konnect.konghq.com'")
		})
	cl := crd.Client

	from := func(namespace string) map[string]any {
		return map[string]any{"group": "configuration.konghq.com", "kind": "EventGatewayVirtualCluster", "namespace": namespace}
	}
	to := func(group string) map[string]any {
		return map[string]any{"group": group, "kind": "EventGatewayBackendCluster"}
	}
	grant := func(toGroup string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": crd.APIVersion,
			"kind":       crd.Kind,
			"metadata": map[string]any{
				"namespace":    ns.Name,
				"generateName": "stored-konnect-group-",
			},
			"spec": map[string]any{
				"from": []any{from("other")},
				"to":   []any{to(toGroup)},
			},
		}}
	}

	// The CRD may take a moment to be served once established.
	stored := grant("konnect.konghq.com")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.NoError(c, cl.Create(ctx, stored))
	}, time.Minute, 100*time.Millisecond)

	// Add the rule, and wait for the API server to enforce it.
	crd.AddRules(t)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		probe := grant("konnect.konghq.com")
		err := cl.Create(ctx, probe)
		if err == nil {
			assert.NoError(c, cl.Delete(ctx, probe))
			assert.Fail(c, "an Event Gateway entity under konnect.konghq.com is still accepted")
			return
		}
		assert.True(c, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
		assert.ErrorContains(c, err, rejected)
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
	t.Run("from can change", func(t *testing.T) {
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedSlice(obj.Object, []any{from("other"), from("third")}, "spec", "from"))
		}))
	})
	t.Run("to can't change while it keeps the entry", func(t *testing.T) {
		err := update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedSlice(obj.Object,
				[]any{to("konnect.konghq.com"), to("configuration.konghq.com")}, "spec", "to"))
		})
		require.True(t, apierrors.IsInvalid(err), "expected an admission validation error, got %v", err)
		require.ErrorContains(t, err, rejected)
	})
	t.Run("the entry can be moved to configuration.konghq.com", func(t *testing.T) {
		require.NoError(t, update(func(obj *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedSlice(obj.Object, []any{to("configuration.konghq.com")}, "spec", "to"))
		}))
	})
}
