package konnect

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers,verbs=get;list;watch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
