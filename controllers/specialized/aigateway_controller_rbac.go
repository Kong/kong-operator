package specialized

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=aigateways/finalizers,verbs=update
