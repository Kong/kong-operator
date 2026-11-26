package types

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
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
	GatewayTLSConfig       = gatewayv1.ListenerTLSConfig
	Group                  = gatewayv1.Group
	GroupVersionKind       = gatewayv1.Group
	Hostname               = gatewayv1.Hostname
	HTTPBackendRef         = gatewayv1.HTTPBackendRef
	HTTPHeader             = gatewayv1.HTTPHeader
	HTTPHeaderFilter       = gatewayv1.HTTPHeaderFilter
	HTTPRoute              = gatewayv1.HTTPRoute
	HTTPRouteFilter        = gatewayv1.HTTPRouteFilter
	HTTPRouteFilterType    = gatewayv1.HTTPRouteFilterType
	HTTPRouteList          = gatewayv1.HTTPRouteList
	HTTPRouteMatch         = gatewayv1.HTTPRouteMatch
	HTTPRouteRule          = gatewayv1.HTTPRouteRule
	HTTPRouteSpec          = gatewayv1.HTTPRouteSpec
	HTTPRouteStatus        = gatewayv1.HTTPRouteStatus
	Kind                   = gatewayv1.Kind
	Listener               = gatewayv1.Listener
	LocalObjectReference   = gatewayv1.LocalObjectReference
	Namespace              = gatewayv1.Namespace
	ObjectName             = gatewayv1.ObjectName
	ParametersReference    = gatewayv1.ParametersReference
	ParentReference        = gatewayv1.ParentReference
	PortNumber             = gatewayv1.PortNumber
	ReferenceGrant         = gatewayv1.ReferenceGrant
	ReferenceGrantList     = gatewayv1.ReferenceGrantList
	ReferenceGrantFrom     = gatewayv1.ReferenceGrantFrom
	ReferenceGrantTo       = gatewayv1.ReferenceGrantTo
	ReferenceGrantSpec     = gatewayv1.ReferenceGrantSpec
	RouteGroupKind         = gatewayv1.RouteGroupKind
	RouteNamespaces        = gatewayv1.RouteNamespaces
	RouteParentStatus      = gatewayv1.RouteParentStatus
	SectionName            = gatewayv1.SectionName
	TLSRoute               = gatewayv1.TLSRoute
	TLSRouteList           = gatewayv1.TLSRouteList
	TLSRouteSpec           = gatewayv1.TLSRouteSpec
	TLSRouteStatus         = gatewayv1.TLSRouteStatus
	TLSRouteRule           = gatewayv1.TLSRouteRule
	TCPRoute               = gatewayv1.TCPRoute
	TCPRouteList           = gatewayv1.TCPRouteList
	TCPRouteSpec           = gatewayv1.TCPRouteSpec
	TCPRouteStatus         = gatewayv1.TCPRouteStatus
	TCPRouteRule           = gatewayv1.TCPRouteRule
	GRPCRoute              = gatewayv1.GRPCRoute
	GRPCRouteList          = gatewayv1.GRPCRouteList
	GRPCRouteSpec          = gatewayv1.GRPCRouteSpec
	GRPCRouteRule          = gatewayv1.GRPCRouteRule
	GRPCBackendRef         = gatewayv1.GRPCBackendRef
	UDPRoute               = gatewayv1.UDPRoute
	UDPRouteList           = gatewayv1.UDPRouteList
	UDPRouteSpec           = gatewayv1.UDPRouteSpec
	UDPRouteStatus         = gatewayv1.UDPRouteStatus
	UDPRouteRule           = gatewayv1.UDPRouteRule
)

type (
	Duration                 = gatewayv1.Duration
	HTTPRouteRetry           = gatewayv1.HTTPRouteRetry
	HTTPRouteRetryStatusCode = gatewayv1.HTTPRouteRetryStatusCode
)

var GroupVersion = gatewayv1.GroupVersion

const (
	GroupName                             = gatewayv1.GroupName
	HTTPProtocolType                      = gatewayv1.HTTPProtocolType
	HTTPSProtocolType                     = gatewayv1.HTTPSProtocolType
	TLSProtocolType                       = gatewayv1.TLSProtocolType
	UDPProtocolType                       = gatewayv1.UDPProtocolType
	TCPProtocolType                       = gatewayv1.TCPProtocolType
	HTTPRouteFilterExtensionRef           = gatewayv1.HTTPRouteFilterExtensionRef
	GRPCRouteFilterExtensionRef           = gatewayv1.GRPCRouteFilterExtensionRef
	HTTPRouteFilterRequestHeaderModifier  = gatewayv1.HTTPRouteFilterRequestHeaderModifier
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
	RouteReasonUnsupportedValue           = gatewayv1.RouteReasonUnsupportedValue
	TLSModeTerminate                      = gatewayv1.TLSModeTerminate
	TLSModePassthrough                    = gatewayv1.TLSModePassthrough
)

