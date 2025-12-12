package konnect

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations/status,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes/status,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaydataplanegroupconfigurations,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaydataplanegroupconfigurations/status,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaydataplanegroupconfigurations/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaytransitgateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaytransitgateways/status,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaytransitgateways/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups/status,verbs=get;update;patch

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaynetworks,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaynetworks/status,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectcloudgatewaynetworks/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcacertificates,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcacertificates/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcacertificates/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumergroups/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongconsumers/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialacls,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialacls/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialacls/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialapikeys,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialapikeys/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialapikeys/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialbasicauths/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialhmacs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialhmacs/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialhmacs/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialjwts,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialjwts/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcredentialjwts/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongdataplaneclientcertificates,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongdataplaneclientcertificates/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongdataplaneclientcertificates/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeys,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeys/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeys/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeysets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeysets/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongkeysets/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongvaults,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongvaults/status,verbs=update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongvaults/finalizers,verbs=update;patch

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongreferencegrants,verbs=get;list;watch
