package consts

import "github.com/kong/gateway-operator/internal/versions"

// ServiceType is a re-typing of string to be used to distinguish between proxy and admin service
type ServiceType string

// -----------------------------------------------------------------------------
// Consts - Standard Kubernetes Object Labels
// -----------------------------------------------------------------------------

const (
	// OperatorLabelPrefix is the common label prefix used by the operator
	OperatorLabelPrefix = "gateway-operator.konghq.com/"

	// GatewayOperatorManagedByLabel is the label that is used for objects which
	// were created by this operator.
	// The value associated with this label indicated what component is controlling
	// the resource that has this label set.
	GatewayOperatorManagedByLabel = OperatorLabelPrefix + "managed-by"

	// GatewayOperatorManagedByLabelLegacy is the legacy label used for object
	// with were created by this operator
	//
	// Deprecated: use GatewayOperatorManagedByLabel instead.
	// TODO: Remove adding this to managed resources after several versions with
	// the new managed-by label were released: https://github.com/Kong/gateway-operator/issues/1101
	GatewayOperatorManagedByLabelLegacy = "konghq.com/gateway-operator"

	// ControlPlaneManagedLabelValue indicates that an object's lifecycle is managed
	// by the controlplane controller.
	ControlPlaneManagedLabelValue = "controlplane"

	// GatewayManagedLabelValue indicates that the object's lifecycle is managed by
	// the gateway controller.
	GatewayManagedLabelValue = "gateway"

	// ServiceSecretLabel is a label that is added to operator related Service
	// Secrets to designate which Service this particular Secret it used by.
	ServiceSecretLabel = OperatorLabelPrefix + "service-secret"

	// OperatorLabelSelector is a label name that is used for operator resources
	// as a label selector key.
	// Used with e.g. DataPlane's status.selector field.
	OperatorLabelSelector = OperatorLabelPrefix + "selector"
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
	// ClusterCertificateVolume is the name of the volume that holds the certificate
	// and keys which are used for serving traffic and  ControlPlane and DataPlane communication.
	ClusterCertificateVolume = "cluster-certificate"

	// ClusterCertificateVolumeMountPath holds the path where cluster certificate
	// volume will be mounted.
	ClusterCertificateVolumeMountPath = "/var/cluster-certificate"
)

// -----------------------------------------------------------------------------
// Consts - Webhook-related parameters
// -----------------------------------------------------------------------------

const (
	// WebhookCertificateConfigBaseImage is the image to use by the certificate config Jobs.
	WebhookCertificateConfigBaseImage = "registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.3.0"
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