type (
	AnnotationKey                             = gatewayv1.AnnotationKey
	AnnotationValue                           = gatewayv1.AnnotationValue
	BackendTLSPolicy                          = gatewayv1.BackendTLSPolicy
	BackendTLSPolicyList                      = gatewayv1.BackendTLSPolicyList
	BackendTLSPolicySpec                      = gatewayv1.BackendTLSPolicySpec
	BackendTLSPolicyValidation                = gatewayv1.BackendTLSPolicyValidation
	GRPCHeaderMatch                           = gatewayv1.GRPCHeaderMatch
	GRPCHeaderMatchType                       = gatewayv1.GRPCHeaderMatchType
	GRPCHeaderName                            = gatewayv1.GRPCHeaderName
	GRPCMethodMatch                           = gatewayv1.GRPCMethodMatch
	GRPCMethodMatchType                       = gatewayv1.GRPCMethodMatchType
	GRPCRouteMatch                            = gatewayv1.GRPCRouteMatch
	GRPCRouteStatus                           = gatewayv1.GRPCRouteStatus
	GatewayClassList                          = gatewayv1.GatewayClassList
	GatewayClassStatus                        = gatewayv1.GatewayClassStatus
	GatewayStatus                             = gatewayv1.GatewayStatus
	HTTPHeaderMatch                           = gatewayv1.HTTPHeaderMatch
	HTTPHeaderName                            = gatewayv1.HTTPHeaderName
	HTTPMethod                                = gatewayv1.HTTPMethod
	HTTPPathMatch                             = gatewayv1.HTTPPathMatch
	HTTPPathModifier                          = gatewayv1.HTTPPathModifier
	HTTPPathModifierType                      = gatewayv1.HTTPPathModifierType
	HTTPQueryParamMatch                       = gatewayv1.HTTPQueryParamMatch
	HTTPRequestRedirectFilter                 = gatewayv1.HTTPRequestRedirectFilter
	HTTPRouteTimeouts                         = gatewayv1.HTTPRouteTimeouts
	HTTPURLRewriteFilter                      = gatewayv1.HTTPURLRewriteFilter
	HeaderMatchType                           = gatewayv1.HeaderMatchType
	ListenerConditionReason                   = gatewayv1.ListenerConditionReason
	ListenerConditionType                     = gatewayv1.ListenerConditionType
	ListenerStatus                            = gatewayv1.ListenerStatus
	LocalPolicyTargetReference                = gatewayv1.LocalPolicyTargetReference
	LocalPolicyTargetReferenceWithSectionName = gatewayv1.LocalPolicyTargetReferenceWithSectionName
	PathMatchType                             = gatewayv1.PathMatchType
	PolicyAncestorStatus                      = gatewayv1.PolicyAncestorStatus
	PolicyStatus                              = gatewayv1.PolicyStatus
	PreciseHostname                           = gatewayv1.PreciseHostname
	ProtocolType                              = gatewayv1.ProtocolType
	RouteConditionReason                      = gatewayv1.RouteConditionReason
	RouteStatus                               = gatewayv1.RouteStatus
	SecretObjectReference                     = gatewayv1.SecretObjectReference
	SubjectAltName                            = gatewayv1.SubjectAltName
	SubjectAltNameType                        = gatewayv1.SubjectAltNameType
	WellKnownCACertificatesType               = gatewayv1.WellKnownCACertificatesType
)

