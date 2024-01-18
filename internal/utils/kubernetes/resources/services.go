package resources

import (
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
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

// GenerateNewIngressServiceForDataplane is a helper to generate the dataplane ingress service
func GenerateNewIngressServiceForDataplane(dataplane *operatorv1beta1.DataPlane, opts ...ServiceOpt) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: fmt.Sprintf("%s-ingress-%s-", consts.DataPlanePrefix, dataplane.Name),
			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneIngressServiceLabelValue),
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
	LabelObjectAsDataPlaneManaged(svc)

	for _, opt := range opts {
		opt(svc)
	}

	if selectorOverride, ok := dataplane.Annotations[consts.ServiceSelectorOverrideAnnotation]; ok {
		newSelector, err := getSelectorOverrides(selectorOverride)
		if err != nil {
			return nil, err
		}
		svc.Spec.Selector = newSelector
	}

	k8sutils.SetOwnerForObject(svc, dataplane)
	k8sutils.EnsureFinalizersInMetadata(&svc.ObjectMeta, consts.DataPlaneOwnedWaitForOwnerFinalizer)

	return svc, nil
}

const DefaultDataPlaneIngressServiceType = corev1.ServiceTypeLoadBalancer

func getDataPlaneIngressServiceType(dataplane *operatorv1beta1.DataPlane) corev1.ServiceType {
	if dataplane == nil || dataplane.Spec.Network.Services == nil {
		return DefaultDataPlaneIngressServiceType
	}

	return dataplane.Spec.Network.Services.Ingress.Type
}

type ServiceOpt func(*corev1.Service)

func ServiceWithLabel(k, v string) func(s *corev1.Service) {
	return func(s *corev1.Service) {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		s.Labels[k] = v
	}
}

// GenerateNewAdminServiceForDataPlane is a helper to generate the headless dataplane admin service
func GenerateNewAdminServiceForDataPlane(dataplane *operatorv1beta1.DataPlane, opts ...ServiceOpt) (*corev1.Service, error) {
	adminService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: dataPlaneAdminServiceGenerateName(dataplane),
			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				"app": dataplane.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       consts.DataPlaneAdminServicePortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(consts.DataPlaneAdminAPIPort),
					TargetPort: intstr.FromInt(consts.DataPlaneAdminAPIPort),
				},
			},
			// We need to set the field PublishNotReadyAddresses for a chicken-egg problem
			// in the context of the managed gateways. In that scenario, the controlplane needs
			// to istantiate the connection with the dataplane to become ready, and the dataplane
			// waits for a controlplane configuration to become ready. For this reason, we need
			// the dataplane admin endpoints to be created even if the dataplane pod is not running,
			// so that the controlplane can push the configuration to the dataplane and the pods
			// can become ready.
			PublishNotReadyAddresses: true,
		},
	}
	LabelObjectAsDataPlaneManaged(adminService)

	for _, opt := range opts {
		opt(adminService)
	}

	// Service selector override via annotation takes precedence over provided options.
	if selectorOverride, ok := dataplane.Annotations[consts.ServiceSelectorOverrideAnnotation]; ok {
		newSelector, err := getSelectorOverrides(selectorOverride)
		if err != nil {
			return nil, err
		}
		adminService.Spec.Selector = newSelector
	}

	k8sutils.SetOwnerForObject(adminService, dataplane)
	k8sutils.EnsureFinalizersInMetadata(&adminService.ObjectMeta, consts.DataPlaneOwnedWaitForOwnerFinalizer)
	return adminService, nil
}

func getSelectorOverrides(overrideAnnotation string) (map[string]string, error) {
	if overrideAnnotation == "" {
		return nil, errors.New("selector override empty - expected format: key1=value,key2=value2")
	}

	selector := make(map[string]string)
	overrides := strings.Split(overrideAnnotation, ",")
	for _, o := range overrides {
		annotationParts := strings.Split(o, "=")
		if len(annotationParts) != 2 || annotationParts[0] == "" || annotationParts[1] == "" {
			return nil, errors.New("selector override malformed - expected format: key1=value,key2=value2")
		}
		selector[annotationParts[0]] = annotationParts[1]
	}
	return selector, nil
}

func dataPlaneAdminServiceGenerateName(dataplane *operatorv1beta1.DataPlane) string {
	return fmt.Sprintf("%s-admin-%s-", consts.DataPlanePrefix, dataplane.Name)
}
