package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// V1GatewayTypeMeta is the TypeMeta of the Gateway API v1 Gateway.
var V1GatewayTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "Gateway",
}

// V1GatewayClassTypeMeta is the TypeMeta of the Gateway API v1 GatewayClass.
var V1GatewayClassTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "GatewayClass",
}

// V1HTTPRouteTypeMeta is the TypeMeta of the Gateway API v1 HTTPRoute.
var V1HTTPRouteTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "HTTPRoute",
}

// ReferenceGrantTypeMeta is the TypeMeta of the Gateway API v1beta1 ReferenceGrant.
var ReferenceGrantTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1beta1.GroupVersion.String(),
	Kind:       "ReferenceGrant",
}

// GRPCRouteTypeMeta is the TypeMeta of the Gateway API v1 GRPCRoute.
var GRPCRouteTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "GRPCRoute",
}

// TCPRouteTypeMeta is the TypeMeta of the Gateway API v1 TCPRoute.
var TCPRouteTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "TCPRoute",
}

// TLSRouteTypeMeta is the TypeMeta of the Gateway API v1 TLSRoute.
var TLSRouteTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "TLSRoute",
}

// UDPRouteTypeMeta is the TypeMeta of the Gateway API v1 UDPRoute.
var UDPRouteTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1.GroupVersion.String(),
	Kind:       "UDPRoute",
}

var (
	// V1GatewayGVResource is the GroupVersionResource of the Gateway API v1 Gateway.
	V1GatewayGVResource = metav1.GroupVersionResource{
		Group:    gatewayv1.GroupVersion.Group,
		Version:  gatewayv1.GroupVersion.Version,
		Resource: "gateways",
	}
	// V1HTTPRouteGVResource is the GroupVersionResource of the Gateway API v1 HTTPRoute.
	V1HTTPRouteGVResource = metav1.GroupVersionResource{
		Group:    gatewayv1.GroupVersion.Group,
		Version:  gatewayv1.GroupVersion.Version,
		Resource: "httproutes",
	}
	// V1beta1GatewayGVResource is the GroupVersionResource of the Gateway API v1beta1 Gateway.
	V1beta1GatewayGVResource = metav1.GroupVersionResource{
		Group:    gatewayv1beta1.GroupVersion.Group,
		Version:  gatewayv1beta1.GroupVersion.Version,
		Resource: "gateways",
	}
	// V1beta1HTTPRouteGVResource is the GroupVersionResource of the Gateway API v1beta1 HTTPRoute.
	V1beta1HTTPRouteGVResource = metav1.GroupVersionResource{
		Group:    gatewayv1beta1.GroupVersion.Group,
		Version:  gatewayv1beta1.GroupVersion.Version,
		Resource: "httproutes",
	}
)
