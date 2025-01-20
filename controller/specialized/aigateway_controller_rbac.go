package specialized

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/status,verbs=update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/finalizers,verbs=update

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
