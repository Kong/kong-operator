package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
)

// AnnotateConfigMapWithKongPluginInstallation ensures that annotation that maps
// particular ConfigMap with KongPluginInstallation based which it's been populated.
// Annotation value is in the form `Namespace/Name` of the KongPluginInstallation.
func AnnotateConfigMapWithKongPluginInstallation(cm *corev1.ConfigMap, kpi operatorv1alpha1.KongPluginInstallation) {
	annotations := cm.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[consts.AnnotationMappedToKongPluginInstallation] = client.ObjectKeyFromObject(&kpi).String()
	cm.SetAnnotations(annotations)
}

// AnnotatePodTemplateSpecHash sets the hash of the PodTemplateSpec in the Deployment annotations.
func AnnotatePodTemplateSpecHash(
	deployment *appsv1.Deployment,
	pts *corev1.PodTemplateSpec,
) error {
	// After all the patches are applied, calculate the hash of the PodTemplateSpec
	// and store it in the Deployment annotations.
	// This will allow us to detect changes to the PodTemplateSpec and enforce them.
	hashSpec, err := CalculateHash(pts)
	if err != nil {
		return fmt.Errorf("failed to calculate hash spec from DataPlane: %w", err)
	}
	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	deployment.Annotations[consts.AnnotationPodTemplateSpecHash] = hashSpec
	return nil
}
