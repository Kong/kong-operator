package konnect

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongvaults,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongvaults/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
