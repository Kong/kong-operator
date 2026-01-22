package gatewayapi

import internal "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"

type (
	AllowedRoutes          = internal.AllowedRoutes
	BackendObjectReference = internal.BackendObjectReference
	BackendRef             = internal.BackendRef
	CommonRouteSpec        = internal.CommonRouteSpec
	Gateway                = internal.Gateway
	GatewayClass           = internal.GatewayClass
	GatewayClassSpec       = internal.GatewayClassSpec
	GatewayController      = internal.GatewayController
	GatewayList            = internal.GatewayList
	GatewaySpec            = internal.GatewaySpec
	GatewayStatus          = internal.GatewayStatus
	GatewayStatusAddress   = internal.GatewayStatusAddress
	Group                  = internal.Group
	Kind                   = internal.Kind
	HTTPBackendRef         = internal.HTTPBackendRef
	HTTPPathMatch          = internal.HTTPPathMatch
	HTTPRoute              = internal.HTTPRoute
	HTTPRouteMatch         = internal.HTTPRouteMatch
	HTTPRouteRule          = internal.HTTPRouteRule
	HTTPRouteSpec          = internal.HTTPRouteSpec
	HTTPRouteStatus        = internal.HTTPRouteStatus
	Listener               = internal.Listener
	ListenerStatus         = internal.ListenerStatus
	Namespace              = internal.Namespace
	ObjectName             = internal.ObjectName
	ParentReference        = internal.ParentReference
	PortNumber             = internal.PortNumber
	PolicyAncestorStatus   = internal.PolicyAncestorStatus
	ReferenceGrant         = internal.ReferenceGrant
	ReferenceGrantFrom     = internal.ReferenceGrantFrom
	ReferenceGrantSpec     = internal.ReferenceGrantSpec
	ReferenceGrantTo       = internal.ReferenceGrantTo
	RouteGroupKind         = internal.RouteGroupKind
	RouteNamespaces        = internal.RouteNamespaces
	RouteParentStatus      = internal.RouteParentStatus
	RouteStatus            = internal.RouteStatus
	SectionName            = internal.SectionName

	GRPCRoute     = internal.GRPCRoute
	GRPCRouteSpec = internal.GRPCRouteSpec
	TCPRoute      = internal.TCPRoute
	TCPRouteSpec  = internal.TCPRouteSpec
	TCPRouteRule  = internal.TCPRouteRule
	TLSRoute      = internal.TLSRoute
	TLSRouteSpec  = internal.TLSRouteSpec
	TLSRouteRule  = internal.TLSRouteRule
	UDPRoute      = internal.UDPRoute
	UDPRouteSpec  = internal.UDPRouteSpec
	UDPRouteRule  = internal.UDPRouteRule
)

const (
	GatewayConditionAccepted   = internal.GatewayConditionAccepted
	GatewayConditionProgrammed = internal.GatewayConditionProgrammed
	HTTPProtocolType           = internal.HTTPProtocolType
	IPAddressType              = internal.IPAddressType
	NamespacesFromAll          = internal.NamespacesFromAll
	PathMatchPathPrefix        = internal.PathMatchPathPrefix
	RouteConditionAccepted     = internal.RouteConditionAccepted
	V1Group                    = internal.V1Group
)

var V1GroupVersion = internal.V1GroupVersion
