package controlplane

// -----------------------------------------------------------------------------
// Reconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/status,verbs=update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/finalizers,verbs=update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=watchnamespacegrants,verbs=list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
