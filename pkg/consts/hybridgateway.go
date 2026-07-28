package consts

const (
	// ServiceManagedByLabel indicates that an object's lifecycle is managed
	// by the Service controller.
	ServiceManagedByLabel = "service"

	// HTTPRouteManagedByLabel indicates that an object's lifecycle is managed
	// by the HTTPRoute controller.
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

	// HybridRouteHTTPRouteAnnotation is used to annotate resources created for hybrid gateways,
	// indicating which HTTPRoute is associated with the resource.
	HybridRouteHTTPRouteAnnotation = "hybrid-routes"

	// HybridRouteTLSRouteAnnotation is used to annotate resources created for hybrid gateways,
	// indicating which TLSRoutes are associated with the resource.
	HybridRouteTLSRouteAnnotation = "hybrid-routes-TLSRoute"

	// HybridRouteTCPRouteAnnotation is used to annotate resources created for hybrid gateways,
	// indicating which TCPRoutes are associated with the resource.
	HybridRouteTCPRouteAnnotation = "hybrid-routes-TCPRoute"

	// GatewayOperatorHybridRoutesHTTPRouteAnnotation is the fully qualified annotation key
	// used to annotate resources created for hybrid gateways, indicating which HTTPRoute
	// is associated with the resource.
	GatewayOperatorHybridRoutesHTTPRouteAnnotation = OperatorAnnotationPrefix + HybridRouteHTTPRouteAnnotation

	// GatewayOperatorHybridRoutesTLSRouteAnnotation is the fully qualified annotation key
	// used to annotate resources created for hybrid gateways, indicating which TLSRoute
	// is associated with the resource.
	GatewayOperatorHybridRoutesTLSRouteAnnotation = OperatorAnnotationPrefix + HybridRouteTLSRouteAnnotation

	// GatewayOperatorHybridRoutesTCPRouteAnnotation is the fully qualified annotation key
	// used to annotate resources created for hybrid gateways, indicating which TCPRoute
	// is associated with the resource.
	GatewayOperatorHybridRoutesTCPRouteAnnotation = OperatorAnnotationPrefix + HybridRouteTCPRouteAnnotation

	// GatewayOperatorHybridListenerPortLabel is the fully qualified label key
	// used to label resources created for hybrid gateways, indicating the listener port
	// associated with the resource.
	GatewayOperatorHybridListenerPortLabel = OperatorAnnotationPrefix + "listener-port"
)
