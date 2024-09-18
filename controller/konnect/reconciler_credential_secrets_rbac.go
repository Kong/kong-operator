package konnect

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=kongconsumers,verbs=get;list;watch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=credentialbasicauths,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
