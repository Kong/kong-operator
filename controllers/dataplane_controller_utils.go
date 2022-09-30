package controllers

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

func generateDataPlaneImage(dataplane *operatorv1alpha1.DataPlane) string {
	if dataplane.Spec.ContainerImage != nil {
		dataplaneImage := *dataplane.Spec.ContainerImage
		if dataplane.Spec.Version != nil {
			dataplaneImage = fmt.Sprintf("%s:%s", dataplaneImage, *dataplane.Spec.Version)
		}
		return dataplaneImage
	}

	if relatedKongImage := os.Getenv("RELATED_IMAGE_KONG"); relatedKongImage != "" {
		// RELATED_IMAGE_KONG is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongImage
	}

	return consts.DefaultDataPlaneImage // TODO: https://github.com/Kong/gateway-operator/issues/20

}

func generateNewServiceForDataplane(dataplane *operatorv1alpha1.DataPlane) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    dataplane.Namespace,
			GenerateName: fmt.Sprintf("%s-%s-", consts.DataPlanePrefix, dataplane.Name),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{"app": dataplane.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       dataplaneutils.DefaultHTTPPort,
					TargetPort: intstr.FromInt(dataplaneutils.DefaultKongHTTPPort),
				},
				{
					Name:       "https",
					Protocol:   corev1.ProtocolTCP,
					Port:       dataplaneutils.DefaultHTTPSPort,
					TargetPort: intstr.FromInt(dataplaneutils.DefaultKongHTTPSPort),
				},
				{
					Name:     "admin",
					Protocol: corev1.ProtocolTCP,
					Port:     dataplaneutils.DefaultKongAdminPort,
				},
			},
		},
	}
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func addLabelForDataplane(obj client.Object) {
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
