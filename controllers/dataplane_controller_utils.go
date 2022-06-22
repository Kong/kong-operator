package controllers

import (
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Defaulters
// -----------------------------------------------------------------------------

const (
	defaultHTTPPort  = 80
	defaultHTTPSPort = 443

	defaultKongHTTPPort   = 8000
	defaultKongHTTPSPort  = 8443
	defaultKongAdminPort  = 8444
	defaultKongStatusPort = 8100
)

var dataplaneDefaults = map[string]string{
	"KONG_ADMIN_ACCESS_LOG":       "/dev/stdout",
	"KONG_ADMIN_ERROR_LOG":        "/dev/stderr",
	"KONG_ADMIN_GUI_ACCESS_LOG":   "/dev/stdout",
	"KONG_ADMIN_GUI_ERROR_LOG":    "/dev/stderr",
	"KONG_CLUSTER_LISTEN":         "off",
	"KONG_DATABASE":               "off",
	"KONG_NGINX_WORKER_PROCESSES": "2",
	"KONG_PLUGINS":                "bundled",
	"KONG_PORTAL_API_ACCESS_LOG":  "/dev/stdout",
	"KONG_PORTAL_API_ERROR_LOG":   "/dev/stderr",
	"KONG_PORT_MAPS":              "80:8000, 443:8443",
	"KONG_PROXY_ACCESS_LOG":       "/dev/stdout",
	"KONG_PROXY_ERROR_LOG":        "/dev/stderr",
	"KONG_PROXY_LISTEN":           fmt.Sprintf("0.0.0.0:%d reuseport backlog=16384, 0.0.0.0:%d http2 ssl reuseport backlog=16384", defaultKongHTTPPort, defaultKongHTTPSPort),
	"KONG_STATUS_LISTEN":          fmt.Sprintf("0.0.0.0:%d", defaultKongStatusPort),

	// TODO: reconfigure following https://github.com/Kong/gateway-operator/issues/7
	"KONG_ADMIN_LISTEN": fmt.Sprintf("0.0.0.0:%d http2 ssl reuseport backlog=16384", defaultKongAdminPort),
}

func setDataPlaneDefaults(spec *operatorv1alpha1.DataPlaneDeploymentOptions, dontOverride map[string]struct{}) {
	for k, v := range dataplaneDefaults {
		if _, isOverrideDisabled := dontOverride[k]; !isOverrideDisabled {
			spec.Env = append(spec.Env, corev1.EnvVar{Name: k, Value: v})
		}
	}
	sort.Sort(envWrapper(spec.Env))
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

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
// DataPlane - Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func labelObjForDataplane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1alpha1.DataPlaneDeploymentOptions) bool {
	return deploymentOptionsDeepEqual(&spec1.DeploymentOptions, &spec2.DeploymentOptions)
}
