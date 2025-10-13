package types

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type (
	AllowedRoutes          = gatewayv1.AllowedRoutes
	BackendObjectReference = gatewayv1.BackendObjectReference
	BackendRef             = gatewayv1.BackendRef
	CommonRouteSpec        = gatewayv1.CommonRouteSpec
	Gateway                = gatewayv1.Gateway
	GatewayClass           = gatewayv1.GatewayClass
	GatewayClassSpec       = gatewayv1.GatewayClassSpec
	GatewayController      = gatewayv1.GatewayController
	GatewayList            = gatewayv1.GatewayList
	GatewaySpec            = gatewayv1.GatewaySpec
	GatewayStatusAddress   = gatewayv1.GatewayStatusAddress
	GRPCRoute              = gatewayv1.GRPCRoute
	Group                  = gatewayv1.Group
	GroupVersionKind       = gatewayv1.Group
	Hostname               = gatewayv1.Hostname
	HTTPBackendRef         = gatewayv1.HTTPBackendRef
	HTTPRoute              = gatewayv1.HTTPRoute
	HTTPRouteFilter        = gatewayv1.HTTPRouteFilter
	HTTPRouteList          = gatewayv1.HTTPRouteList
	HTTPRouteMatch         = gatewayv1.HTTPRouteMatch
	HTTPRouteRule          = gatewayv1.HTTPRouteRule
	HTTPRouteSpec          = gatewayv1.HTTPRouteSpec
	HTTPRouteStatus        = gatewayv1.HTTPRouteStatus
	Kind                   = gatewayv1.Kind
	Listener               = gatewayv1.Listener
	Namespace              = gatewayv1.Namespace
	ObjectName             = gatewayv1.ObjectName
	ParametersReference    = gatewayv1.ParametersReference
	ParentReference        = gatewayv1.ParentReference
	PortNumber             = gatewayv1.PortNumber
	ReferenceGrantList     = gatewayv1beta1.ReferenceGrantList
	ReferenceGrantSpec     = gatewayv1beta1.ReferenceGrantSpec
	RouteGroupKind         = gatewayv1.RouteGroupKind
	RouteNamespaces        = gatewayv1.RouteNamespaces
	RouteParentStatus      = gatewayv1.RouteParentStatus
	SectionName            = gatewayv1.SectionName
)

var GroupVersion = gatewayv1.GroupVersion

const (
	GroupName                             = gatewayv1.GroupName
	HTTPProtocolType                      = gatewayv1.HTTPProtocolType
	HTTPSProtocolType                     = gatewayv1.HTTPSProtocolType
	ListenerConditionProgrammed           = gatewayv1.ListenerConditionProgrammed
	NamespacesFromAll                     = gatewayv1.NamespacesFromAll
	NamespacesFromSame                    = gatewayv1.NamespacesFromSame
	NamespacesFromSelector                = gatewayv1.NamespacesFromSelector
	RouteConditionAccepted                = gatewayv1.RouteConditionAccepted
	RouteConditionResolvedRefs            = gatewayv1.RouteConditionResolvedRefs
	RouteReasonAccepted                   = gatewayv1.RouteReasonAccepted
	RouteReasonBackendNotFound            = gatewayv1.RouteReasonBackendNotFound
	RouteReasonInvalidKind                = gatewayv1.RouteReasonInvalidKind
	RouteReasonNoMatchingListenerHostname = gatewayv1.RouteReasonNoMatchingListenerHostname
	RouteReasonNoMatchingParent           = gatewayv1.RouteReasonNoMatchingParent
	RouteReasonNotAllowedByListeners      = gatewayv1.RouteReasonNotAllowedByListeners
	RouteReasonRefNotPermitted            = gatewayv1.RouteReasonRefNotPermitted
	RouteReasonResolvedRefs               = gatewayv1.RouteReasonResolvedRefs
	TLSModeTerminate                      = gatewayv1.TLSModeTerminate
)
