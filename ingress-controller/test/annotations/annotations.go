package annotations

import internal "github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"

const (
	IngressClassKey                                 = internal.IngressClassKey
	AnnotationPrefix                                = internal.AnnotationPrefix
	GatewayClassUnmanagedKey                        = internal.GatewayClassUnmanagedKey
	GatewayClassUnmanagedAnnotationValuePlaceholder = internal.GatewayClassUnmanagedAnnotationValuePlaceholder
	DefaultIngressClass                             = internal.DefaultIngressClass
)

var GatewayClassUnmanagedAnnotation = internal.GatewayClassUnmanagedAnnotation
