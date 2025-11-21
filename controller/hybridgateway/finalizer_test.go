package hybridgateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	routeconst "github.com/kong/kong-operator/controller/hybridgateway/const/route"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestFinalizerFunctionality(t *testing.T) {
	// Test basic finalizer operations
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-route",
			Namespace: "default",
		},
		Spec: gwtypes.HTTPRouteSpec{},
	}

	// Test adding finalizer
	assert.False(t, controllerutil.ContainsFinalizer(route, routeconst.RouteFinalizer))
	controllerutil.AddFinalizer(route, routeconst.RouteFinalizer)
	assert.True(t, controllerutil.ContainsFinalizer(route, routeconst.RouteFinalizer))
	assert.Contains(t, route.Finalizers, routeconst.RouteFinalizer)

	// Test removing finalizer
	controllerutil.RemoveFinalizer(route, routeconst.RouteFinalizer)
	assert.False(t, controllerutil.ContainsFinalizer(route, routeconst.RouteFinalizer))
	assert.NotContains(t, route.Finalizers, routeconst.RouteFinalizer)
}

func TestFinalizerConstant(t *testing.T) {
	// Verify the finalizer constant is properly defined
	assert.Equal(t, "gateway-operator.konghq.com/route-cleanup", routeconst.RouteFinalizer)
	assert.NotEmpty(t, routeconst.RouteFinalizer)
}
