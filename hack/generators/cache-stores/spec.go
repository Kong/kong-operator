package main

// supportedTypes is a list of types that the cache stores support.
// To add a new type support, add a new entry to this list.
var supportedTypes = []cacheStoreSupportedType{
	// Core Kubernetes types
	{
		Type:       "Ingress",
		Package:    "netv1",
		StoreField: "IngressV1",
	},
	{
		Type:       "IngressClass",
		Package:    "netv1",
		StoreField: "IngressClassV1",
		KeyFunc:    clusterWideKeyFunc,
	},
	{
		Type:    "Service",
		Package: "corev1",
	},
	{
		Type:    "Secret",
		Package: "corev1",
	},
	{
		Type:    "ConfigMap",
		Package: "corev1",
	},
	{
		Type:    "EndpointSlice",
		Package: "discoveryv1",
	},
	// Gateway API types
	{
		Type:    "HTTPRoute",
		Package: "gatewayapi",
	},
	{
		Type:    "UDPRoute",
		Package: "gatewayapi",
	},
	{
		Type:    "TCPRoute",
		Package: "gatewayapi",
	},
	{
		Type:    "TLSRoute",
		Package: "gatewayapi",
	},
	{
		Type:    "GRPCRoute",
		Package: "gatewayapi",
	},
	{
		Type:    "ReferenceGrant",
		Package: "gatewayapi",
	},
	{
		Type:    "Gateway",
		Package: "gatewayapi",
	},
	{
		Type:    "BackendTLSPolicy",
		Package: "gatewayapi",
	},
	// Kong types
	{
		Type:       "KongPlugin",
		Package:    "kongv1",
		StoreField: "Plugin",
	},
	{
		Type:       "KongClusterPlugin",
		Package:    "kongv1",
		StoreField: "ClusterPlugin",
		KeyFunc:    clusterWideKeyFunc,
	},
	{
		Type:       "KongConsumer",
		Package:    "kongv1",
		StoreField: "Consumer",
	},
	{
		Type:       "KongConsumerGroup",
		Package:    "kongv1beta1",
		StoreField: "ConsumerGroup",
	},
	{
		Type:    "KongUpstreamPolicy",
		Package: "kongv1beta1",
	},
	{
		Type:       "IngressClassParameters",
		Package:    "kongv1alpha1",
		StoreField: "IngressClassParametersV1alpha1",
	},
	{
		Type:    "KongServiceFacade",
		Package: "incubatorv1alpha1",
	},
	{
		Type:    "KongVault",
		Package: "kongv1alpha1",
		KeyFunc: clusterWideKeyFunc,
	},
	{
		Type:    "KongCustomEntity",
		Package: "kongv1alpha1",
	},
	// v1alpha1 Kong Gateway entity types (KIC standalone support)
	{
		Type:       "KongService",
		Package:    "kongv1alpha1",
		StoreField: "KongServiceV1Alpha1",
	},
	{
		Type:       "KongRoute",
		Package:    "kongv1alpha1",
		StoreField: "KongRouteV1Alpha1",
	},
	{
		Type:       "KongUpstream",
		Package:    "kongv1alpha1",
		StoreField: "KongUpstreamV1Alpha1",
	},
	{
		Type:       "KongTarget",
		Package:    "kongv1alpha1",
		StoreField: "KongTargetV1Alpha1",
	},
	{
		Type:       "KongCertificate",
		Package:    "kongv1alpha1",
		StoreField: "KongCertificateV1Alpha1",
	},
	{
		Type:       "KongCACertificate",
		Package:    "kongv1alpha1",
		StoreField: "KongCACertificateV1Alpha1",
	},
	{
		Type:       "KongSNI",
		Package:    "kongv1alpha1",
		StoreField: "KongSNIV1Alpha1",
	},
	{
		Type:       "KongPluginBinding",
		Package:    "kongv1alpha1",
		StoreField: "KongPluginBindingV1Alpha1",
	},
}
