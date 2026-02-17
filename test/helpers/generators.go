package helpers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

// MustGenerateGatewayClass generates the default GatewayClass to be used in tests.
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

// GenerateGateway generates a Gateway to be used in tests.
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

type gatewayConfigurationOption func(*operatorv2beta1.GatewayConfiguration)

// GenerateGatewayConfiguration generates a GatewayConfiguration to be used in tests.
func GenerateGatewayConfiguration(namespace string, opts ...gatewayConfigurationOption) *operatorv2beta1.GatewayConfiguration {
	gwc := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      uuid.NewString(),
		},
		Spec: operatorv2beta1.GatewayConfigurationSpec{
			// TODO(pmalek): add support for ControlPlane optionns using GatewayConfiguration v2
			// https://github.com/kong/kong-operator/issues/1728

			DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv2beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											FailureThreshold:    3,
											InitialDelaySeconds: 1,
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
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(gwc)
	}
	return gwc
}

// GenerateHTTPRoute generates an HTTPRoute to be used in tests.
func GenerateHTTPRoute(namespace string, gatewayName, serviceName string, opts ...func(*gatewayv1.HTTPRoute)) *gatewayv1.HTTPRoute {
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
									Port: lo.ToPtr(gatewayv1.PortNumber(80)),
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
