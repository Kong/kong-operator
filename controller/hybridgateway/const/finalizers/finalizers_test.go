package finalizers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/converter"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestFinalizerConstants(t *testing.T) {
	t.Run("HTTPRouteFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-httproute-cleanup", HybridHTTPRouteFinalizer)
		assert.NotEmpty(t, HybridHTTPRouteFinalizer)
	})

	t.Run("GRPCRouteFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-grpcroute-cleanup", HybridGRPCRouteFinalizer)
		assert.NotEmpty(t, HybridGRPCRouteFinalizer)
	})

	t.Run("GatewayFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-gateway-cleanup", HybridGatewayFinalizer)
		assert.NotEmpty(t, HybridGatewayFinalizer)
	})

	t.Run("TCPRouteFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-tcproute-cleanup", HybridTCPRouteFinalizer)
		assert.NotEmpty(t, HybridTCPRouteFinalizer)
	})

	t.Run("UDPRouteFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-udproute-cleanup", HybridUDPRouteFinalizer)
		assert.NotEmpty(t, HybridUDPRouteFinalizer)
	})

	t.Run("DefaultFinalizer is defined", func(t *testing.T) {
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-resource-cleanup", HybridDefaultFinalizer)
		assert.NotEmpty(t, HybridDefaultFinalizer)
	})

	t.Run("all finalizers are unique", func(t *testing.T) {
		finalizers := []string{HybridHTTPRouteFinalizer, HybridGRPCRouteFinalizer, HybridTLSRouteFinalizer, HybridTCPRouteFinalizer, HybridUDPRouteFinalizer, HybridGatewayFinalizer, HybridDefaultFinalizer}
		seen := make(map[string]bool)
		for _, f := range finalizers {
			assert.False(t, seen[f], "Duplicate finalizer found: %s", f)
			seen[f] = true
		}
	})

	t.Run("all finalizers follow naming convention", func(t *testing.T) {
		finalizers := []string{HybridHTTPRouteFinalizer, HybridGRPCRouteFinalizer, HybridTLSRouteFinalizer, HybridTCPRouteFinalizer, HybridUDPRouteFinalizer, HybridGatewayFinalizer, HybridDefaultFinalizer}
		for _, f := range finalizers {
			assert.Contains(t, f, "gateway-operator.konghq.com/", "Finalizer should contain domain prefix: %s", f)
			assert.Contains(t, f, "-cleanup", "Finalizer should contain -cleanup suffix: %s", f)
		}
	})
}

func TestGetFinalizerForType(t *testing.T) {
	t.Run("HTTPRoute returns HTTPRouteFinalizer", func(t *testing.T) {
		route := gwtypes.HTTPRoute{
			Name:      "test-route",
			Namespace: "default",
		}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridHTTPRouteFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-httproute-cleanup", finalizer)
	})

	t.Run("GRPCRoute returns GRPCRouteFinalizer", func(t *testing.T) {
		route := gwtypes.GRPCRoute{
			Name:      "test-route",
			Namespace: "default",
		}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridGRPCRouteFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-grpcroute-cleanup", finalizer)
	})

	t.Run("Gateway returns GatewayFinalizer", func(t *testing.T) {
		gateway := gwtypes.Gateway{
			Name:      "test-gateway",
			Namespace: "default",
		}
		finalizer := GetFinalizerForType(gateway)
		assert.Equal(t, HybridGatewayFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-gateway-cleanup", finalizer)
	})

	t.Run("TCPRoute returns TCPRouteFinalizer", func(t *testing.T) {
		route := gwtypes.TCPRoute{
			Name:      "test-route",
			Namespace: "default",
		}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridTCPRouteFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-tcproute-cleanup", finalizer)
	})

	t.Run("works with zero-value HTTPRoute", func(t *testing.T) {
		route := gwtypes.HTTPRoute{}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridHTTPRouteFinalizer, finalizer)
	})

	t.Run("works with zero-value GRPCRoute", func(t *testing.T) {
		route := gwtypes.GRPCRoute{}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridGRPCRouteFinalizer, finalizer)
	})

	t.Run("works with zero-value Gateway", func(t *testing.T) {
		gateway := gwtypes.Gateway{}
		finalizer := GetFinalizerForType(gateway)
		assert.Equal(t, HybridGatewayFinalizer, finalizer)
	})

	t.Run("works with zero-value TCPRoute", func(t *testing.T) {
		route := gwtypes.TCPRoute{}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridTCPRouteFinalizer, finalizer)
	})

	t.Run("UDPRoute returns UDPRouteFinalizer", func(t *testing.T) {
		route := gwtypes.UDPRoute{
			Name:      "test-route",
			Namespace: "default",
		}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridUDPRouteFinalizer, finalizer)
		assert.Equal(t, "gateway-operator.konghq.com/hybrid-udproute-cleanup", finalizer)
	})

	t.Run("works with zero-value UDPRoute", func(t *testing.T) {
		route := gwtypes.UDPRoute{}
		finalizer := GetFinalizerForType(route)
		assert.Equal(t, HybridUDPRouteFinalizer, finalizer)
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
		testGenericFinalizerFunction[gwtypes.GRPCRoute](t)
		testGenericFinalizerFunction[gwtypes.TCPRoute](t)
		testGenericFinalizerFunction[gwtypes.UDPRoute](t)
		testGenericFinalizerFunction[gwtypes.Gateway](t)
	})
}

func testGenericFinalizerFunction[T converter.RootObject](t *testing.T) {
	var obj T
	finalizer := GetFinalizerForType(obj)
	assert.NotEmpty(t, finalizer)
	assert.Contains(t, finalizer, "gateway-operator.konghq.com/")
}
