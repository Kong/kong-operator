package types

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type (
	AllowedRoutes          = gatewayv1.AllowedRoutes
	BackendObjectReference = gatewayv1.BackendObjectReference
	BackendRef             = gatewayv1.BackendRef
	CommonRouteSpec        = gatewayv1.CommonRouteSpec
	GRPCRoute              = gatewayv1.GRPCRoute
	Gateway                = gatewayv1.Gateway
	GatewayClass           = gatewayv1.GatewayClass
	GatewayClassSpec       = gatewayv1.GatewayClassSpec
	GatewayController      = gatewayv1.GatewayController
	GatewayList            = gatewayv1.GatewayList
	GatewaySpec            = gatewayv1.GatewaySpec
	GatewayStatusAddress   = gatewayv1.GatewayStatusAddress
	Group                  = gatewayv1.Group
	HTTPBackendRef         = gatewayv1.HTTPBackendRef
	HTTPRoute              = gatewayv1.HTTPRoute
	HTTPRouteFilter        = gatewayv1.HTTPRouteFilter
	HTTPRouteList          = gatewayv1.HTTPRouteList
	HTTPRouteMatch         = gatewayv1.HTTPRouteMatch
	HTTPRouteRule          = gatewayv1.HTTPRouteRule
	HTTPRouteSpec          = gatewayv1.HTTPRouteSpec
	Kind                   = gatewayv1.Kind
	Listener               = gatewayv1.Listener
	Namespace              = gatewayv1.Namespace
	ObjectName             = gatewayv1.ObjectName
	ParametersReference    = gatewayv1.ParametersReference
	ParentReference        = gatewayv1.ParentReference
	PortNumber             = gatewayv1.PortNumber
	RouteGroupKind         = gatewayv1.RouteGroupKind
	RouteNamespaces        = gatewayv1.RouteNamespaces
	RouteParentStatus      = gatewayv1.RouteParentStatus
	SectionName            = gatewayv1.SectionName
)

var GroupVersion = gatewayv1.GroupVersion

const (
	HTTPProtocolType = gatewayv1.HTTPProtocolType

	NamespacesFromAll      = gatewayv1.NamespacesFromAll
	NamespacesFromSame     = gatewayv1.NamespacesFromSame
	NamespacesFromSelector = gatewayv1.NamespacesFromSelector
)
