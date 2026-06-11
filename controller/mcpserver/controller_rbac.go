package mcpserver

// +kubebuilder:rbac:groups=konnect.konghq.com,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=mcpservers/status,verbs=update;patch
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=mcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=create;get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
