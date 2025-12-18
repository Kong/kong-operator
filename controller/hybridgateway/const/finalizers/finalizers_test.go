package finalizers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestFinalizerConstants(t *testing.T) {
	t.Run("HTTPRouteFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/httproute-cleanup", HTTPRouteFinalizer)
		assert.NotEmpty(t, HTTPRouteFinalizer)
	})

	t.Run("GatewayFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/gateway-cleanup", GatewayFinalizer)
		assert.NotEmpty(t, GatewayFinalizer)
	})

	t.Run("DefaultFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/resource-cleanup", DefaultFinalizer)
		assert.NotEmpty(t, DefaultFinalizer)
	})

	t.Run("all finalizers are unique", func(t *testing.T) {
		finalizers := []string{HTTPRouteFinalizer, GatewayFinalizer, DefaultFinalizer}
		seen := make(map[string]bool)
		for _, f := range finalizers {
			assert.False(t, seen[f], "Duplicate finalizer found: %s", f)
			seen[f] = true
		}
	})

	t.Run("all finalizers follow naming convention", func(t *testing.T) {
		finalizers := []string{HTTPRouteFinalizer, GatewayFinalizer, DefaultFinalizer}
		for _, f := range finalizers {
			assert.Contains(t, f, "gateway-operator.konghq.com/", "Finalizer should contain domain prefix: %s", f)
			assert.Contains(t, f, "-cleanup", "Finalizer should contain -cleanup suffix: %s", f)
		}
	})
}

func TestGetFinalizerForType(t *testing.T) {
	t.Run("HTTPRoute returns HTTPRouteFinalizer", func(t *testing.T) {
		route := gwtypes.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-route",
				Namespace: "default",
			},
		}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HTTPRouteFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/httproute-cleanup", finalizer)
	})

	t.Run("Gateway returns GatewayFinalizer", func(t *testing.T) {
		gateway := gwtypes.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-gateway",
				Namespace: "default",
			},
		}
		finalizer := GetFinalizerForType(gateway)
		assert.Equal(t, GatewayFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/gateway-cleanup", finalizer)
	})

	t.Run("works with zero-value HTTPRoute", func(t *testing.T) {
		route := gwtypes.HTTPRoute{}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HTTPRouteFinalizer, finalizer)
	})

	t.Run("works with zero-value Gateway", func(t *testing.T) {
		gateway := gwtypes.Gateway{}
		finalizer := GetFinalizerForType(gateway)
		assert.Equal(t, GatewayFinalizer, finalizer)
	})

	t.Run("HTTPRoute and Gateway have different finalizers", func(t *testing.T) {
		route := gwtypes.HTTPRoute{}
		gateway := gwtypes.Gateway{}

		routeFinalizer := GetFinalizerForType(route)
		gatewayFinalizer := GetFinalizerForType(gateway)

		assert.NotEqual(t, routeFinalizer, gatewayFinalizer)
	})
}

func TestGenericTypeConstraints(t *testing.T) {
	t.Run("RootObject constraint matches converter.RootObject", func(t *testing.T) {
		// This test verifies that we're using the same RootObject constraint
		// from the converter package
		testGenericFinalizerFunction[gwtypes.HTTPRoute](t)
		testGenericFinalizerFunction[gwtypes.Gateway](t)
	})
}

func testGenericFinalizerFunction[T converter.RootObject](t *testing.T) {
	var obj T
	finalizer := GetFinalizerForType(obj)
	assert.NotEmpty(t, finalizer)
	assert.Contains(t, finalizer, "gateway-operator.konghq.com/")
}
