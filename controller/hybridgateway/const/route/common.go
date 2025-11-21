package route

// HTTPRouteKey identifies HTTPRoute resources.
const HTTPRouteKey = "HTTPRoute"

// RouteFinalizer is the finalizer added to Route objects to manage cleanup of generated resources.
const RouteFinalizer = "gateway-operator.konghq.com/route-cleanup"
