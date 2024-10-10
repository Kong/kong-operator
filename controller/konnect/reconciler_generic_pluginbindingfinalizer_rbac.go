package konnect

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/finalizers,verbs=update

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/finalizers,verbs=update

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers,verbs=get
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers/finalizers,verbs=update

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups,verbs=get
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups/finalizers,verbs=update

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=delete;list
