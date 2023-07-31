package consts

import "github.com/kong/gateway-operator/internal/versions"

// ServiceType is a re-typing of string to be used to distinguish between proxy and admin service
type ServiceType string

// -----------------------------------------------------------------------------
// Consts - Standard Kubernetes Object Labels
// -----------------------------------------------------------------------------

const (
	// GatewayOperatorControlledLabel is the label that is used for objects which
	// were created by this operator.
	GatewayOperatorControlledLabel = "konghq.com/gateway-operator"

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

	// ControlPlanePrefix is used as a name prefix to generate controlplane-owned objects' name
	ControlPlanePrefix = "controlplane"
)

// -----------------------------------------------------------------------------
// Consts - Container Parameters
// -----------------------------------------------------------------------------

const (
	// DefaultControlPlaneBaseImage is the base container image that can be used
	// by default for a ControlPlane resource if all other attempts to dynamically
	// decide an image fail.
	DefaultControlPlaneBaseImage = "kong/kubernetes-ingress-controller"

	// DefaultControlPlaneImage is the default container image that can be used if
	// all other attempts to dynamically decide the default image fail.
	DefaultControlPlaneImage = DefaultControlPlaneBaseImage + ":" + versions.DefaultControlPlaneVersion

	// ControlPlaneControllerContainerName is the name of the ingress controller container in a ControlPlane Deployment
	ControlPlaneControllerContainerName = "controller"
)

// -----------------------------------------------------------------------------
// Consts - Names for Shared Resources
// -----------------------------------------------------------------------------

const (
	ClusterCertificateVolume = "cluster-certificate"
)

// -----------------------------------------------------------------------------
// Consts - Webhook-related parameters
// -----------------------------------------------------------------------------

const (
	// WebhookCertificateConfigBaseImage is the image to use by the certificate config Jobs.
	WebhookCertificateConfigBaseImage = "registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.1.1"
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
