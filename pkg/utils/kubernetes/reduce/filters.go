package reduce

import (
	"github.com/samber/lo"
	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/gateway-operator/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
)

// FiltenNone filter nothing, that is it returns the same slice as provided.
func FilterNone[T any](objs []T) []T {
	return objs
}

// -----------------------------------------------------------------------------
// Filter functions - Secrets
// -----------------------------------------------------------------------------

// filterSecrets filters out the Secret to be kept and returns all the Secrets
// to be deleted.
// The filtered-out Secret is decided as follows:
// 1. creationTimestamp (older is better)
func filterSecrets(secrets []corev1.Secret) []corev1.Secret {
	if len(secrets) < 2 {
		return []corev1.Secret{}
	}

	legacySecrets := lo.Filter(secrets, func(s corev1.Secret, index int) bool {
		_, okLegacy := s.Labels[consts.GatewayOperatorManagedByLabelLegacy]
		_, ok := s.Labels[consts.GatewayOperatorManagedByLabel]
		return okLegacy && !ok
	})
	// If all Secrets are legacy, then remove all but one.
	// The last one which we won't return for deletion will get updated on the next reconcile.
	if len(legacySecrets) == len(secrets) {
		return legacySecrets[:len(legacySecrets)-1]
		// Otherwise - if not all Secrets are legacy - then remove all legacy Secrets.
	} else if len(legacySecrets) > 0 {
		return legacySecrets
	}

	toFilter := 0
	for i, secret := range secrets {
		if secret.CreationTimestamp.Before(&secrets[toFilter].CreationTimestamp) {
			toFilter = i
		}
	}

	return append(secrets[:toFilter], secrets[toFilter+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - ServiceAccounts
// -----------------------------------------------------------------------------

// filterServiceAccounts filters out the ServiceAccount to be kept and returns
// all the ServiceAccounts to be deleted.
// The filtered-out ServiceAccount is decided as follows:
// 1. creationTimestamp (older is better)
func filterServiceAccounts(serviceAccounts []corev1.ServiceAccount) []corev1.ServiceAccount {
	if len(serviceAccounts) < 2 {
		return []corev1.ServiceAccount{}
	}

	toFilter := 0
	for i, serviceAccount := range serviceAccounts {
		if serviceAccount.CreationTimestamp.Before(&serviceAccounts[toFilter].CreationTimestamp) {
			toFilter = i
		}
	}

	return append(serviceAccounts[:toFilter], serviceAccounts[toFilter+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - ClusterRoles
// -----------------------------------------------------------------------------

// filterClusterRoles filters out the ClusterRole to be kept and returns
// all the ClusterRoles to be deleted.
// The filtered-out ClusterRole is decided as follows:
//  1. creationTimestamp (newer is better, because newer ClusterRoles can contain new policy rules)
//  2. using legacy labels (if present): if a ClusterRole does not have the legacy labels, it is considered newer
//     and will be kept.
func filterClusterRoles(clusterRoles []rbacv1.ClusterRole) []rbacv1.ClusterRole {
	if len(clusterRoles) < 2 {
		return []rbacv1.ClusterRole{}
	}

	newestWithManagedByLabels := -1
	newestLegacy := -1
	for i, clusterRole := range clusterRoles {
		labels := clusterRole.GetLabels()

		_, okManagedBy := labels[consts.GatewayOperatorManagedByLabel]
		_, okManagedByNs := labels[consts.GatewayOperatorManagedByNamespaceLabel]
		_, okManagedByName := labels[consts.GatewayOperatorManagedByNameLabel]
		if okManagedBy && okManagedByNs && okManagedByName {
			if newestWithManagedByLabels == -1 {
				newestWithManagedByLabels = i
				continue
			}

			if clusterRole.CreationTimestamp.After(clusterRoles[newestWithManagedByLabels].CreationTimestamp.Time) {
				newestWithManagedByLabels = i
			}
			continue
		}

		if newestLegacy == -1 {
			newestLegacy = i
			continue
		}

		if clusterRole.CreationTimestamp.After(clusterRoles[newestLegacy].CreationTimestamp.Time) {
			newestLegacy = i
		}
		continue
	}

	if newestWithManagedByLabels != -1 {
		return append(clusterRoles[:newestWithManagedByLabels], clusterRoles[newestWithManagedByLabels+1:]...)
	}
	return append(clusterRoles[:newestLegacy], clusterRoles[newestLegacy+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - ClusterRoleBindings
// -----------------------------------------------------------------------------

// filterClusterRoleBindings filters out the ClusterRoleBinding to be kept and returns
// all the ClusterRoleBindings to be deleted.
// The filtered-out ClusterRoleBinding is decided as follows:
// 1. creationTimestamp (older is better)
func filterClusterRoleBindings(clusterRoleBindings []rbacv1.ClusterRoleBinding) []rbacv1.ClusterRoleBinding {
	if len(clusterRoleBindings) < 2 {
		return []rbacv1.ClusterRoleBinding{}
	}

	oldestWithManagedByLabels := -1
	oldestLegacy := -1
	for i, clusterRoleBinding := range clusterRoleBindings {
		labels := clusterRoleBinding.GetLabels()

		_, okManagedBy := labels[consts.GatewayOperatorManagedByLabel]
		_, okManagedByNs := labels[consts.GatewayOperatorManagedByNamespaceLabel]
		_, okManagedByName := labels[consts.GatewayOperatorManagedByNameLabel]
		if okManagedBy && okManagedByNs && okManagedByName {
			if oldestWithManagedByLabels == -1 {
				oldestWithManagedByLabels = i
				continue
			}

			if clusterRoleBinding.CreationTimestamp.Before(&clusterRoleBindings[oldestWithManagedByLabels].CreationTimestamp) {
				oldestWithManagedByLabels = i
			}
			continue
		}

		if oldestLegacy == -1 {
			oldestLegacy = i
			continue
		}

		if clusterRoleBinding.CreationTimestamp.Before(&clusterRoleBindings[oldestLegacy].CreationTimestamp) {
			oldestLegacy = i
		}
		continue
	}

	if oldestWithManagedByLabels != -1 {
		return append(clusterRoleBindings[:oldestWithManagedByLabels], clusterRoleBindings[oldestWithManagedByLabels+1:]...)
	}
	return append(clusterRoleBindings[:oldestLegacy], clusterRoleBindings[oldestLegacy+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - Deployments
// -----------------------------------------------------------------------------

// filterDeployments filters out the Deployment to be kept and returns
// all the Deployments to be deleted.
//
// The filtered-out Deployment is decided as follows:
// 1. using legacy labels (if present)
// 2. number of availableReplicas (higher is better)
// 3. number of readyReplicas (higher is better)
// 4. creationTimestamp (older is better)
func filterDeployments(deployments []appsv1.Deployment) []appsv1.Deployment {
	if len(deployments) < 2 {
		return []appsv1.Deployment{}
	}

	legacyDeployments := lo.Filter(deployments, func(d appsv1.Deployment, index int) bool {
		_, okLegacy := d.Labels[consts.GatewayOperatorManagedByLabelLegacy]
		_, ok := d.Labels[consts.GatewayOperatorManagedByLabel]
		return okLegacy && !ok
	})
	// If all Deployments are legacy, then remove all but one.
	// The last one which we won't return for deletion will get updated on the next reconcile.
	if len(legacyDeployments) == len(deployments) {
		return legacyDeployments[:len(legacyDeployments)-1]
		// Otherwise - if not all Deployments are legacy - then remove all legacy Deployments.
	} else if len(legacyDeployments) > 0 {
		return legacyDeployments
	}

	toFilter := 0
	for i, deployment := range deployments {
		// check which deployment has more availableReplicas
		if deployment.Status.AvailableReplicas != deployments[toFilter].Status.AvailableReplicas {
			if deployment.Status.AvailableReplicas > deployments[toFilter].Status.AvailableReplicas {
				toFilter = i
			}
			continue
		}
		// check which deployment has more readyReplicas
		if deployment.Status.ReadyReplicas != deployments[toFilter].Status.ReadyReplicas {
			if deployment.Status.ReadyReplicas > deployments[toFilter].Status.ReadyReplicas {
				toFilter = i
			}
			continue
		}
		// check the older service
		if deployment.CreationTimestamp.Before(&deployments[toFilter].CreationTimestamp) {
			toFilter = i
			continue
		}
	}

	return append(deployments[:toFilter], deployments[toFilter+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - Services
// -----------------------------------------------------------------------------

// filterServices filters out the Service to be kept and returns
// all the Services to be deleted.
// The arguments are the slice of Services to apply the logic on, and a map
// that associates all the Services to the owned EndpointSlices.
//
// The filtered-out Service is decided as follows:
// 1. using legacy labels (if present)
// 2. amount of LoadBalancer Ingresses (higher is better)
// 3. amount of endpointSlices allocated for the service (higher is better)
// 4. amount of ready endpoints for the service (higher is better)
// 5. creationTimestamp (older is better)
func filterServices(services []corev1.Service, endpointSlices map[string][]discoveryv1.EndpointSlice) []corev1.Service {
	if len(services) < 2 {
		return []corev1.Service{}
	}

	legacyServices := lo.Filter(services, func(s corev1.Service, index int) bool {
		_, okLegacy := s.Labels[consts.GatewayOperatorManagedByLabelLegacy]
		_, ok := s.Labels[consts.GatewayOperatorManagedByLabel]
		return okLegacy && !ok
	})
	// If all services are legacy, then remove all but one.
	// The last one which we won't return for deletion will get updated on the next reconcile.
	if len(legacyServices) == len(services) {
		return legacyServices[:len(legacyServices)-1]
		// Otherwise - if not all services are legacy - then remove all legacy services.
	} else if len(legacyServices) > 0 {
		return legacyServices
	}

	toFilter, toFilterReadyEndpointsCount := 0, getReadyEndpointsCount(endpointSlices[services[0].Name])
	for i, service := range services {
		iReadyEndpointsCount := getReadyEndpointsCount(endpointSlices[service.Name])
		// check the loadBalancer addresses first
		if len(service.Status.LoadBalancer.Ingress) != len(services[toFilter].Status.LoadBalancer.Ingress) {
			if len(service.Status.LoadBalancer.Ingress) > len(services[toFilter].Status.LoadBalancer.Ingress) {
				toFilter = i
				toFilterReadyEndpointsCount = iReadyEndpointsCount
			}
			continue
		}
		// check the amount of endpointSlices allocated for the service
		if len(endpointSlices[service.Name]) != len(endpointSlices[services[toFilter].Name]) {
			if len(endpointSlices[service.Name]) > len(endpointSlices[services[toFilter].Name]) {
				toFilter = i
				toFilterReadyEndpointsCount = iReadyEndpointsCount
			}
			continue
		}
		// check the amount of Ready endpoints for the service
		if iReadyEndpointsCount != toFilterReadyEndpointsCount {
			if iReadyEndpointsCount > toFilterReadyEndpointsCount {
				toFilter = i
				toFilterReadyEndpointsCount = iReadyEndpointsCount
			}
			continue
		}
		// check the older service
		if service.CreationTimestamp.Before(&services[toFilter].CreationTimestamp) {
			toFilter = i
			toFilterReadyEndpointsCount = iReadyEndpointsCount
		}
	}
	return append(services[:toFilter], services[toFilter+1:]...)
}

// getReadyEndpointsCount returns the amount of ready endpoints in a set of endpointSlices.
func getReadyEndpointsCount(endpointSlices []discoveryv1.EndpointSlice) int {
	readyEndpoints := 0
	for _, epSlice := range endpointSlices {
		for _, endpoint := range epSlice.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				readyEndpoints++
			}
		}
	}
	return readyEndpoints
}

// -----------------------------------------------------------------------------
// Filter functions - NetworkPolicies
// -----------------------------------------------------------------------------

// filterNetworkPolicies filters out the NetworkPolicy to be kept and returns all the NetworkPolicies
// to be deleted.
// The filtered-out NetworkPolicy is decided as follows:
// 1. creationTimestamp (older is better)
func filterNetworkPolicies(networkPolicies []networkingv1.NetworkPolicy) []networkingv1.NetworkPolicy {
	if len(networkPolicies) < 2 {
		return []networkingv1.NetworkPolicy{}
	}

	best := 0
	for i, networkPolicy := range networkPolicies {
		if networkPolicy.CreationTimestamp.Before(&networkPolicies[best].CreationTimestamp) {
			best = i
		}
	}

	return append(networkPolicies[:best], networkPolicies[best+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - HorizontalPodAutoscalers
// -----------------------------------------------------------------------------

// FilterHPAs filters out the HorizontalPodAutoscalers to be kept and returns all
// the HorizontalPodAutoscalers to be deleted.
// The filtered-out HorizontalPodAutoscalers is decided as follows:
// 1. creationTimestamp (older is better)
func FilterHPAs(hpas []autoscalingv2.HorizontalPodAutoscaler) []autoscalingv2.HorizontalPodAutoscaler {
	if len(hpas) < 2 {
		return []autoscalingv2.HorizontalPodAutoscaler{}
	}

	best := 0
	for i, hpa := range hpas {
		if hpa.CreationTimestamp.Before(&hpas[best].CreationTimestamp) {
			best = i
		}
	}

	return append(hpas[:best], hpas[best+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - ValidatingWebhookConfigurations
// -----------------------------------------------------------------------------

// filterValidatingWebhookConfigurations filters out the ValidatingWebhookConfigurations
// to be kept and returns all the ValidatingWebhookConfigurations to be deleted.
// The following criteria are used:
//  1. creationTimestamp (newer is better, because newer ValidatingWebhookConfiguration can contain new rules)
//  2. using legacy labels (if present): if a ValidatingWebhookConfiguration does
//     not have the legacy labels, it is considered newer and will be kept.
func filterValidatingWebhookConfigurations(vwcs []admregv1.ValidatingWebhookConfiguration) []admregv1.ValidatingWebhookConfiguration {
	if len(vwcs) < 2 {
		return []admregv1.ValidatingWebhookConfiguration{}
	}

	newestWithManagedByLabels := -1
	newestLegacy := -1
	for i, vwc := range vwcs {
		labels := vwc.GetLabels()

		_, okManagedBy := labels[consts.GatewayOperatorManagedByLabel]
		_, okManagedByNs := labels[consts.GatewayOperatorManagedByNamespaceLabel]
		_, okManagedByName := labels[consts.GatewayOperatorManagedByNameLabel]
		if okManagedBy && okManagedByNs && okManagedByName {
			if newestWithManagedByLabels == -1 {
				newestWithManagedByLabels = i
				continue
			}

			if vwc.CreationTimestamp.After(vwcs[newestWithManagedByLabels].CreationTimestamp.Time) {
				newestWithManagedByLabels = i
			}
			continue
		}

		if newestLegacy == -1 {
			newestLegacy = i
			continue
		}

		if vwc.CreationTimestamp.After(vwcs[newestLegacy].CreationTimestamp.Time) {
			newestLegacy = i
		}
		continue
	}

	if newestWithManagedByLabels != -1 {
		return append(vwcs[:newestWithManagedByLabels], vwcs[newestWithManagedByLabels+1:]...)
	}
	return append(vwcs[:newestLegacy], vwcs[newestLegacy+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - DataPlanes
// -----------------------------------------------------------------------------

// filterDataPlanes filters out the DataPlanes to be kept and returns all the DataPlanes
// to be deleted. The oldest DataPlane is kept.
func filterDataPlanes(dataplanes []operatorv1beta1.DataPlane) []operatorv1beta1.DataPlane {
	if len(dataplanes) < 2 {
		return []operatorv1beta1.DataPlane{}
	}

	best := 0
	for i, dataplane := range dataplanes {
		if dataplane.CreationTimestamp.Before(&dataplanes[best].CreationTimestamp) {
			best = i
		}
	}

	return append(dataplanes[:best], dataplanes[best+1:]...)
}
