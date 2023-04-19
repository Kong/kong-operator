package consts

// ServiceType is a re-typing of string to be used to distinguish between proxy and admin service
type ServiceType string

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

	// DataPlaneServiceTypeLabel is the labels that is used for the services created by
	// the DataPlane controller to expose the DataPlane deployment.
	DataPlaneServiceTypeLabel = "konghq.com/dataplane-service-type"

	// DataPlaneAdminServiceLabelValue indicates that the service is intended to expose the
	// DataPlane admin API.
	DataPlaneAdminServiceLabelValue ServiceType = "admin"

	// DataPlaneProxyServiceLabelValue indicates that the service is inteded to expose the
	// DataPlane proxy.
	DataPlaneProxyServiceLabelValue ServiceType = "proxy"
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
	DefaultDataPlaneTag = "3.2.2" // TODO: automatic PR updates https://github.com/Kong/gateway-operator/issues/209

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
	DefaultControlPlaneTag = "2.9.3" // TODO: automatic PR updates https://github.com/Kong/gateway-operator/issues/210

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

// -----------------------------------------------------------------------------
// Consts - Webhook-related parameters
// -----------------------------------------------------------------------------

const (
	// WebhookCertificateConfigBaseImage is the image to use by the certificate config Jobs.
	WebhookCertificateConfigBaseImage = "k8s.gcr.io/ingress-nginx/kube-webhook-certgen:v1.1.1"
	// WebhookName is the ValidatingWebhookConfiguration name.
	WebhookName = "gateway-operator-validation.konghq.com"
	// WebhookCertificateConfigSecretName is the name of the secret containing the webhook certificate.
	WebhookCertificateConfigSecretName = "gateway-operator-webhook-certs"
	// WebhookCertificateConfigName is the name given to the resources related by the certificate config Jobs.
	WebhookCertificateConfigName = "gateway-operator-admission"
	// WebhookCertificateConfigLabelvalue is the default label for all the resources related
	// to the certificate config Jobs.
	WebhookCertificateConfigLabelvalue = "gateway-operator-certificate-config"
	// WebhookServiceName is the name of the service that exposes the validating webhook
	WebhookServiceName = "gateway-operator-validating-webhook"
)
