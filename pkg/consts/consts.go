package consts

import "time"

// ServiceType is a re-typing of string to be used to distinguish between proxy and admin service.
type ServiceType string

// -----------------------------------------------------------------------------
// Consts - Standard Kubernetes Object Labels
// -----------------------------------------------------------------------------

const (
	// OperatorLabelPrefix is the common label prefix used by the operator.
	OperatorLabelPrefix = "gateway-operator.konghq.com/"

	// OperatorAnnotationPrefix is the common annotation prefix used by the operator.
	OperatorAnnotationPrefix = OperatorLabelPrefix

	// GatewayOperatorManagedByLabel is the label that is used for objects which
	// were created by this operator.
	// The value associated with this label indicated what component is controlling
	// the resource that has this label set, e.g. controlplane.
	GatewayOperatorManagedByLabel = OperatorLabelPrefix + "managed-by"

	// GatewayOperatorManagedByNameLabel is the label that is used for objects which
	// were created by this operator.
	// The value set for this label is the name of the object that is controlling
	// the resource that has this label set.
	// This can be used e.g. as a link between a managing object and the managed object
	// specifying when there's a cross namespace reference which is disallowed by the
	// Kubernetes API.
	GatewayOperatorManagedByNameLabel = OperatorLabelPrefix + "managed-by-name"

	// GatewayOperatorManagedByNamespaceLabel is the label that is used for objects which
	// were created by this operator.
	// The value set for this label is the namespace of the object that is controlling
	// the resource that has this label set.
	// This can be used e.g. as a link between a managing object and the managed object
	// specifying when there's a cross namespace reference which is disallowed by the
	// Kubernetes API.
	GatewayOperatorManagedByNamespaceLabel = OperatorLabelPrefix + "managed-by-namespace"

	// GatewayOperatorKongPluginTypeLabel is the label set on KongPlugin instances
	// to indicate the type of the plugin.
	// It is used to filter KongPlugin instances that are managed by the ControlPlane.
	GatewayOperatorKongPluginTypeLabel = OperatorLabelPrefix + "kong-plugin-type"

	// GatewayOperatorOwnerUIDControlPlane is the label that is used for objects
	// to indicate a ControlPlane resource is the owner of the object.
	// The value set for this label is the UID of the ControlPlane resource that
	// owns the object.
	GatewayOperatorOwnerUIDControlPlane = OperatorLabelPrefix + "controlplane-owner-uid"

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

	// ControlPlaneServiceLabel is a Service's label that is used to indicate which kind of Service it is.
	ControlPlaneServiceLabel = OperatorLabelPrefix + "service"

	// SecretUsedByServiceLabel is a Secret's label that is used to indicate which Service kind is using the Secret.
	SecretUsedByServiceLabel = OperatorLabelPrefix + "secret-used-by-service"

	// ControlPlaneServiceKindAdmin is the value for SecretUsedByServiceLabel or ControlPlaneServiceLabel that
	// is used to indicate that a Service is an admin service.
	ControlPlaneServiceKindAdmin = "admin"

	// ControlPlaneServiceKindWebhook is the value for the SecretUsedByServiceLabel or ControlPlaneServiceLabel
	// that is used to indicate that a Service is a webhook service.
	ControlPlaneServiceKindWebhook = "webhook"

	// CertPurposeLabel indicates the purpose of a certificate.
	CertPurposeLabel = OperatorLabelPrefix + "cert-purpose"

	// ControlPlaneKGOCleanupAnnotation indicates that the clean up KGO related resources
	// has been performed for this ControlPlane.
	// NOTE: This will be removed together with the logic that performs the cleanup
	// as part of https://github.com/Kong/kong-operator/issues/2228.
	ControlPlaneKGOCleanupAnnotation = OperatorAnnotationPrefix + "kgo-cleanup"

	// GatewayStaticNamingAnnotation indicates that the gateway uses static naming for its resources.
	// This means that the DataPlane, ControlPlane and KonnectGatewayControlPlane resources
	// are named as the Gateway resource.
	GatewayStaticNamingAnnotation = OperatorAnnotationPrefix + "static-naming"
)

// -----------------------------------------------------------------------------
// Consts - Names and Paths for Shared Resources
// -----------------------------------------------------------------------------

