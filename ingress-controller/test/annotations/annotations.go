package annotations

import internal "github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"

const (
	IngressClassKey                                 = internal.IngressClassKey
	AnnotationPrefix                                = internal.AnnotationPrefix
	GatewayClassUnmanagedKey                        = internal.GatewayClassUnmanagedKey
	GatewayClassUnmanagedAnnotationValuePlaceholder = internal.GatewayClassUnmanagedAnnotationValuePlaceholder
	DefaultIngressClass                             = internal.DefaultIngressClass
	PluginsKey                                      = internal.PluginsKey
	StripPathKey                                    = internal.StripPathKey
	ProtocolKey                                     = internal.ProtocolKey
	ProtocolsKey                                    = internal.ProtocolsKey
	PathKey                                         = internal.PathKey
	HostHeaderKey                                   = internal.HostHeaderKey
	RewriteURIKey                                   = internal.RewriteURIKey
	TLSVerifyKey                                    = internal.TLSVerifyKey
	TLSVerifyDepthKey                               = internal.TLSVerifyDepthKey
	CACertificatesSecretsKey                        = internal.CACertificatesSecretsKey
)

var GatewayClassUnmanagedAnnotation = internal.GatewayClassUnmanagedAnnotation

func ExtractGatewayPublishService(anns map[string]string) []string {
	return internal.ExtractGatewayPublishService(anns)
}
