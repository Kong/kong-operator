package kubernetes

import (
	"context"
	"slices"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListDeploymentsForOwner which gets a list of Deployments using the provided
// list options and reduce by OwnerReference UID and namespace to efficiently
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
		if IsOwnedByRefUID(&deployment, uid) {
			deployments = append(deployments, deployment)
		}
	}

	return deployments, nil
}

// ListHPAsForOwner is a helper function which gets a list of HorizontalPodAutoscalers
// using the provided list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListHPAsForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	hpaList := &autoscalingv2.HorizontalPodAutoscalerList{}

	err := c.List(
		ctx,
		hpaList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	hpas := make([]autoscalingv2.HorizontalPodAutoscaler, 0)
	for _, hpa := range hpaList.Items {
		if IsOwnedByRefUID(&hpa, uid) {
			hpas = append(hpas, hpa)
		}
	}

	return hpas, nil
}

// ListPodDisruptionBudgetsForOwner is a helper function which gets a list of PodDisruptionBudget
// using the provided list options and reduce by OwnerReference UID and namespace to efficiently
// list only the objects owned by the provided UID.
func ListPodDisruptionBudgetsForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]policyv1.PodDisruptionBudget, error) {
	pdbList := &policyv1.PodDisruptionBudgetList{}

	err := c.List(
		ctx,
		pdbList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	var pdbs []policyv1.PodDisruptionBudget
	for _, pdb := range pdbList.Items {
		if IsOwnedByRefUID(&pdb, uid) {
			pdbs = append(pdbs, pdb)
		}
	}

	return pdbs, nil
}

// ListServicesForOwner is a helper function which gets a list of Services
// using the provided list options and reduce by OwnerReference UID and namespace to efficiently
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
		if IsOwnedByRefUID(&service, uid) {
			services = append(services, service)
		}
	}

	return services, nil
}

// ListServiceAccountsForOwner is a helper function which gets a list of ServiceAccounts
// using the provided list options and reduce by OwnerReference UID and namespace to efficiently
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
		if IsOwnedByRefUID(&serviceAccount, uid) {
			serviceAccounts = append(serviceAccounts, serviceAccount)
		}
	}

	return serviceAccounts, nil
}

// ListRoles is a helper function which gets a list of Roles
// using the provided list options.
func ListRoles(
	ctx context.Context,
	c client.Client,
	listOpts ...client.ListOption,
) ([]rbacv1.Role, error) {
	roleList := &rbacv1.RoleList{}

	err := c.List(
		ctx,
		roleList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	return roleList.Items, nil
}

// ListClusterRoles is a helper function which gets a list of ClusterRoles
// using the provided list options.
func ListClusterRoles(
	ctx context.Context,
	c client.Client,
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

	return clusterRoleList.Items, nil
}

// ListClusterRoleBindings is a helper function which gets a list of ClusterRoleBindings
// using the provided list options.
func ListClusterRoleBindings(
	ctx context.Context,
	c client.Client,
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

	return clusterRoleBindingList.Items, nil
}

// ListRoleBindings is a helper function which gets a list of RoleBindings
// using the provided list options.
func ListRoleBindings(
	ctx context.Context,
	c client.Client,
	listOpts ...client.ListOption,
) ([]rbacv1.RoleBinding, error) {
	roleBindingList := &rbacv1.RoleBindingList{}

	err := c.List(
		ctx,
		roleBindingList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	return roleBindingList.Items, nil
}

// ListConfigMapsForOwner is a helper function which gets a list of ConfigMaps
// using the provided list options and reduce by OwnerReference UID to efficiently
// list only the objects owned by the provided UID.
func ListConfigMapsForOwner(ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]corev1.ConfigMap, error) {
	configMapList := &corev1.ConfigMapList{}
	if err := c.List(
		ctx,
		configMapList,
		listOpts...,
	); err != nil {
		return nil, err
	}

	configMaps := make([]corev1.ConfigMap, 0)
	for _, cm := range configMapList.Items {
		if IsOwnedByRefUID(&cm, uid) {
			configMaps = append(configMaps, cm)
		}
	}

	return configMaps, nil
}

// ListSecretsForOwner is a helper function which gets a list of Secrets
// using the provided list options and reduce by OwnerReference UID to efficiently
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
		if IsOwnedByRefUID(&secret, uid) {
			secrets = append(secrets, secret)
		}
	}

	return secrets, nil
}

// ListValidatingWebhookConfigurations is a helper function that gets a list of ValidatingWebhookConfiguration
// using the provided list options.
func ListValidatingWebhookConfigurations(
	ctx context.Context,
	c client.Client,
	listOpts ...client.ListOption,
) ([]admregv1.ValidatingWebhookConfiguration, error) {
	cfgList := &admregv1.ValidatingWebhookConfigurationList{}
	err := c.List(
		ctx,
		cfgList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	return cfgList.Items, nil
}

// ListValidatingWebhookConfigurationsForOwner is a helper function that gets a list of ValidatingWebhookConfiguration
// using the provided list options and checking if the provided object is the owner.
func ListValidatingWebhookConfigurationsForOwner(
	ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]admregv1.ValidatingWebhookConfiguration, error) {
	cfgList := &admregv1.ValidatingWebhookConfigurationList{}
	err := c.List(
		ctx,
		cfgList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(
		cfgList.Items, func(vwc admregv1.ValidatingWebhookConfiguration) bool {
			return !IsOwnedByRefUID(&vwc, uid)
		},
	), nil
}

func ListKongDataPlaneClientCertificateForOwner(
	ctx context.Context,
	c client.Client,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]configurationv1alpha1.KongDataPlaneClientCertificate, error) {
	certList := &configurationv1alpha1.KongDataPlaneClientCertificateList{}
	err := c.List(
		ctx,
		certList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(
		certList.Items, func(cert configurationv1alpha1.KongDataPlaneClientCertificate) bool {
			return !IsOwnedByRefUID(&cert, uid)
		},
	), nil
}
