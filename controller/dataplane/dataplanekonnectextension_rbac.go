package dataplane

// -----------------------------------------------------------------------------
// DataPlaneKonnectExtensionReconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanekonnectextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanekonnectextensions/finalizers,verbs=update;patch
