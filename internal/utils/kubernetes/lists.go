package kubernetes

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListDeploymentsForOwner is a helper function to map a list of Deployments
// by list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListDeploymentsForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]appsv1.Deployment, error) {
	deploymentList := &appsv1.DeploymentList{}

	err := c.List(
		ctx,
		deploymentList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	deployments := make([]appsv1.Deployment, 0)
	for _, deployment := range deploymentList.Items {
		if IsOwnedByRefUID(&deployment.ObjectMeta, uid) {
			deployments = append(deployments, deployment)
		}
	}

	return deployments, nil
}

// ListServicesForOwner is a helper function to map a list of Services
// by list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListServicesForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]corev1.Service, error) {
	serviceList := &corev1.ServiceList{}

	err := c.List(
		ctx,
		serviceList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	services := make([]corev1.Service, 0)
	for _, service := range serviceList.Items {
		if IsOwnedByRefUID(&service.ObjectMeta, uid) {
			services = append(services, service)
		}
	}

	return services, nil
}

// ListServiceAccountsForOwner is a helper function to map a list of ServiceAccounts
// by list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListServiceAccountsForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]corev1.ServiceAccount, error) {
	serviceAccountList := &corev1.ServiceAccountList{}

	err := c.List(
		ctx,
		serviceAccountList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	serviceAccounts := make([]corev1.ServiceAccount, 0)
	for _, serviceAccount := range serviceAccountList.Items {
		for _, ownerRef := range serviceAccount.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				serviceAccounts = append(serviceAccounts, serviceAccount)
				break
			}
		}
	}

	return serviceAccounts, nil
}

// ListClusterRolesForOwner is a helper function to map a list of ClusterRoles
// by list options and reduce by OwnerReference UID to efficiently
// list only the objects owned by the provided UID.
func ListClusterRolesForOwner(
	ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]rbacv1.ClusterRole, error) {
	clusterRoleList := &rbacv1.ClusterRoleList{}

	err := c.List(
		ctx,
		clusterRoleList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	clusterRoles := make([]rbacv1.ClusterRole, 0)
	for _, clusterRole := range clusterRoleList.Items {
		for _, ownerRef := range clusterRole.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				clusterRoles = append(clusterRoles, clusterRole)
				break
			}
		}
	}

	return clusterRoles, nil
}

// ListClusterRoleBindingsForOwner is a helper function to map a list of ClusterRoleBindings
// by list options and reduce by OwnerReference UID to efficiently
// list only the objects owned by the provided UID.
func ListClusterRoleBindingsForOwner(
	ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]rbacv1.ClusterRoleBinding, error) {
	clusterRoleBindingList := &rbacv1.ClusterRoleBindingList{}

	err := c.List(
		ctx,
		clusterRoleBindingList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	clusterRoleBindings := make([]rbacv1.ClusterRoleBinding, 0)
	for _, clusterRoleBinding := range clusterRoleBindingList.Items {
		for _, ownerRef := range clusterRoleBinding.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				clusterRoleBindings = append(clusterRoleBindings, clusterRoleBinding)
				break
			}
		}
	}

	return clusterRoleBindings, nil
}

// ListSecretsForOwner is a helper function to map a list of Secrets
// by list options and reduce by OwnerReference UID to efficiently
// list only the objects owned by the provided UID.
func ListSecretsForOwner(ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]corev1.Secret, error) {
	secretList := &corev1.SecretList{}

	err := c.List(
		ctx,
		secretList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	secrets := make([]corev1.Secret, 0)
	for _, secret := range secretList.Items {
		for _, ownerRef := range secret.ObjectMeta.OwnerReferences {
			if ownerRef.UID == uid {
				secrets = append(secrets, secret)
				break
			}
		}
	}

	return secrets, nil
}
