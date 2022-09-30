package reduce

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

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
// 1. creationTimestamp (older is better)
func filterClusterRoles(clusterRoles []rbacv1.ClusterRole) []rbacv1.ClusterRole {
	if len(clusterRoles) < 2 {
		return []rbacv1.ClusterRole{}
	}

	toFilter := 0
	for i, clusterRole := range clusterRoles {
		if clusterRole.CreationTimestamp.Before(&clusterRoles[toFilter].CreationTimestamp) {
			toFilter = i
		}
	}

	return append(clusterRoles[:toFilter], clusterRoles[toFilter+1:]...)
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

	toFilter := 0
	for i, clusterRoleBinding := range clusterRoleBindings {
		if clusterRoleBinding.CreationTimestamp.Before(&clusterRoleBindings[toFilter].CreationTimestamp) {
			toFilter = i
		}
	}

	return append(clusterRoleBindings[:toFilter], clusterRoleBindings[toFilter+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - Deployments
// -----------------------------------------------------------------------------

// filterDeployments filters out the Deployment to be kept and returns
// all the Deployments to be deleted.
// The filtered-out Deployment is decided as follows:
// 1. number of availableReplicas (higher is better)
// 2. number of readyReplicas (higher is better)
// 3. creationTimestamp (older is better)
func filterDeployments(deployments []appsv1.Deployment) []appsv1.Deployment {
	if len(deployments) < 2 {
		return []appsv1.Deployment{}
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
// The filtered-out Service is decided as follows:
// 1. amount of LoadBalancer Ingresses (higher is better)
// 2. amount of endpointSlices allocated for the service (higher is better)
// 3. amount of ready endpoints for the service (higher is better)
// 4. creationTimestamp (older is better)
func filterServices(services []corev1.Service, endpointSlices map[string][]discoveryv1.EndpointSlice) []corev1.Service {
	if len(services) < 2 {
		return []corev1.Service{}
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
				readyEndpoints += 1
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
