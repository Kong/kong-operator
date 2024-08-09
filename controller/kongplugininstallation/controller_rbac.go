package kongplugininstallation

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=kongplugininstallations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=kongplugininstallations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;
//+kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get
