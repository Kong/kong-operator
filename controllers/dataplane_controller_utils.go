package controllers

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Private Functions - Generators
// -----------------------------------------------------------------------------

const (
	defaultHTTPPort  = 80
	defaultHTTPSPort = 443

	defaultKongHTTPPort   = 8000
	defaultKongHTTPSPort  = 8443
	defaultKongAdminPort  = 8444
	defaultKongStatusPort = 8100
)

func setDataPlaneDefaults(dataplane *operatorv1alpha1.DataPlane) {
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_ADMIN_ACCESS_LOG", Value: "/dev/stdout"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_ADMIN_ERROR_LOG", Value: "/dev/stderr"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_ADMIN_GUI_ACCESS_LOG", Value: "/dev/stdout"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_ADMIN_GUI_ERROR_LOG", Value: "/dev/stderr"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_ADMIN_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d ssl", defaultKongAdminPort)})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_CLUSTER_LISTEN", Value: "off"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_DATABASE", Value: "off"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_LUA_PACKAGE_PATH", Value: "/opt/?.lua;/opt/?/init.lua;;"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_NGINX_WORKER_PROCESSES", Value: "2"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PLUGINS", Value: "bundled"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PORTAL_API_ACCESS_LOG", Value: "/dev/stdout"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PORTAL_API_ERROR_LOG", Value: "/dev/stderr"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PORT_MAPS", Value: "80:8000, 443:8443"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PROXY_ACCESS_LOG", Value: "/dev/stdout"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PROXY_ERROR_LOG", Value: "/dev/stderr"})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_PROXY_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d, 0.0.0.0:%d http2 ssl", defaultKongHTTPPort, defaultKongHTTPSPort)})
	dataplane.Spec.Env = append(dataplane.Spec.Env, corev1.EnvVar{Name: "KONG_STATUS_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d", defaultKongStatusPort)})
}

func generateNewDeploymentForDataPlane(dataplane *operatorv1alpha1.DataPlane) *appsv1.Deployment {
	var dataplaneImage string
	if dataplane.Spec.ContainerImage != nil {
		dataplaneImage = *dataplane.Spec.ContainerImage
		if dataplane.Spec.Version != nil {
			dataplaneImage = fmt.Sprintf("%s:%s", dataplaneImage, *dataplane.Spec.Version)
		}
	} else {
		dataplaneImage = consts.DefaultDataPlaneImage // TODO: https://github.com/Kong/gateway-operator/issues/20
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplane.Namespace,
			Name:      dataplane.Name, // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
			Labels: map[string]string{
				"app": dataplane.Name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": dataplane.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": dataplane.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "proxy",
						Env:             dataplane.Spec.Env,
						EnvFrom:         dataplane.Spec.EnvFrom,
						Image:           dataplaneImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Lifecycle: &corev1.Lifecycle{
							PreStop: &corev1.LifecycleHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"/bin/sh",
										"-c",
										"kong quit",
									},
								},
							},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "proxy",
								ContainerPort: 8000,
								Protocol:      corev1.ProtocolTCP,
							},
							{
								Name:          "proxy-ssl",
								ContainerPort: 8443,
								Protocol:      corev1.ProtocolTCP,
							},
							{
								Name:          "metrics",
								ContainerPort: 8100,
								Protocol:      corev1.ProtocolTCP,
							},
							{
								Name:          "admin-ssl",
								ContainerPort: 8444,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						ReadinessProbe: &corev1.Probe{
							FailureThreshold:    3,
							InitialDelaySeconds: 5,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      1,
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path:   "/status",
									Port:   intstr.FromInt(8100),
									Scheme: corev1.URISchemeHTTP,
								},
							},
						},
					}},
				},
			},
		},
	}
	return deployment
}

func generateNewServiceForDataplane(dataplane *operatorv1alpha1.DataPlane) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       dataplane.Namespace,
			Name:            "svc-" + dataplane.Name, // TODO: generated names https://github.com/Kong/gateway-operator/issues/21
			OwnerReferences: []metav1.OwnerReference{createObjectOwnerRef(dataplane)},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{"app": dataplane.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       defaultHTTPPort,
					TargetPort: intstr.FromInt(defaultKongHTTPPort),
				},
				{
					Name:       "https",
					Protocol:   corev1.ProtocolTCP,
					Port:       defaultHTTPSPort,
					TargetPort: intstr.FromInt(defaultKongHTTPSPort),
				},
				{
					Name:     "admin",
					Protocol: corev1.ProtocolTCP,
					Port:     defaultKongAdminPort,
				},
			},
		},
	}
}

// -----------------------------------------------------------------------------
// Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func labelObjForDataplane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}
