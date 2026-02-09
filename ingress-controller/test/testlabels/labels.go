package testlabels

const (
	// Kind is the label key used to store the primary kind that's being tested.
	Kind = "kind"

	// KindUDPRoute is the value used to indicate UDPRoute resources.
	KindUDPRoute = "UDPRoute"
	// KindTCPRoute is the value used to indicate TCPRoute resources.
	KindTCPRoute = "TCPRoute"
	// KindTLSRoute is the value used to indicate TLSRoute resources.
	KindTLSRoute = "TLSRoute"
	// KindHTTPRoute is the value used to indicate HTTPRoute resources.
	KindHTTPRoute = "HTTPRoute"
	// KindGRPCRoute is the value used to indicate GRPCRoute resources.
	KindGRPCRoute = "GRPCRoute"
	// KindBackendTLSPolicy is the value used to indicate BackendTLSPolicy resources.
	KindBackendTLSPolicy = "BackendTLSPolicy"
	// KindIngress is the value used to indicate Ingress resources.
	KindIngress = "Ingress"
	// KindKongServiceFacade is the value used to indicate KongServiceFacade resources.
	KindKongServiceFacade = "KongServiceFacade"
	// KindKongUpstreamPolicy is the value used to indicate KongUpstreamPolicy resources.
	KindKongUpstreamPolicy = "KongUpstreamPolicy"
	// KindKongLicense is the value used to indicate KongLicense resources.
	KindKongLicense = "KongLicense"
	// KindKongCustomEntity is the value used to indicate KongCustomEntity resources.
	KindKongCustomEntity = "KongCustomEntity"
	// KindKongPlugin is the value used to indicate KongPlugin resources.
	KindKongPlugin = "KongPlugin"
)

const (
	// NetworkingFamily is the label key used to store the networking family of
	// resources that are being tests.
	//
	// Possible, values: "gatewayapi", "ingress".
	NetworkingFamily = "networkingfamily"
	// NetworkingFamilyGatewayAPI is the value used to indicate Gateway API resources.
	NetworkingFamilyGatewayAPI = "gatewayapi"
	// NetworkingFamilyIngress is the value used to indicate Ingress resources.
	NetworkingFamilyIngress = "ingress"
)

const (
	// Example is the label key used to indicate whether the test is testing
	// example manifests.
	Example = "example"
	// ExampleTrue is the value used to indicate that the test is using example manifests.
	ExampleTrue = "true"
)
