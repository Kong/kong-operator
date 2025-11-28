package konnect

// -----------------------------------------------------------------------------
// KonnectExtensionReconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=controlplane,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=konnectextensions,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=konnectextensions/status,verbs=patch;update
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=konnectextensions/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectextensions,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectextensions/status,verbs=patch;update
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectextensions/finalizers,verbs=update;patch
// +kubebuilder:rbac:groups="konnect.konghq.com",resources="konnectgatewaycontrolplanes",verbs=get;list;watch
// +kubebuilder:rbac:groups="konnect.konghq.com",resources="konnectapiauthconfigurations",verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups="",resources=secrets/finalizers,verbs=patch;update
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongdataplaneclientcertificates,verbs=create;get;list;delete;update;patch;watch
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongdataplaneclientcertificates/status,verbs=get;list;watch
