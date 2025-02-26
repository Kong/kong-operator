package kubernetes

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// ListHPAsForOwner is a helper function which gets a list of HorizontalPodAutoscalers
// using the provided list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListKongDataPlaneClientCertificateForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]configurationv1alpha1.KongDataPlaneClientCertificate, error) {
	dpClientCertList := &configurationv1alpha1.KongDataPlaneClientCertificateList{}

	err := c.List(
		ctx,
		dpClientCertList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	dpClientCerts := make([]configurationv1alpha1.KongDataPlaneClientCertificate, 0)
	for _, hpa := range dpClientCertList.Items {
		if IsOwnedByRefUID(&hpa, uid) {
			dpClientCerts = append(dpClientCerts, hpa)
		}
	}

	return dpClientCerts, nil
}
