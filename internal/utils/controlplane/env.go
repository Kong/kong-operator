package controlplane

// TODO(mlavacca): comment
const (
	// metadata Env Vars
	PodNamespaceEnvVarName = "POD_NAMESPACE"
	PodNameEnvVarName      = "POD_NAME"

	// Controller Env Vars
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
	ControllerFeatureGatesEnvVarName                = "CONTROLLER_FEATURE_GATES"

	// Konnect Env vars
	ControllerKonnectAddressEnvVarName          = "CONTROLLER_KONNECT_ADDRESS"
	ControllerKonnectControlPlaneIDEnvVarName   = "CONTROLLER_KONNECT_CONTROL_PLANE_ID"
	ControllerKonnectLicensingEnabledEnvVarName = "CONTROLLER_KONNECT_LICENSING_ENABLED"
	ControllerKonnectSyncEnabledEnvVarName      = "CONTROLLER_KONNECT_SYNC_ENABLED"
	ControllerKonnectTLSClientCertEnvVarName    = "CONTROLLER_KONNECT_TLS_CLIENT_CERT"
	ControllerKonnectTLSClientKeyEnvVarName     = "CONTROLLER_KONNECT_TLS_CLIENT_KEY"
)
