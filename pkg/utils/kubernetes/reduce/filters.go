package reduce

import (
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/controller/konnect/constraints"
)

// FilterNone filter nothing, that is it returns the same slice as provided.
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

	toFilter := 0
	for i, secret := range secrets {
		if secret.CreationTimestamp.Before(&secrets[toFilter].CreationTimestamp) {
			toFilter = i
		}
	}

	return append(secrets[:toFilter], secrets[toFilter+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - Deployments
// -----------------------------------------------------------------------------

// filterDeployments filters out the Deployment to be kept and returns
// all the Deployments to be deleted.
//
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
// Filter functions - PodDisruptionBudgets
// -----------------------------------------------------------------------------

// FilterPodDisruptionBudgets filters out the PodDisruptionBudgets to be kept and returns all
// the PodDisruptionBudgets to be deleted.
// The filtered-out PodDisruptionBudget is decided as follows:
// 1. creationTimestamp (older is better)
func FilterPodDisruptionBudgets(pdbs []policyv1.PodDisruptionBudget) []policyv1.PodDisruptionBudget {
	if len(pdbs) < 2 {
		return nil
	}

	best := 0
	for i, hpa := range pdbs {
		if hpa.CreationTimestamp.Before(&pdbs[best].CreationTimestamp) {
			best = i
		}
	}

	return append(pdbs[:best], pdbs[best+1:]...)
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

// -----------------------------------------------------------------------------
// Filter functions - KongPluginBindings
// -----------------------------------------------------------------------------

// filterKongPluginBindings filters out the KongPluginBindings to be kept and returns all the KongPluginBindings
// to be deleted.
// The KongPluginBinding with Programmed status condition is kept.
// If no such binding is found the oldest is kept.
func filterKongPluginBindings(kpbs []configurationv1alpha1.KongPluginBinding) []configurationv1alpha1.KongPluginBinding {
	if len(kpbs) < 2 {
		return []configurationv1alpha1.KongPluginBinding{}
	}

	programmed := -1
	best := 0
	for i, kpb := range kpbs {
		if lo.ContainsBy(kpb.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				c.Status == metav1.ConditionTrue
		}) {

			if programmed != -1 && kpb.CreationTimestamp.Before(&kpbs[programmed].CreationTimestamp) {
				best = i
				programmed = i
			} else if programmed == -1 {
				best = i
				programmed = i
			}

			continue
		}

		if kpb.CreationTimestamp.Before(&kpbs[best].CreationTimestamp) && programmed == -1 {
			best = i
		}
	}

	return append(kpbs[:best], kpbs[best+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - KongCredentials
// -----------------------------------------------------------------------------

// filterKongCredentials filters out the KongCredentials to be kept and returns all the KongCredentials
// to be deleted.
// The KongCredential with Programmed status condition is kept.
// If no such credential is found the oldest is kept.
func filterKongCredentials[
	T constraints.SupportedCredentialType,
	TPtr constraints.KongCredential[T],
](creds []T) []T {
	if len(creds) < 2 {
		return []T{}
	}

	programmed := -1
	best := 0
	for i, cred := range creds {
		ptr := TPtr(&cred)
		containsProgrammedConditionTrue := lo.ContainsBy(ptr.GetConditions(),
			func(c metav1.Condition) bool {
				return c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					c.Status == metav1.ConditionTrue
			},
		)

		if containsProgrammedConditionTrue {
			switch programmed {
			case -1:
				best = i
				programmed = i
			default:
				ptrProgrammed := TPtr(&creds[programmed])
				if ptr.GetCreationTimestamp().UTC().Before(ptrProgrammed.GetCreationTimestamp().UTC()) {
					best = i
					programmed = i
				}
			}

			continue
		}

		ptrBest := TPtr(&creds[best])
		if ptr.GetCreationTimestamp().UTC().Before(ptrBest.GetCreationTimestamp().UTC()) && programmed == -1 {
			best = i
		}
	}

	return append(creds[:best], creds[best+1:]...)
}

// -----------------------------------------------------------------------------
// Filter functions - KongDataPlaneClientCertificates
// -----------------------------------------------------------------------------

// KongDataPlaneClientCertificates filters out the KongDataPlaneClientCertificates to be kept and returns all the KongDataPlaneClientCertificates
// to be deleted.
// The KongDataPlaneClientCertificate with Programmed status condition is kept.
// If no such certificate is found the oldest is kept.
func filterKongDataPlaneClientCertificates(certs []configurationv1alpha1.KongDataPlaneClientCertificate) []configurationv1alpha1.KongDataPlaneClientCertificate {
	if len(certs) < 2 {
		return []configurationv1alpha1.KongDataPlaneClientCertificate{}
	}

	programmed := -1
	best := 0
	for i, kpb := range certs {
		if lo.ContainsBy(kpb.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				c.Status == metav1.ConditionTrue
		}) {

			if programmed != -1 && kpb.CreationTimestamp.Before(&certs[programmed].CreationTimestamp) {
				best = i
				programmed = i
			} else if programmed == -1 {
				best = i
				programmed = i
			}

			continue
		}

		if kpb.CreationTimestamp.Before(&certs[best].CreationTimestamp) && programmed == -1 {
			best = i
		}
	}

	return append(certs[:best], certs[best+1:]...)
}
