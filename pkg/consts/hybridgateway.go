package consts

const (
	// ServiceManagedByLabel indicates that an object's lifecycle is managed
	// by the Service controller.
	ServiceManagedByLabel = "service"

	HTTPRouteManagedByLabel = "httproute"

	// HashSpecValueLabel is the label's suffix used to indicate the hash of an object's spec.
	HashSpecValueLabel = "hash-spec"

	// GatewayOperatorHashSpecLabel is the label used to indicate the hash of an object's spec.
	GatewayOperatorHashSpecLabel = OperatorLabelPrefix + HashSpecValueLabel
)

const (
	// HybridGatewaysAnnotation is used to annotate resources created for hybrid gateways,
	// indicating which gateways are associated with the resource.
	HybridGatewaysAnnotation = "hybrid-gateways"

	// GatewayOperatorHybridGatewaysAnnotation is the fully qualified annotation key
	// used to annotate resources created for hybrid gateways, indicating which gateways
	// are associated with the resource.
	GatewayOperatorHybridGatewaysAnnotation = OperatorAnnotationPrefix + HybridGatewaysAnnotation

	// HybridRouteAnnotation is used to annotate resources created for hybrid gateways,
	// indicating which route is associated with the resource.
	HybridRouteAnnotation = "hybrid-routes"

	// GatewayOperatorHybridRoutesAnnotation is the fully qualified annotation key
	// used to annotate resources created for hybrid gateways, indicating which route
	// is associated with the resource.
	GatewayOperatorHybridRoutesAnnotation = OperatorAnnotationPrefix + HybridRouteAnnotation

	// GatewayOperatorHybridGatewaysNameLabel is the fully qualified label key
	// used to label resources created for hybrid gateways, indicating the name of the gateway
	// associated with the resource.
	GatewayOperatorHybridGatewaysNameLabel = OperatorAnnotationPrefix + HybridGatewaysAnnotation + "-name"

	// GatewayOperatorHybridGatewaysNamespaceLabel is the fully qualified label key
	// used to label resources created for hybrid gateways, indicating the namespace of the gateway
	// associated with the resource.
	GatewayOperatorHybridGatewaysNamespaceLabel = OperatorAnnotationPrefix + HybridGatewaysAnnotation + "-namespace"
)