const (
	FullPathHTTPPathModifier              = gatewayv1.FullPathHTTPPathModifier
	GRPCHeaderMatchExact                  = gatewayv1.GRPCHeaderMatchExact
	GRPCMethodMatchExact                  = gatewayv1.GRPCMethodMatchExact
	GRPCMethodMatchRegularExpression      = gatewayv1.GRPCMethodMatchRegularExpression
	GatewayClassConditionStatusAccepted   = gatewayv1.GatewayClassConditionStatusAccepted
	GatewayClassReasonAccepted            = gatewayv1.GatewayClassReasonAccepted
	GatewayConditionAccepted              = gatewayv1.GatewayConditionAccepted
	GatewayConditionProgrammed            = gatewayv1.GatewayConditionProgrammed
	GatewayReasonAccepted                 = gatewayv1.GatewayReasonAccepted
	GatewayReasonPending                  = gatewayv1.GatewayReasonPending
	GatewayReasonProgrammed               = gatewayv1.GatewayReasonProgrammed
	HTTPMethodDelete                      = gatewayv1.HTTPMethodDelete
	HTTPMethodGet                         = gatewayv1.HTTPMethodGet
	HTTPRouteFilterCORS                   = gatewayv1.HTTPRouteFilterCORS
	HTTPRouteFilterRequestMirror          = gatewayv1.HTTPRouteFilterRequestMirror
	HTTPRouteFilterRequestRedirect        = gatewayv1.HTTPRouteFilterRequestRedirect
	HTTPRouteFilterResponseHeaderModifier = gatewayv1.HTTPRouteFilterResponseHeaderModifier
	HTTPRouteFilterURLRewrite             = gatewayv1.HTTPRouteFilterURLRewrite
	HeaderMatchExact                      = gatewayv1.HeaderMatchExact
	HeaderMatchRegularExpression          = gatewayv1.HeaderMatchRegularExpression
	HostnameAddressType                   = gatewayv1.HostnameAddressType
	HostnameSubjectAltNameType            = gatewayv1.HostnameSubjectAltNameType
	IPAddressType                         = gatewayv1.IPAddressType
	ListenerConditionAccepted             = gatewayv1.ListenerConditionAccepted
	ListenerConditionConflicted           = gatewayv1.ListenerConditionConflicted
	ListenerConditionResolvedRefs         = gatewayv1.ListenerConditionResolvedRefs
	ListenerReasonAccepted                = gatewayv1.ListenerReasonAccepted
	ListenerReasonHostnameConflict        = gatewayv1.ListenerReasonHostnameConflict
	ListenerReasonInvalid                 = gatewayv1.ListenerReasonInvalid
	ListenerReasonInvalidCertificateRef   = gatewayv1.ListenerReasonInvalidCertificateRef
	ListenerReasonInvalidRouteKinds       = gatewayv1.ListenerReasonInvalidRouteKinds
	ListenerReasonNoConflicts             = gatewayv1.ListenerReasonNoConflicts
	ListenerReasonPending                 = gatewayv1.ListenerReasonPending
	ListenerReasonPortUnavailable         = gatewayv1.ListenerReasonPortUnavailable
	ListenerReasonProgrammed              = gatewayv1.ListenerReasonProgrammed
	ListenerReasonProtocolConflict        = gatewayv1.ListenerReasonProtocolConflict
	ListenerReasonRefNotPermitted         = gatewayv1.ListenerReasonRefNotPermitted
	ListenerReasonResolvedRefs            = gatewayv1.ListenerReasonResolvedRefs
	ListenerReasonUnsupportedProtocol     = gatewayv1.ListenerReasonUnsupportedProtocol
	PathMatchExact                        = gatewayv1.PathMatchExact
	PathMatchPathPrefix                   = gatewayv1.PathMatchPathPrefix
	PathMatchRegularExpression            = gatewayv1.PathMatchRegularExpression
	PolicyConditionAccepted               = gatewayv1.PolicyConditionAccepted
	PolicyReasonAccepted                  = gatewayv1.PolicyReasonAccepted
	PolicyReasonConflicted                = gatewayv1.PolicyReasonConflicted
	PolicyReasonInvalid                   = gatewayv1.PolicyReasonInvalid
	PrefixMatchHTTPPathModifier           = gatewayv1.PrefixMatchHTTPPathModifier
	QueryParamMatchExact                  = gatewayv1.QueryParamMatchExact
	QueryParamMatchRegularExpression      = gatewayv1.QueryParamMatchRegularExpression
	URISubjectAltNameType                 = gatewayv1.URISubjectAltNameType
)

const (
	// V1Group is the Gateway API group as a typed Group.
	V1Group = Group(GroupName)

	// TLSVerifyDepthKey is the key used to store the tls verify depth.
	// This is used in the BackendTLSPolicy options.
	TLSVerifyDepthKey AnnotationKey = "tls-verify-depth"
)

// V1GroupVersion is the version of the Gateway API v1 group.
var V1GroupVersion = gatewayv1.GroupVersion.Version
