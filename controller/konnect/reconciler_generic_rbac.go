package konnect

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcontrolplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcontrolplanes/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
