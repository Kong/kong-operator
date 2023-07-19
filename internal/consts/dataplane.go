package consts

// -----------------------------------------------------------------------------
// Consts - Container Parameters
// -----------------------------------------------------------------------------

const (
	// DefaultDataPlaneBaseImage is the base container image that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultDataPlaneBaseImage = "kong"

	// DefaultDataPlaneEnterpriseImage is the enterprise base container image.
	DefaultDataPlaneEnterpriseImage = "kong/kong-gateway"

	// DefaultDataPlaneTag is the base container image tag that can be used
	// by default for a DataPlane resource if all other attempts to dynamically
	// decide an image tag fail.
	DefaultDataPlaneTag = "3.3.1" // TODO: automatic PR updates https://github.com/Kong/gateway-operator/issues/209

	// DefaultDataPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultDataPlaneImage = DefaultDataPlaneBaseImage + ":" + DefaultDataPlaneTag

	// DataPlanePrefix is used as a name prefix to generate dataplane-owned objects' name
	DataPlanePrefix = "dataplane"

	// DataPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the dataplane controller.
	DataPlaneManagedLabelValue = "dataplane"

	// DataPlaneServiceTypeLabel is the labels that is used for the services created by
	// the DataPlane controller to expose the DataPlane deployment.
	DataPlaneServiceTypeLabel = "konghq.com/dataplane-service-type"

	// DataPlaneAdminServiceLabelValue indicates that the service is intended to expose the
	// DataPlane admin API.
	DataPlaneAdminServiceLabelValue ServiceType = "admin"

	// DataPlaneProxyServiceLabelValue indicates that the service is inteded to expose the
	// DataPlane proxy.
	DataPlaneProxyServiceLabelValue ServiceType = "proxy"

	// DataPlaneProxyContainerName is the name of the Kong proxy container
	DataPlaneProxyContainerName = "proxy"
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

	// DataPlaneHTTPSPort is the port that the dataplane uses for Admin API.
	DataPlaneAdminAPIPort = 8444

	// DataPlaneHTTPSPort is the port that the dataplane uses for HTTP.
	DataPlaneProxyPort = 8000

	// DataPlaneHTTPSPort is the port that the dataplane uses for HTTPS.
	DataPlaneProxySSLPort = 8443

	// DataPlaneHTTPSPort is the port that the dataplane uses for metrics.
	DataPlaneMetricsPort = 8100

	// DefaultKongStatusPort is the port that the dataplane users for status.
	DataPlaneStatusPort = 8100
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
