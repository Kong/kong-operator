package controlplane

// -----------------------------------------------------------------------------
// Reconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/status,verbs=update;patch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplanes/finalizers,verbs=update

// Rules allowing deleting old owned resources.
// TODO: delete after a few releases.
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=list;watch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=list;watch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=list;watch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=list;watch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=list;watch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=list;watch;delete
