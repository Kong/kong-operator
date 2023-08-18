package test

import (
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/pkg/vars"
)

// GenerateGatewayClass generates the default GatewayClass to be used in tests
func GenerateGatewayClass() *gatewayv1beta1.GatewayClass {
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
	}
	return gatewayClass
}

// GenerateGateway generates a Gateway to be used in tests
func GenerateGateway(gatewayNSN types.NamespacedName, gatewayClass *gatewayv1beta1.GatewayClass) *gwtypes.Gateway {
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gatewayNSN.Namespace,
			Name:      gatewayNSN.Name,
		},
		Spec: gatewayv1beta1.GatewaySpec{
			GatewayClassName: gatewayv1beta1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1beta1.Listener{{
				Name:     "http",
				Protocol: gatewayv1beta1.HTTPProtocolType,
				Port:     gatewayv1beta1.PortNumber(80),
			}},
		},
	}
	return gateway
}

// GenerateGatewayConfiguration generates a GatewayConfiguration to be used in tests
func GenerateGatewayConfiguration(gatewayConfigurationNSN types.NamespacedName) *operatorv1alpha1.GatewayConfiguration {
	return &operatorv1alpha1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: gatewayConfigurationNSN.Namespace,
			Name:      gatewayConfigurationNSN.Name,
		},
		Spec: operatorv1alpha1.GatewayConfigurationSpec{
			ControlPlaneOptions: &operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
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
								},
							},
						},
					},
				},
			},
			DataPlaneOptions: &operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
										ReadinessProbe: &corev1.Probe{
											FailureThreshold:    3,
											InitialDelaySeconds: 0,
											PeriodSeconds:       1,
											SuccessThreshold:    1,
											TimeoutSeconds:      1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/status",
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
}
