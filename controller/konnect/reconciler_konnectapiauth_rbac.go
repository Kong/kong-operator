package konnect

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
