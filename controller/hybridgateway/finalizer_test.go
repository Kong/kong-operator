package hybridgateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	finalizerconst "github.com/kong/kong-operator/controller/hybridgateway/const/finalizers"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestFinalizerFunctionality(t *testing.T) {
	t.Run("HTTPRoute finalizer operations", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
			Spec: gwtypes.HTTPRouteSpec{},
		}

		finalizerName := finalizerconst.GetFinalizerForType(*route)

		// Test adding finalizer
		assert.False(t, controllerutil.ContainsFinalizer(route, finalizerName))
		controllerutil.AddFinalizer(route, finalizerName)
		assert.True(t, controllerutil.ContainsFinalizer(route, finalizerName))
		assert.Contains(t, route.Finalizers, finalizerName)

		// Test removing finalizer
		controllerutil.RemoveFinalizer(route, finalizerName)
		assert.False(t, controllerutil.ContainsFinalizer(route, finalizerName))
		assert.NotContains(t, route.Finalizers, finalizerName)
	})

	t.Run("Gateway finalizer operations", func(t *testing.T) {
		gateway := &gwtypes.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-gateway",
				Namespace: "default",
			},
			Spec: gwtypes.GatewaySpec{},
		}

		finalizerName := finalizerconst.GetFinalizerForType(*gateway)

		// Test adding finalizer
		assert.False(t, controllerutil.ContainsFinalizer(gateway, finalizerName))
		controllerutil.AddFinalizer(gateway, finalizerName)
		assert.True(t, controllerutil.ContainsFinalizer(gateway, finalizerName))
		assert.Contains(t, gateway.Finalizers, finalizerName)

		// Test removing finalizer
		controllerutil.RemoveFinalizer(gateway, finalizerName)
		assert.False(t, controllerutil.ContainsFinalizer(gateway, finalizerName))
		assert.NotContains(t, gateway.Finalizers, finalizerName)
	})
}

func TestFinalizerConstant(t *testing.T) {
	t.Run("HTTPRoute finalizer is properly defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/httproute-cleanup", finalizerconst.HTTPRouteFinalizer)
		assert.NotEmpty(t, finalizerconst.HTTPRouteFinalizer)
	})

	t.Run("Gateway finalizer is properly defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/gateway-cleanup", finalizerconst.GatewayFinalizer)
		assert.NotEmpty(t, finalizerconst.GatewayFinalizer)
	})

	t.Run("different resource types have different finalizers", func(t *testing.T) {
		assert.NotEqual(t, finalizerconst.HTTPRouteFinalizer, finalizerconst.GatewayFinalizer)
	})
}
