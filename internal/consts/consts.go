package consts

// -----------------------------------------------------------------------------
// Consts - Standard Kubernetes Object Labels
// -----------------------------------------------------------------------------

const (
	// GatewayOperatorControlledLabel is the label that is used for objects which
	// were created by this operator.
	GatewayOperatorControlledLabel = "konghq.com/gateway-operator"

	// DataPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the dataplane controller.
	DataPlaneManagedLabelValue = "dataplane"

	// ControlPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the controlplane controller.
	ControlPlaneManagedLabelValue = "controlplane"

	// GatewayManagedLabelValue indicates that the object's lifecycle is managed by
	// the gateway controller.
	GatewayManagedLabelValue = "gateway"
)

// -----------------------------------------------------------------------------
// Consts - Kubernetes GenerateName prefixes
// -----------------------------------------------------------------------------

const (
	// DataPlanePrefix is used as a name prefix to generate dataplane-owned objects' name
	DataPlanePrefix = "dataplane"

	// ControlPlanePrefix is used as a name prefix to generate controlplane-owned objects' name
	ControlPlanePrefix = "controlplane"
)

// -----------------------------------------------------------------------------
// Consts - Container Parameters
// -----------------------------------------------------------------------------

const (
	// DefaultDataPlaneBaseImage is the base container image that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultDataPlaneBaseImage = "kong"

	// DefaultDataPlaneTag is the base container image tag that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image tag fail.
	DefaultDataPlaneTag = "2.8" // TODO: automatic PR updates https://github.com/Kong/gateway-operator/issues/209

	// DefaultDataPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultDataPlaneImage = DefaultDataPlaneBaseImage + ":" + DefaultDataPlaneTag

	// DefaultControlPlaneBaseImage is the base container image that can be used
	// by default for a ControlPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultControlPlaneBaseImage = "kong/kubernetes-ingress-controller"

	// DefaultControlPlaneTag is the base container image tag that can be used
	// by default for a ControlPlane resource if all other attempts to dynamically
	// decide an image tag fail.
	DefaultControlPlaneTag = "2.5" // TODO: automatic PR updates https://github.com/Kong/gateway-operator/issues/209

	// DefaultControlPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultControlPlaneImage = DefaultControlPlaneBaseImage + ":" + DefaultControlPlaneTag

	// ControlPlaneControllerContainerName is the name of the ingress controller container in a ControlPlane Deployment
	ControlPlaneControllerContainerName = "controller"

	// DataPlaneProxyContainerName is the name of the Kong proxy container
	DataPlaneProxyContainerName = "proxy"
)

// -----------------------------------------------------------------------------
// Consts - DataPlane exposed ports
// -----------------------------------------------------------------------------

const (
	// DataPlaneHTTPSPort is the port that the dataplane uses for Admin API.
	DataPlaneAdminAPIPort = 8444

	// DataPlaneHTTPSPort is the port that the dataplane uses for HTTP.
	DataPlaneProxyPort = 8000

	// DataPlaneHTTPSPort is the port that the dataplane uses for HTTPS.
	DataPlaneProxySSLPort = 8443

	// DataPlaneHTTPSPort is the port that the dataplane uses for metrics.
	DataPlaneMetricsPort = 8100
)

// -----------------------------------------------------------------------------
// Consts - Names for Shared Resources
// -----------------------------------------------------------------------------

const (
	ClusterCertificateVolume = "cluster-certificate"
)

// -----------------------------------------------------------------------------
// Consts - Environment Variable Names
// -----------------------------------------------------------------------------

const (
	// EnvVarKongDatabase is the environment variable name to specify database
	// backend used for dataplane(Kong gateway). Currently only DBLess mode
	// (empty, or "off") is supported.
	EnvVarKongDatabase = "KONG_DATABASE"
)
