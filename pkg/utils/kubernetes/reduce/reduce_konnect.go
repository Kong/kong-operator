package reduce

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=delete

// HPAFilterFunc filters a list of HorizontalPodAutoscalers and returns the ones that should be deleted.
type KongDataPlaneClientCertificateFilterFunc func([]configurationv1alpha1.KongDataPlaneClientCertificate) []configurationv1alpha1.KongDataPlaneClientCertificate

// ReduceKongDataPlaneClientCertificates detects the best KongDataPlaneClientCertificate in the set and deletes all the others.
func ReduceKongDataPlaneClientCertificates(ctx context.Context, k8sClient client.Client, certs []configurationv1alpha1.KongDataPlaneClientCertificate, filter KongDataPlaneClientCertificateFilterFunc) error {
	for _, hpa := range filter(certs) {
		if err := k8sClient.Delete(ctx, &hpa); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