const (
	// ClusterCertificateVolume is the name of the volume that holds the certificate
	// and keys which are used for serving traffic and  ControlPlane and DataPlane communication.
	ClusterCertificateVolume = "cluster-certificate"

	// ClusterCertificateVolumeMountPath holds the path where cluster certificate
	// volume will be mounted.
	ClusterCertificateVolumeMountPath = "/var/cluster-certificate"

	// TLSCRT is the filename for the tls.crt.
	TLSCRT = "tls.crt"

	// TLSKey is the filename for the tls.key.
	TLSKey = "tls.key"

	// CACRT is the filename for the ca.crt.
	CACRT = "ca.crt"

	// TLSCRTPath is the full path for the tls.crt file.
	TLSCRTPath = ClusterCertificateVolumeMountPath + "/" + TLSCRT

	// TLSKeyPath is the full path for the tls.key file.
	TLSKeyPath = ClusterCertificateVolumeMountPath + "/" + TLSKey

	// TLSCACRTPath is the full path for the ca.crt file.
	TLSCACRTPath = ClusterCertificateVolumeMountPath + "/" + CACRT

	// CertFieldSecret is the field name in Kubernetes secret - WebhookCertificateConfigSecretName.
	CertFieldSecret = "cert"

	// KeyFieldSecret is the field name in Kubernetes secret - WebhookCertificateConfigSecretName.
	KeyFieldSecret = "key"

	// CAFieldSecret is the field name in Kubernetes secret - WebhookCertificateConfigSecretName.
	CAFieldSecret = "ca"

	// KongClusterCertVolume is the name of the volume that holds the certificate the enables
	// communication between Kong and Konnect.
	KongClusterCertVolume = "kong-cluster-cert"

	// KongClusterCertVolumeMountPath holds the path where the Kong Cluster certificate
	// volume will be mounted.
	KongClusterCertVolumeMountPath = "/etc/secrets/kong-cluster-cert"
)

// -----------------------------------------------------------------------------
// Consts - Kong proxy environment variables
// -----------------------------------------------------------------------------

const (
	// ClusterCertEnvKey is the environment variable name for the cluster certificate.
	ClusterCertEnvKey = "KONG_CLUSTER_CERT"
	// ClusterCertKeyEnvKey is the environment variable name for the cluster certificate key.
	ClusterCertKeyEnvKey = "KONG_CLUSTER_CERT_KEY"
	// RouterFlavorEnvKey is the environment variable name for the Kong router flavor.
	RouterFlavorEnvKey = "KONG_ROUTER_FLAVOR"
)

// -----------------------------------------------------------------------------
// Consts - Konnect related consts
// -----------------------------------------------------------------------------

const (
	// DefaultKonnectSyncPeriod is the default sync period for Konnect entities.
	DefaultKonnectSyncPeriod = time.Minute

	// DefaultMaxConcurrentReconcilesKonnect is the default max concurrent
	// reconciles for Konnect entities controllers.
	DefaultMaxConcurrentReconcilesKonnect = uint(8)

	// DefaultMaxConcurrentReconcilesDataPlane is the default max concurrent
	// reconciles for the DataPlane controllers.
	DefaultMaxConcurrentReconcilesDataPlane = uint(1)

	// DefaultMaxConcurrentReconcilesControlPlane is the default max concurrent
	// reconciles for the ControlPlane controllers.
	DefaultMaxConcurrentReconcilesControlPlane = uint(1)

	// DefaultMaxConcurrentReconcilesGateway is the default max concurrent
	// reconciles for the Gateway controllers.
	DefaultMaxConcurrentReconcilesGateway = uint(1)
)

const (
	// KongIngressControllerPluginsAnnotation is the name of the annotation set on Services
	// which indicates to ControlPlane which KongPlugin instances to enable
	// for the Service.
	//
	// Ref: https://docs.konghq.com/kubernetes-ingress-controller/latest/reference/custom-resources/#kongplugin
	KongIngressControllerPluginsAnnotation = "konghq.com/plugins"

	// KongPluginNamePrometheus is the name of the KongPlugin for the Prometheus plugin.
	KongPluginNamePrometheus = "prometheus"
)
