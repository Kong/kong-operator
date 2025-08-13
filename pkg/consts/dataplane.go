package consts

// -----------------------------------------------------------------------------
// Consts - DataPlane Generic Parameters
// -----------------------------------------------------------------------------

const (
	// DataPlanePrefix is used as a name prefix to generate dataplane-owned objects' name
	DataPlanePrefix = "dataplane"
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Labels and Annotations
// -----------------------------------------------------------------------------

const (
	// DataPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the dataplane controller.
	DataPlaneManagedLabelValue = "dataplane"

	// DataPlaneServiceTypeLabel is the label that is used for the services created by
	// the DataPlane controller to expose the DataPlane deployment.
	DataPlaneServiceTypeLabel = "gateway-operator.konghq.com/dataplane-service-type"

	// DataPlaneServiceStateLabel indicates the state of a DataPlane service.
	// Useful for progressive rollouts.
	DataPlaneServiceStateLabel = "gateway-operator.konghq.com/dataplane-service-state"

	// DataPlaneDeploymentStateLabel indicates the state of a DataPlane deployment.
	// Useful for progressive rollouts.
	DataPlaneDeploymentStateLabel = "gateway-operator.konghq.com/dataplane-deployment-state"

	// AnnotationLastAppliedAnnotations is the annotation key to store the last annotations
	// of a DataPlane-owned object (e.g. Ingress `Service`) applied by the DataPlane controller.
	// It allows the controller to decide which annotations are outdated compared to the DataPlane spec and
	// shall be removed. This guarantees no interference with annotations from other sources (e.g. users).
	AnnotationLastAppliedAnnotations = "gateway-operator.konghq.com/last-applied-annotations"

	// DataPlanePodStateLabel indicates the state of a DataPlane Pod.
	// Useful for progressive rollouts.
	DataPlanePodStateLabel = "gateway-operator.konghq.com/dataplane-pod-state"

	// DataPlaneStateLabelValuePreview indicates that a DataPlane resource is
	// a "preview" resource.
	// This is used in:
	// - the "preview" Service that is available to access the "preview" DataPlane Pods.
	// - the "preview" Deployment wraps the "preview" DataPlane Pods.
	DataPlaneStateLabelValuePreview = "preview"

	// DataPlaneStateLabelValueLive indicates that a DataPlane resource is
	// a "live" resource.
	// This is used in:
	// - the "live" Service that is available to access the "live" DataPlane Pods.
	// - the "live" Deployment wraps the "live" DataPlane Pods.
	DataPlaneStateLabelValueLive = "live"

	// DataPlaneAdminServiceLabelValue indicates that the service is intended to expose the
	// DataPlane admin API.
	DataPlaneAdminServiceLabelValue ServiceType = "admin"

	// DataPlaneIngressServiceLabelValue indicates that the service is intended to expose the
	// DataPlane proxy.
	DataPlaneIngressServiceLabelValue ServiceType = "ingress"
)

// -----------------------------------------------------------------------------
// Consts - Router flavor parameters
// -----------------------------------------------------------------------------

// RouterFlavor represents the flavor of the Kong router.
// ref: https://docs.konghq.com/gateway/latest/reference/configuration/#router_flavor
type RouterFlavor string

const (
	// RouterFlavorTraditionalCompatible is the traditional compatible router flavor.
	RouterFlavorTraditionalCompatible RouterFlavor = "traditional_compatible"
	// RouterFlavorExpressions is the expressions router flavor.
	RouterFlavorExpressions RouterFlavor = "expressions"
	// DefaultRouterFlavor is the default router flavor.
	// https://developer.konghq.com/gateway/configuration/#router-flavor
	DefaultRouterFlavor = RouterFlavorTraditionalCompatible
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Container Parameters
// -----------------------------------------------------------------------------

const (
	// DefaultDataPlaneBaseImage is the base container image that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultDataPlaneBaseImage = "kong"

	// DefaultDataPlaneBaseEnterpriseImage is the enterprise base container image.
	DefaultDataPlaneBaseEnterpriseImage = "kong/kong-gateway"

	// DefaultDataPlaneTag is the base container image tag that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image tag fail.
	DefaultDataPlaneTag = "3.9" // renovate: datasource=docker depName=kong

	// DefaultDataPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultDataPlaneImage = DefaultDataPlaneBaseImage + ":" + DefaultDataPlaneTag

	// DefaultDataPlaneEnterpriseImage is the default enterprise container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultDataPlaneEnterpriseImage = DefaultDataPlaneBaseEnterpriseImage + ":" + DefaultDataPlaneTag

	// ServiceSelectorOverrideAnnotation is used on the dataplane to override the Selector
	// of both the admin and proxy services.
	// The value of such an annotation is to be intended as a comma-separated list of
	// key=value selectors, so that it is possible to add multiple selectors to the same
	// service.
	//
	// Example:
	// gateway-operator.konghq.com/service-selector-override: "key1=value,key2=value2"
	ServiceSelectorOverrideAnnotation = "gateway-operator.konghq.com/service-selector-override"

	// DataPlaneProxyContainerName is the name of the Kong proxy container
	DataPlaneProxyContainerName = "proxy"

	// DataPlaneStatusEndpoint is the endpoint to use for DataPlane readiness probe.
	DataPlaneStatusEndpoint = "/status"

	// DataPlaneStatusReadyEndpoint is the endpoint to use for DataPlane readiness probe
	// in the context of managed gateways.
	DataPlaneStatusReadyEndpoint = "/status/ready"
)

// -----------------------------------------------------------------------------
// Consts - DataPlane ports
// -----------------------------------------------------------------------------

const (
	// DefaultHTTPPort is the default port used for HTTP ingress network traffic
	// from outside clusters.
	DefaultHTTPPort = 80

	// DefaultHTTPSPort is the default port used for HTTPS ingress network traffic
	// from outside clusters.
	DefaultHTTPSPort = 443

	// DataPlaneAdminAPIPort is the port that the dataplane uses for Admin API.
	DataPlaneAdminAPIPort = 8444

	// DataPlaneProxyPort is the port that the dataplane uses for HTTP.
	DataPlaneProxyPort = 8000

	// DataPlaneProxySSLPort is the port that the dataplane uses for HTTPS.
	DataPlaneProxySSLPort = 8443

	// DataPlaneMetricsPort is the port that the dataplane uses for metrics.
	DataPlaneMetricsPort = 8100

	// DataPlaneStatusPort is the port that the dataplane uses for status.
	DataPlaneStatusPort = 8100
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Services parameters
// -----------------------------------------------------------------------------
const (
	// DataPlaneAdminServicePortName is the port name of the DataPlane admin service.
	DataPlaneAdminServicePortName = "admin"

	// DataPlanePODDNSDiscoveryStrategy is DNS strategy to use when creating Gateway's Admin API addresses.
	DataPlaneServiceDNSDiscoveryStrategy = "service"
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Environment Variable Names
// -----------------------------------------------------------------------------

const (
	// EnvVarKongDatabase is the environment variable name to specify database
	// backend used for dataplane(Kong gateway). Currently only DBLess mode
	// (empty, or "off") is supported.
	EnvVarKongDatabase = "KONG_DATABASE"
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Finalizers
// -----------------------------------------------------------------------------

const (
	// DataPlaneOwnedWaitForOwnerFinalizer is the finalizer added to resources owned by a DataPlane
	// to ensure that the resources are not deleted before the DataPlane is deleted.
	DataPlaneOwnedWaitForOwnerFinalizer = "gateway-operator.konghq.com/wait-for-owner"
)

// -----------------------------------------------------------------------------
// Consts - KonnectExtension Finalizers
// -----------------------------------------------------------------------------

const (
	// ExtensionInUseFinalizer is the finalizer added to KonnectExtension
	// to ensure that the resources are not deleted when in use by a dataplane.
	ExtensionInUseFinalizer = "gateway-operator.konghq.com/extension-in-use"
)
