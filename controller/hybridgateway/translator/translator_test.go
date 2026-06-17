package translator

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

var testKongPluginBindingGVK = schema.GroupVersionKind{
	Group:   "configuration.konghq.com",
	Version: "v1alpha1",
	Kind:    "KongPluginBinding",
}

func newTestKongObject(namespace, name string, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(testKongPluginBindingGVK)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func TestVerifyAndUpdate(t *testing.T) {
	ctx := context.Background()
	route := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{Kind: "HTTPRoute", APIVersion: "gateway.networking.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "route-a",
		},
	}
	routeKey := client.ObjectKeyFromObject(route).String()

	tests := []struct {
		name           string
		exclusiveRoute bool
		existing       *unstructured.Unstructured
		wantErr        bool
		wantRoutes     string
	}{
		{
			name:           "exclusive object with missing annotation is claimed by current route",
			exclusiveRoute: true,
			existing:       newTestKongObject("ns", "binding", nil),
			wantRoutes:     routeKey,
		},
		{
			name:           "exclusive object with empty annotation is claimed by current route",
			exclusiveRoute: true,
			existing: newTestKongObject("ns", "binding", map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "",
			}),
			wantRoutes: routeKey,
		},
		{
			name:           "exclusive object with another route is rejected",
			exclusiveRoute: true,
			existing: newTestKongObject("ns", "binding", map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/other-route",
			}),
			wantErr: true,
		},
		{
			name:           "shared object preserves existing routes and appends current route",
			exclusiveRoute: false,
			existing: newTestKongObject("ns", "binding", map[string]string{
				consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/other-route",
			}),
			wantRoutes: "ns/other-route," + routeKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(runtime.NewScheme()).
				WithObjects(tt.existing).
				Build()
			desired := newTestKongObject(tt.existing.GetNamespace(), tt.existing.GetName(), nil)

			exists, err := VerifyAndUpdate(ctx, logr.Discard(), cl, desired, route, tt.exclusiveRoute)
			require.True(t, exists)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRoutes, desired.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])
		})
	}
}
