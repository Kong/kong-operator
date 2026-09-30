package konnect

// RBAC rules for the KonnectConfigStoreSync controller.
//
// The controller reads its own kind (and patches it to manage the cleanup
// finalizer), the referenced Secrets, the referenced KonnectConfigStores, and
// (along the credential chain) the referenced KonnectGatewayControlPlanes and
// KonnectAPIAuthConfigurations. It lists KongReferenceGrants to authorize
// cross-namespace references and KongCertificates for the deletion in-use
// check. It emits events.k8s.io Events.

//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectconfigstoresyncs,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectconfigstoresyncs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectconfigstoresyncs/finalizers,verbs=update;patch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectconfigstores,verbs=get;list;watch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectgatewaycontrolplanes,verbs=get;list;watch
//+kubebuilder:rbac:groups=konnect.konghq.com,resources=konnectapiauthconfigurations,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongreferencegrants,verbs=get;list;watch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates,verbs=get;list;watch
