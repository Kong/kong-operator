package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// Service generators
// -----------------------------------------------------------------------------

// GenerateNewServiceForCertificateConfig is a helper to generate a service
// to expose the operator webhook
func GenerateNewServiceForCertificateConfig(namespace, name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "webhook",
					Port:       443,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(9443),
				},
			},
			Selector: map[string]string{
				"control-plane": "controller-manager",
			},
		},
	}
}

// GenerateNewProxyServiceForDataplane is a helper to generate the dataplane proxy service
func GenerateNewProxyServiceForDataplane(dataplane *operatorv1beta1.DataPlane) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: fmt.Sprintf("%s-proxy-%s-", consts.DataPlanePrefix, dataplane.Name),
			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneProxyServiceLabelValue),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     getDataPlaneIngressServiceType(dataplane),
			Selector: map[string]string{"app": dataplane.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       consts.DefaultHTTPPort,
					TargetPort: intstr.FromInt(consts.DataPlaneProxyPort),
				},
				{
					Name:       "https",
					Protocol:   corev1.ProtocolTCP,
					Port:       consts.DefaultHTTPSPort,
					TargetPort: intstr.FromInt(consts.DataPlaneProxySSLPort),
				},
			},
		},
	}
}

const DefaultDataPlaneProxyServiceType = corev1.ServiceTypeLoadBalancer

func getDataPlaneIngressServiceType(dataplane *operatorv1beta1.DataPlane) corev1.ServiceType {
	if dataplane == nil || dataplane.Spec.Network.Services == nil {
		return DefaultDataPlaneProxyServiceType
	}

	return dataplane.Spec.Network.Services.Ingress.Type
}

// GenerateNewAdminServiceForDataPlane is a helper to generate the headless dataplane admin service
func GenerateNewAdminServiceForDataPlane(dataplane *operatorv1beta1.DataPlane) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: fmt.Sprintf("%s-admin-%s-", consts.DataPlanePrefix, dataplane.Name),
			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  map[string]string{"app": dataplane.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "admin",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(consts.DataPlaneAdminAPIPort),
					TargetPort: intstr.FromInt(consts.DataPlaneAdminAPIPort),
				},
			},
		},
	}
}
