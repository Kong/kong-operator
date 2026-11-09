package consts

// NOTE: These paths have to match paths used in Kubernetes configuration:
// config/default/manager_webhook_patch.yaml that is used for Helm Chart generation.
const (
	// DefaultAdmissionWebhookCertDir is the directory that holds the validating
	// admission webhook files.
	DefaultAdmissionWebhookCertDir = "/tmp/k8s-webhook-server/serving-certs/validating-admission-webhook"
	// DefaultAdmissionWebhookBasePath is the prefix of the paths to validating
	// admission webhook files. It ends with a slash.
	DefaultAdmissionWebhookBasePath = DefaultAdmissionWebhookCertDir + "/"
	// DefaultAdmissionWebhookCertPath is the default path to the any (validation, conversion) webhook server TLS certificate.
	DefaultAdmissionWebhookCertPath = DefaultAdmissionWebhookBasePath + "tls.crt"
	// DefaultAdmissionWebhookKeyPath is the default path to the any (validation, conversion) webhook server TLS key.
	DefaultAdmissionWebhookKeyPath = DefaultAdmissionWebhookBasePath + "tls.key"
)

// WebhookPort is the port where the validating (admission) webhook server listens.
const WebhookPort = 5443
