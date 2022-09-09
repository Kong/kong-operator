package controllers

// -----------------------------------------------------------------------------
// Webhook manager - RBAC Permissions
// -----------------------------------------------------------------------------

//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;create
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;create
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;create
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;create
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;create;update;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;create
