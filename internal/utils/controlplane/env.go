package controlplane

// TODO(mlavacca): comment
const (
	PodNamespaceEnvVarName                          = "POD_NAMESPACE"
	PodNameEnvVarName                               = "POD_NAME"
	ControllerAnonymousReportsEnvVarName            = "CONTROLLER_ANONYMOUS_REPORTS"
	ControllerGatewayAPIControllerNameEnvVarName    = "CONTROLLER_GATEWAY_API_CONTROLLER_NAME"
	ControllerPublishServiceEnvVarName              = "CONTROLLER_PUBLISH_SERVICE"
	ControllerKongAdminSVCEnvVarName                = "CONTROLLER_KONG_ADMIN_SVC"
	ControllerKongAdminSVCPortNamesEnvVarName       = "CONTROLLER_KONG_ADMIN_SVC_PORT_NAMES"
	ControllerGatewayDiscoveryDNSStrategyEnvVarName = "CONTROLLER_GATEWAY_DISCOVERY_DNS_STRATEGY"
	ControllerKongAdminInitRetryDelayEnvVarName     = "CONTROLLER_KONG_ADMIN_INIT_RETRY_DELAY"
	ControllerGatewayToReconcileEnvVarName          = "CONTROLLER_GATEWAY_TO_RECONCILER"
	ControllerKongAdminTLSClientCertFileEnvVarName  = "CONTROLLER_KONG_ADMIN_TLS_CLIENT_CERT_FILE"
	ControllerKongAdminTLSClientKeyFileEnvVarName   = "CONTROLLER_KONG_ADMIN_TLS_CLIENT_KEY_FILE"
	ControllerKongAdminCACertFileEnvVarName         = "CONTROLLER_KONG_ADMIN_CA_CERT_FILE"
	ControllerElectionIDEnvVarName                  = "CONTROLLER_ELECTION_ID"
	ControllerAdmissionWebhookListenEnvVarName      = "CONTROLLER_ADMISSION_WEBHOOK_LISTEN"
)
