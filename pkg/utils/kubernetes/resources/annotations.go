package resources

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/pkg/consts"
)

// AnnotateConfigMapWithKongPluginInstallation ensures that annotation that maps
// particular ConfigMap with KongPluginInstallation based which it's been populated.
// Annotation value is in the form `Namespace/Name` of the KongPluginInstallation.
func AnnotateConfigMapWithKongPluginInstallation(cm *corev1.ConfigMap, kpi v1alpha1.KongPluginInstallation) {
	annotations := cm.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[consts.AnnotationMappedToKongPluginInstallation] = client.ObjectKeyFromObject(&kpi).String()
	cm.SetAnnotations(annotations)
}
