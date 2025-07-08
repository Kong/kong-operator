package resources

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	pkgapiscorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// -----------------------------------------------------------------------------
// Service generators
// -----------------------------------------------------------------------------

// GenerateNewIngressServiceForDataPlane is a helper to generate the dataplane ingress service
func GenerateNewIngressServiceForDataPlane(dataplane *operatorv1beta1.DataPlane, opts ...ServiceOpt) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplane.Namespace,

			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneIngressServiceLabelValue),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: getDataPlaneIngressServiceType(dataplane),
			Selector: map[string]string{
				"app": dataplane.Name,
			},
			Ports: DefaultDataPlaneIngressServicePorts,
		},
	}

	// Assign the service name if the DataPlane specifies name of ingress service.
	if serviceName := GetDataPlaneIngressServiceName(dataplane); serviceName != "" {
		svc.Name = serviceName
	} else {
		// If the service name is not specified, use the generated name.
		svc.GenerateName = k8sutils.TrimGenerateName(fmt.Sprintf("%s-ingress-%s-", consts.DataPlanePrefix, dataplane.Name))
	}

	setDataPlaneIngressServiceExternalTrafficPolicy(dataplane, svc)
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
	controllerutil.AddFinalizer(svc, consts.DataPlaneOwnedWaitForOwnerFinalizer)

	return svc, nil
}

// DefaultDataPlaneIngressServiceType is the default Service type for a DataPlane.
const DefaultDataPlaneIngressServiceType = corev1.ServiceTypeLoadBalancer

// DefaultDataPlaneIngressServicePorts returns the default ServicePorts for a DataPlane.
var DefaultDataPlaneIngressServicePorts = []corev1.ServicePort{
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
}

func getDataPlaneIngressServiceType(dataplane *operatorv1beta1.DataPlane) corev1.ServiceType {
	if dataplane == nil || dataplane.Spec.Network.Services == nil {
		return DefaultDataPlaneIngressServiceType
	}

	return dataplane.Spec.Network.Services.Ingress.Type
}

func setDataPlaneIngressServiceExternalTrafficPolicy(
	dataplane *operatorv1beta1.DataPlane,
	svc *corev1.Service,
) {
	if dataplane == nil ||
		dataplane.Spec.Network.Services == nil ||
		dataplane.Spec.Network.Services.Ingress == nil ||
		dataplane.Spec.Network.Services.Ingress.ExternalTrafficPolicy == "" {
		return
	}

	svc.Spec.ExternalTrafficPolicy = dataplane.Spec.Network.Services.Ingress.ExternalTrafficPolicy
}

// ServiceOpt is an option function for a Service.
type ServiceOpt func(*corev1.Service)

// ServiceWithLabel adds a label to a Service.
func ServiceWithLabel(k, v string) ServiceOpt {
	return func(s *corev1.Service) {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		s.Labels[k] = v
	}
}

// LabelSelectorFromDataPlaneStatusSelectorServiceOpt returns a ServiceOpt function
// which will set Service's selector based on provided DataPlane's Status selector
// field.
func LabelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane *operatorv1beta1.DataPlane) ServiceOpt {
	return func(s *corev1.Service) {
		if dataplane.Status.Selector != "" {
			s.Spec.Selector[consts.OperatorLabelSelector] = dataplane.Status.Selector
		}
	}
}

// ServicePortsFromDataPlaneIngressOpt is a helper to translate the DataPlane service ports
// field into actual service ports.
func ServicePortsFromDataPlaneIngressOpt(dataplane *operatorv1beta1.DataPlane) ServiceOpt {
	return func(service *corev1.Service) {
		if dataplane.Spec.Network.Services == nil ||
			dataplane.Spec.Network.Services.Ingress == nil ||
			len(dataplane.Spec.Network.Services.Ingress.Ports) == 0 {
			return
		}
		newPorts := make([]corev1.ServicePort, 0, len(dataplane.Spec.Network.Services.Ingress.Ports))
		alreadyUsedPorts := make(map[int32]struct{})
		for _, p := range dataplane.Spec.Network.Services.Ingress.Ports {
			targetPort := intstr.FromInt(consts.DataPlaneProxyPort)
			if !cmp.Equal(p.TargetPort, intstr.IntOrString{}) {
				targetPort = p.TargetPort
			}
			if _, ok := alreadyUsedPorts[p.Port]; !ok {
				newPorts = append(newPorts, corev1.ServicePort{
					Name:       p.Name,
					Protocol:   corev1.ProtocolTCP, // Currently, only TCP protocol supported.
					Port:       p.Port,
					TargetPort: targetPort,
					NodePort:   p.NodePort,
				})
				alreadyUsedPorts[p.Port] = struct{}{}
			}
		}
		service.Spec.Ports = newPorts
	}
}

// GenerateNewAdminServiceForDataPlane is a helper to generate the headless dataplane admin service
func GenerateNewAdminServiceForDataPlane(dataplane *operatorv1beta1.DataPlane, opts ...ServiceOpt) (*corev1.Service, error) {
	adminService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: k8sutils.TrimGenerateName(dataPlaneAdminServiceGenerateName(dataplane)),
			Labels: map[string]string{
				"app":                            dataplane.Name,
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneAdminServiceLabelValue),
			},
		},
		Spec: corev1.ServiceSpec{
			// Headless service, since endpoints for Kong Gateway Admin API are derived from EndpointSlices.
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
			// to instantiate the connection with the dataplane to become ready, and the dataplane
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
	controllerutil.AddFinalizer(adminService, consts.DataPlaneOwnedWaitForOwnerFinalizer)
	return adminService, nil
}

func getSelectorOverrides(overrideAnnotation string) (map[string]string, error) {
	if overrideAnnotation == "" {
		return nil, errors.New("selector override empty - expected format: key1=value,key2=value2")
	}

	selector := make(map[string]string)
	for o := range strings.SplitSeq(overrideAnnotation, ",") {
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

// GenerateNewAdmissionWebhookServiceForControlPlane is a helper to generate the admission webhook service for a control
// plane.
func GenerateNewAdmissionWebhookServiceForControlPlane(cp *gwtypes.ControlPlane) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cp.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-webhook-%s-", consts.ControlPlanePrefix, cp.Name)),
			Labels: map[string]string{
				"app":                           cp.Name,
				consts.ControlPlaneServiceLabel: consts.ControlPlaneServiceKindWebhook,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": cp.Name},
			Ports: []corev1.ServicePort{
				{
					Name:     "webhook",
					Protocol: corev1.ProtocolTCP,
					Port:     consts.ControlPlaneAdmissionWebhookListenPort,
				},
			},
		},
	}
	pkgapiscorev1.SetDefaults_Service(svc)
	LabelObjectAsControlPlaneManaged(svc)
	k8sutils.SetOwnerForObject(svc, cp)

	return svc, nil
}

// GetDataPlaneIngressServiceName fetches the specified name of ingress service of dataplane.
// If the service name is not specified, it returns an empty string.
func GetDataPlaneIngressServiceName(dataPlane *operatorv1beta1.DataPlane) string {
	if dataPlane == nil {
		return ""
	}
	dpServices := dataPlane.Spec.Network.Services
	if dpServices == nil || dpServices.Ingress == nil || dpServices.Ingress.Name == nil {
		return ""
	}
	return *dpServices.Ingress.Name
}
