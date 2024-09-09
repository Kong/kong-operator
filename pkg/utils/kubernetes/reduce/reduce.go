package reduce

import (
	"context"
	"fmt"

	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

// PreDeleteHook is a function that can be executed before deleting an object.
type PreDeleteHook func(ctx context.Context, cl client.Client, obj client.Object) error

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=delete

// ReduceSecrets detects the best secret in the set and deletes all the others.
// It accepts optional preDeleteHooks which are executed before every Secret delete operation.
func ReduceSecrets(ctx context.Context, k8sClient client.Client, secrets []corev1.Secret, preDeleteHooks ...PreDeleteHook) error {
	filteredSecrets := filterSecrets(secrets)
	for _, secret := range filteredSecrets {
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &secret); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &secret); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=delete

// ReduceServiceAccounts detects the best serviceAccount in the set and deletes all the others.
func ReduceServiceAccounts(ctx context.Context, k8sClient client.Client, serviceAccounts []corev1.ServiceAccount) error {
	filteredServiceAccounts := filterServiceAccounts(serviceAccounts)
	for _, serviceAccount := range filteredServiceAccounts {
		if err := k8sClient.Delete(ctx, &serviceAccount); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=delete

// ReduceClusterRoles detects the best ClusterRole in the set and deletes all the others.
func ReduceClusterRoles(ctx context.Context, k8sClient client.Client, clusterRoles []rbacv1.ClusterRole) error {
	filteredClusterRoles := filterClusterRoles(clusterRoles)
	for _, clusterRole := range filteredClusterRoles {
		if err := k8sClient.Delete(ctx, &clusterRole); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=delete

// ReduceClusterRoleBindings detects the best ClusterRoleBinding in the set and deletes all the others.
func ReduceClusterRoleBindings(ctx context.Context, k8sClient client.Client, clusterRoleBindings []rbacv1.ClusterRoleBinding) error {
	filteredCLusterRoleBindings := filterClusterRoleBindings(clusterRoleBindings)
	for _, clusterRoleBinding := range filteredCLusterRoleBindings {
		if err := k8sClient.Delete(ctx, &clusterRoleBinding); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=delete

// ReduceDeployments detects the best Deployment in the set and deletes all the others.
// It accepts optional preDeleteHooks which are executed before every Deployment delete operation.
func ReduceDeployments(ctx context.Context, k8sClient client.Client, deployments []appsv1.Deployment, preDeleteHooks ...PreDeleteHook) error {
	filteredDeployments := filterDeployments(deployments)
	for _, deployment := range filteredDeployments {
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &deployment); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &deployment); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups="discovery.k8s.io",resources=endpointslices,verbs=list;watch
// +kubebuilder:rbac:groups=core,resources=services,verbs=delete

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
		for _, hook := range preDeleteHooks {
			if err := hook(ctx, k8sClient, &service); err != nil {
				return fmt.Errorf("failed to execute pre delete hook: %w", err)
			}
		}
		if err := k8sClient.Delete(ctx, &service); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=delete

// ReduceNetworkPolicies detects the best NetworkPolicy in the set and deletes all the others.
func ReduceNetworkPolicies(ctx context.Context, k8sClient client.Client, networkPolicies []networkingv1.NetworkPolicy) error {
	filteredNetworkPolicies := filterNetworkPolicies(networkPolicies)
	for _, networkPolicy := range filteredNetworkPolicies {
		if err := k8sClient.Delete(ctx, &networkPolicy); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=delete

// HPAFilterFunc filters a list of HorizontalPodAutoscalers and returns the ones that should be deleted.
type HPAFilterFunc func([]autoscalingv2.HorizontalPodAutoscaler) []autoscalingv2.HorizontalPodAutoscaler

// ReduceHPAs detects the best HorizontalPodAutoscaler in the set and deletes all the others.
func ReduceHPAs(ctx context.Context, k8sClient client.Client, hpas []autoscalingv2.HorizontalPodAutoscaler, filter HPAFilterFunc) error {
	for _, hpa := range filter(hpas) {
		if err := k8sClient.Delete(ctx, &hpa); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=delete

// PDBFilterFunc filters a list of PodDisruptionBudgets and returns the ones that should be deleted.
type PDBFilterFunc func([]policyv1.PodDisruptionBudget) []policyv1.PodDisruptionBudget

// ReducePodDisruptionBudgets detects the best PodDisruptionBudget in the set and deletes all the others.
func ReducePodDisruptionBudgets(ctx context.Context, k8sClient client.Client, pdbs []policyv1.PodDisruptionBudget, filter PDBFilterFunc) error {
	for _, pdb := range filter(pdbs) {
		if err := k8sClient.Delete(ctx, &pdb); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=delete

// ReduceValidatingWebhookConfigurations detects the best ValidatingWebhookConfiguration in the set and deletes all the others.
func ReduceValidatingWebhookConfigurations(ctx context.Context, k8sClient client.Client, webhookConfigurations []admregv1.ValidatingWebhookConfiguration) error {
	filteredWebhookConfigurations := filterValidatingWebhookConfigurations(webhookConfigurations)
	for _, webhookConfiguration := range filteredWebhookConfigurations {
		if err := k8sClient.Delete(ctx, &webhookConfiguration); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=delete

// ReduceDataPlanes detects the best DataPlane in the set and deletes all the others.
func ReduceDataPlanes(ctx context.Context, k8sClient client.Client, dataplanes []operatorv1beta1.DataPlane) error {
	filteredDataPlanes := filterDataPlanes(dataplanes)
	for _, dataplane := range filteredDataPlanes {
		if err := k8sClient.Delete(ctx, &dataplane); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}
