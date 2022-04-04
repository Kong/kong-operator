package kubernetes

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// -----------------------------------------------------------------------------
// Public Functions - Owner References
// -----------------------------------------------------------------------------

// ListDeploymentsForOwner is a helper function to map a list of Deployments
// by label and reduce by OwnerReference UID and namespace to efficiently list
// only the objects owned by the provided UID.
func ListDeploymentsForOwner(
	ctx context.Context,
	c client.Client,
	requiredLabel string,
	requiredValue string,
	namespace string,
	uid types.UID,
) ([]appsv1.Deployment, error) {
	requirement, err := labels.NewRequirement(
		requiredLabel,
		selection.Equals,
		[]string{requiredValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		LabelSelector: selector,
	}
	if namespace != "" {
		listOptions.Namespace = namespace
	}

	deploymentList := &appsv1.DeploymentList{}
	if err := c.List(ctx, deploymentList, listOptions); err != nil {
		return nil, err
	}

	deployments := make([]appsv1.Deployment, 0)
	for _, deployment := range deploymentList.Items {
		for _, ownerRef := range deployment.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				deployments = append(deployments, deployment)
				break
			}
		}
	}

	return deployments, nil
}

// ListServicesForOwner is a helper function to map a list of Services
// by label and reduce by OwnerReference UID and namespace to efficiently list
// only the objects owned by the provided UID.
func ListServicesForOwner(
	ctx context.Context,
	c client.Client,
	requiredLabel string,
	requiredValue string,
	namespace string,
	uid types.UID,
) ([]corev1.Service, error) {
	requirement, err := labels.NewRequirement(
		requiredLabel,
		selection.Equals,
		[]string{requiredValue},
	)
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*requirement)

	listOptions := &client.ListOptions{
		LabelSelector: selector,
	}
	if namespace != "" {
		listOptions.Namespace = namespace
	}

	serviceList := &corev1.ServiceList{}
	if err := c.List(ctx, serviceList, listOptions); err != nil {
		return nil, err
	}

	services := make([]corev1.Service, 0)
	for _, service := range serviceList.Items {
		for _, ownerRef := range service.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				services = append(services, service)
				break
			}
		}
	}

	return services, nil
}
