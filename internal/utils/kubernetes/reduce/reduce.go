package reduce

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PreDeleteHook is a function that can be executed before deleting an object.
type PreDeleteHook func(ctx context.Context, cl client.Client, obj client.Object) error

//+kubebuilder:rbac:groups=core,resources=secrets,verbs=delete

// ReduceSecrets detects the best secret in the set and deletes all the others.
// It accepts optional preDeleteHooks which are executed before every Secret delete operation.
func ReduceSecrets(ctx context.Context, k8sClient client.Client, secrets []corev1.Secret, preDeleteHooks ...PreDeleteHook) error {
	filteredSecrets := filterSecrets(secrets)
	for _, secret := range filteredSecrets {
		secret := secret
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &secret); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &secret); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=delete

// ReduceServiceAccounts detects the best serviceAccount in the set and deletes all the others.
func ReduceServiceAccounts(ctx context.Context, k8sClient client.Client, serviceAccounts []corev1.ServiceAccount) error {
	filteredServiceAccounts := filterServiceAccounts(serviceAccounts)
	for _, serviceAccount := range filteredServiceAccounts {
		serviceAccount := serviceAccount
		if err := k8sClient.Delete(ctx, &serviceAccount); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=delete

// ReduceClusterRoles detects the best ClusterRole in the set and deletes all the others.
func ReduceClusterRoles(ctx context.Context, k8sClient client.Client, clusterRoles []rbacv1.ClusterRole) error {
	filteredClusterRoles := filterClusterRoles(clusterRoles)
	for _, clusterRole := range filteredClusterRoles {
		clusterRole := clusterRole
		if err := k8sClient.Delete(ctx, &clusterRole); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=delete

// ReduceClusterRoleBindings detects the best ClusterRoleBinding in the set and deletes all the others.
func ReduceClusterRoleBindings(ctx context.Context, k8sClient client.Client, clusterRoleBindings []rbacv1.ClusterRoleBinding) error {
	filteredCLusterRoleBindings := filterClusterRoleBindings(clusterRoleBindings)
	for _, clusterRoleBinding := range filteredCLusterRoleBindings {
		clusterRoleBinding := clusterRoleBinding
		if err := k8sClient.Delete(ctx, &clusterRoleBinding); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=delete

// ReduceDeployments detects the best Deployment in the set and deletes all the others.
// It accepts optional preDeleteHooks which are executed before every Deployment delete operation.
func ReduceDeployments(ctx context.Context, k8sClient client.Client, deployments []appsv1.Deployment, preDeleteHooks ...PreDeleteHook) error {
	filteredDeployments := filterDeployments(deployments)
	for _, deployment := range filteredDeployments {
		deployment := deployment
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &deployment); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &deployment); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups="discovery.k8s.io",resources=endpointslices,verbs=list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=delete

// ReduceServices detects the best Service in the set and deletes all the others.
// It accepts optional preDeleteHooks which are executed before every Service delete operation.
func ReduceServices(ctx context.Context, k8sClient client.Client, services []corev1.Service, preDeleteHooks ...PreDeleteHook) error {
	mappedEndpointSlices := make(map[string][]discoveryv1.EndpointSlice)
	for _, service := range services {
		endpointSliceList := &discoveryv1.EndpointSliceList{}
		err := k8sClient.List(ctx, endpointSliceList,
			client.InNamespace(service.Namespace),
			client.MatchingLabels{
				discoveryv1.LabelServiceName: service.Name,
			})
		if err != nil {
			return err
		}
		mappedEndpointSlices[service.Name] = endpointSliceList.Items
	}
	filteredServices := filterServices(services, mappedEndpointSlices)
	for _, service := range filteredServices {
		service := service
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &service); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &service); err != nil {
			return err
		}
	}
	return nil
}

//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=delete

// ReduceNetworkPolicies detects the best NetworkPolicy in the set and deletes all the others.
func ReduceNetworkPolicies(ctx context.Context, k8sClient client.Client, networkPolicies []networkingv1.NetworkPolicy) error {
	filteredNetworkPolicies := filterNetworkPolicies(networkPolicies)
	for _, networkPolicy := range filteredNetworkPolicies {
		networkPolicy := networkPolicy
		if err := k8sClient.Delete(ctx, &networkPolicy); err != nil {
			return err
		}
	}
	return nil
}
