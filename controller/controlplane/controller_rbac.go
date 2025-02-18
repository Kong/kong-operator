package controlplane

// -----------------------------------------------------------------------------
// Reconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/status,verbs=update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/finalizers,verbs=update
