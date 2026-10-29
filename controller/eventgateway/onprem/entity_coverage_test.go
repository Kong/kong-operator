package onprem

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// TestBuildConfigurationListsEveryEntityKind fails when an entity kind that
// can belong to an OnPremEventGateway, generated from the crd-from-oas
// configuration, isn't added to the configuration by the builder: the kind
// would be watched, but its entities never pushed nor reported.
func TestBuildConfigurationListsEveryEntityKind(t *testing.T) {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: "gateway", Namespace: ns}
	gateway.SetUID(types.UID("6a2f1c8e-5b3d-4e7a-9c1f-2d8b4a6e0f13"))
	created := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)

	// The builder lists the children of each parent: one entity of each kind
	// of parent, valid or not, makes it list every kind of child.
	parents := []client.Object{
		fromYAML[configurationv1alpha1.EventGatewayBackendCluster](t, created, `
metadata: {name: bc}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec: {name: bc}
`),
		fromYAML[configurationv1alpha1.EventGatewayVirtualCluster](t, created, `
metadata: {name: vc}
spec:
  eventGatewayBackendClusterRef: {type: namespacedRef, namespacedRef: {name: bc}}
  apiSpec: {name: vc}
`),
		fromYAML[configurationv1alpha1.EventGatewayListener](t, created, `
metadata: {name: listener}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec: {name: listener}
`),
	}

	listed := map[reflect.Type]struct{}{}
	cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			listed[reflect.TypeOf(list)] = struct{}{}
			return c.List(ctx, list, opts...)
		},
	}, append(parents, gateway)...)

	_, err := buildConfiguration(t.Context(), cl, gateway, "", nil)
	require.NoError(t, err)

	for _, entity := range entityTypes() {
		list, err := listOf(scheme.Get(), entity)
		require.NoError(t, err)
		assert.Contains(t, listed, reflect.TypeOf(list),
			"%T can belong to an OnPremEventGateway, but buildConfiguration never adds it", entity)
	}
}

// listOf returns an empty list of the kind of the entity.
func listOf(scheme *runtime.Scheme, entity client.Object) (client.ObjectList, error) {
	gvks, _, err := scheme.ObjectKinds(entity)
	if err != nil {
		return nil, err
	}
	obj, err := scheme.New(gvks[0].GroupVersion().WithKind(gvks[0].Kind + "List"))
	if err != nil {
		return nil, err
	}
	list, ok := obj.(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("%T is not a list", obj)
	}
	return list, nil
}
