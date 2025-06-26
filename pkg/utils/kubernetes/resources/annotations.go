package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/pkg/consts"

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

// AnnotateObjWithHash sets the hash of the provided toHash object in the provided
// obj's annotations.
func AnnotateObjWithHash[T any](
	obj client.Object,
	toHash T,
) error {
	hash, err := CalculateHash(toHash)
	if err != nil {
		return fmt.Errorf("failed to calculate hash spec from %T: %w", toHash, err)
	}
	anns := obj.GetAnnotations()
	if anns == nil {
		anns = make(map[string]string)
	}
	anns[consts.AnnotationSpecHash] = hash
	obj.SetAnnotations(anns)

	return nil
}
