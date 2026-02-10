package cpextensions

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanemetricsextensions,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;patch;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=get;create;list;watch;delete;patch;update
