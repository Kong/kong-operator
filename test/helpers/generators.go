package helpers

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
)

// -----------------------------------------------------------------------------
// Kubernetes resources generators
// -----------------------------------------------------------------------------

// This is copy-pasted from the test package in the kong/kubernetes-ingress-controller repo
// https://github.com/Kong/kubernetes-ingress-controller/blob/main/test/integration/tlsroute_test.go#L782
func GenerateTLSEchoContainer(tpcEchoImage string, tlsEchoPort int32, sendMsg string, tlsSecretName string) corev1.Container { //nolint:unparam
	container := generators.NewContainer("tcpecho-"+sendMsg, tpcEchoImage, tlsEchoPort)
	const tlsCertDir = "/var/run/certs"
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "POD_NAME",
			Value: sendMsg,
		},
		corev1.EnvVar{
			Name:  "TLS_PORT",
			Value: fmt.Sprint(tlsEchoPort),
		},
		corev1.EnvVar{
			Name:  "TLS_CERT_FILE",
			Value: tlsCertDir + "/tls.crt",
		},
		corev1.EnvVar{
			Name:  "TLS_KEY_FILE",
			Value: tlsCertDir + "/tls.key",
		},
	)
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      tlsSecretName,
		ReadOnly:  true,
		MountPath: tlsCertDir,
	})
	return container
}

// -----------------------------------------------------------------------------
// Gateway Operator generators
// -----------------------------------------------------------------------------

// GenerateGatewayConfiguration generates a GatewayConfiguration to be used in tests
func GenerateGatewayConfiguration(namespace string) *operatorv1beta1.GatewayConfiguration {
	return &operatorv1beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1beta1.GatewayConfigurationSpec{
			ControlPlaneOptions: &operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  consts.ControlPlaneControllerContainerName,
									Image: consts.DefaultControlPlaneImage,
									ReadinessProbe: &corev1.Probe{
										FailureThreshold:    3,
										InitialDelaySeconds: 0,
										PeriodSeconds:       1,
										SuccessThreshold:    1,
										TimeoutSeconds:      1,
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Path:   "/readyz",
												Port:   intstr.FromInt(10254),
												Scheme: corev1.URISchemeHTTP,
											},
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  "CONTROLLER_LOG_LEVEL",
											Value: "trace",
										},
									},
								},
							},
						},
					},
				},
			},
			DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											FailureThreshold:    3,
											InitialDelaySeconds: 0,
											PeriodSeconds:       1,
											SuccessThreshold:    1,
											TimeoutSeconds:      1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/status/ready",
													Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
													Scheme: corev1.URISchemeHTTP,
												},
											},
										},
										Env: []corev1.EnvVar{
											{
												Name:  "KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT",
												Value: "off",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// -----------------------------------------------------------------------------
// Gateway API generators
// -----------------------------------------------------------------------------

// MustGenerateGatewayClass generates the default GatewayClass to be used in tests
func MustGenerateGatewayClass(t *testing.T, parametersRefs ...gatewayv1.ParametersReference) *gatewayv1.GatewayClass {
	t.Helper()

	if len(parametersRefs) > 1 {
		require.Fail(t, "only one ParametersReference is allowed")
	}
	var parametersRef *gatewayv1.ParametersReference
	if len(parametersRefs) == 1 {
		parametersRef = &parametersRefs[0]
	}
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
			ParametersRef:  parametersRef,
		},
	}
	return gatewayClass
}

// GenerateGateway generates a Gateway to be used in tests
func GenerateGateway(gatewayNSN types.NamespacedName, gatewayClass *gatewayv1.GatewayClass, opts ...func(gateway *gatewayv1.Gateway)) *gwtypes.Gateway {
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gatewayNSN.Namespace,
			Name:      gatewayNSN.Name,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1.Listener{{
				Name:     "http",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(80),
			}},
		},
	}

	for _, opt := range opts {
		opt(gateway)
	}

	return gateway
}

// GenerateHTTPRoute generates an HTTPRoute to be used in tests
func GenerateHTTPRoute(namespace string, gatewayName, serviceName string, servicePort int32, opts ...func(*gatewayv1.HTTPRoute)) *gatewayv1.HTTPRoute {
	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gatewayName),
				}},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  lo.ToPtr(gatewayv1.PathMatchPathPrefix),
								Value: lo.ToPtr("/test"),
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: gatewayv1.ObjectName(serviceName),
									Port: lo.ToPtr(gatewayv1.PortNumber(servicePort)),
									Kind: lo.ToPtr(gatewayv1.Kind("Service")),
								},
							},
						},
					},
				},
			},
		},
	}

	for _, opt := range opts {
		opt(httpRoute)
	}

	return httpRoute
}

// GenerateTLSRoute generates a TLSRoute to be used in tests
func GenerateTLSRoute(namespace string, gatewayName, serviceName string, servicePort int32, opts ...func(*gatewayv1alpha2.TLSRoute)) *gatewayv1alpha2.TLSRoute {
	tlsRoute := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      uuid.NewString(),
			Annotations: map[string]string{
				"konghq.com/strip-path": "true",
			},
		},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Name: gatewayv1.ObjectName(gatewayName),
				}},
			},
			Rules: []gatewayv1alpha2.TLSRouteRule{
				{
					BackendRefs: []gatewayv1alpha2.BackendRef{
						{
							BackendObjectReference: gatewayv1alpha2.BackendObjectReference{
								Name: gatewayv1alpha2.ObjectName(serviceName),
								Port: lo.ToPtr(gatewayv1alpha2.PortNumber(servicePort)),
								Kind: lo.ToPtr(gatewayv1alpha2.Kind("Service")),
							},
						},
					},
				},
			},
		},
	}

	for _, opt := range opts {
		opt(tlsRoute)
	}

	return tlsRoute
}
