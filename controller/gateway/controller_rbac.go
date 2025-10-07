package gateway

// -----------------------------------------------------------------------------
// GatewayReconciler - RBAC Permissions
// -----------------------------------------------------------------------------

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/finalizers,verbs=update
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=referencegrants,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=create;get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=create;get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=gatewayconfigurations,verbs=get;list;watch
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=create;get;update;patch;list;watch;delete

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes,verbs=create;get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectextensions,verbs=create;get;list;watch;update;patch;delete
