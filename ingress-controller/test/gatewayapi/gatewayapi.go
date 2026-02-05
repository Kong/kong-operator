package gatewayapi

import internal "github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"

type (
	AllowedRoutes                             = internal.AllowedRoutes
	BackendObjectReference                    = internal.BackendObjectReference
	BackendRef                                = internal.BackendRef
	BackendTLSPolicy                          = internal.BackendTLSPolicy
	BackendTLSPolicySpec                      = internal.BackendTLSPolicySpec
	BackendTLSPolicyValidation                = internal.BackendTLSPolicyValidation
	CommonRouteSpec                           = internal.CommonRouteSpec
	Duration                                  = internal.Duration
	Gateway                                   = internal.Gateway
	GatewayClass                              = internal.GatewayClass
	GatewayClassSpec                          = internal.GatewayClassSpec
	GatewayController                         = internal.GatewayController
	GatewayList                               = internal.GatewayList
	GatewaySpec                               = internal.GatewaySpec
	GatewayStatus                             = internal.GatewayStatus
	GatewayStatusAddress                      = internal.GatewayStatusAddress
	Group                                     = internal.Group
	Hostname                                  = internal.Hostname
	Kind                                      = internal.Kind
	AnnotationKey                             = internal.AnnotationKey
	AnnotationValue                           = internal.AnnotationValue
	GatewayTLSConfig                          = internal.GatewayTLSConfig
	HTTPBackendRef                            = internal.HTTPBackendRef
	HTTPHeaderMatch                           = internal.HTTPHeaderMatch
	HTTPPathMatch                             = internal.HTTPPathMatch
	HTTPQueryParamMatch                       = internal.HTTPQueryParamMatch
	HTTPRouteFilter                           = internal.HTTPRouteFilter
	HTTPRoute                                 = internal.HTTPRoute
	HTTPRouteMatch                            = internal.HTTPRouteMatch
	HTTPRouteRule                             = internal.HTTPRouteRule
	HTTPRouteSpec                             = internal.HTTPRouteSpec
	HTTPRouteStatus                           = internal.HTTPRouteStatus
	Listener                                  = internal.Listener
	ListenerStatus                            = internal.ListenerStatus
	Namespace                                 = internal.Namespace
	ObjectName                                = internal.ObjectName
	ParentReference                           = internal.ParentReference
	LocalObjectReference                      = internal.LocalObjectReference
	LocalPolicyTargetReference                = internal.LocalPolicyTargetReference
	LocalPolicyTargetReferenceWithSectionName = internal.LocalPolicyTargetReferenceWithSectionName
	PortNumber                                = internal.PortNumber
	ProtocolType                              = internal.ProtocolType
	PolicyAncestorStatus                      = internal.PolicyAncestorStatus
	ReferenceGrant                            = internal.ReferenceGrant
	ReferenceGrantFrom                        = internal.ReferenceGrantFrom
	ReferenceGrantSpec                        = internal.ReferenceGrantSpec
	ReferenceGrantTo                          = internal.ReferenceGrantTo
	RouteGroupKind                            = internal.RouteGroupKind
	RouteNamespaces                           = internal.RouteNamespaces
	RouteParentStatus                         = internal.RouteParentStatus
	RouteStatus                               = internal.RouteStatus
	SectionName                               = internal.SectionName

	GRPCBackendRef  = internal.GRPCBackendRef
	GRPCHeaderMatch = internal.GRPCHeaderMatch
	GRPCHeaderName  = internal.GRPCHeaderName
	GRPCMethodMatch = internal.GRPCMethodMatch
	GRPCRouteMatch  = internal.GRPCRouteMatch
	GRPCRouteRule   = internal.GRPCRouteRule
	GRPCRoute       = internal.GRPCRoute
	GRPCRouteSpec   = internal.GRPCRouteSpec
	TCPRoute        = internal.TCPRoute
	TCPRouteSpec    = internal.TCPRouteSpec
	TCPRouteRule    = internal.TCPRouteRule
	TLSRoute        = internal.TLSRoute
	TLSRouteSpec    = internal.TLSRouteSpec
	TLSRouteRule    = internal.TLSRouteRule
	UDPRoute        = internal.UDPRoute
	UDPRouteSpec    = internal.UDPRouteSpec
	UDPRouteRule    = internal.UDPRouteRule

	RouteConditionReason    = internal.RouteConditionReason
	ListenerConditionType   = internal.ListenerConditionType
	ListenerConditionReason = internal.ListenerConditionReason
	SecretObjectReference   = internal.SecretObjectReference
)

const (
	GatewayConditionAccepted       = internal.GatewayConditionAccepted
	GatewayConditionProgrammed     = internal.GatewayConditionProgrammed
	GatewayReasonProgrammed        = internal.GatewayReasonProgrammed
	HTTPProtocolType               = internal.HTTPProtocolType
	HTTPSProtocolType              = internal.HTTPSProtocolType
	IPAddressType                  = internal.IPAddressType
	ListenerConditionConflicted    = internal.ListenerConditionConflicted
	ListenerReasonProtocolConflict = internal.ListenerReasonProtocolConflict
	NamespacesFromAll              = internal.NamespacesFromAll
	PathMatchExact                 = internal.PathMatchExact
	PathMatchPathPrefix            = internal.PathMatchPathPrefix
	RouteConditionAccepted         = internal.RouteConditionAccepted
	RouteReasonAccepted            = internal.RouteReasonAccepted
	RouteReasonNoMatchingParent    = internal.RouteReasonNoMatchingParent
	V1Group                        = internal.V1Group
	ListenerConditionResolvedRefs  = internal.ListenerConditionResolvedRefs
	ListenerReasonRefNotPermitted  = internal.ListenerReasonRefNotPermitted
	ListenerConditionProgrammed    = internal.ListenerConditionProgrammed
	ListenerReasonProgrammed       = internal.ListenerReasonProgrammed
	TCPProtocolType                = internal.TCPProtocolType
	UDPProtocolType                = internal.UDPProtocolType
	TLSProtocolType                = internal.TLSProtocolType
	HTTPRouteFilterRequestRedirect = internal.HTTPRouteFilterRequestRedirect
	FullPathHTTPPathModifier       = internal.FullPathHTTPPathModifier
	PrefixMatchHTTPPathModifier    = internal.PrefixMatchHTTPPathModifier
	PathMatchRegularExpression     = internal.PathMatchRegularExpression
	HeaderMatchRegularExpression   = internal.HeaderMatchRegularExpression
	QueryParamMatchExact           = internal.QueryParamMatchExact
	TLSModePassthrough             = internal.TLSModePassthrough
	TLSModeTerminate               = internal.TLSModeTerminate
	TLSVerifyDepthKey              = internal.TLSVerifyDepthKey
)

var V1GroupVersion = internal.V1GroupVersion
