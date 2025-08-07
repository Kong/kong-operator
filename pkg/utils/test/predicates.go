package test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/pkg/features"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/pkg/clientset"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

// controlPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a certain state.
func controlPlanePredicate(
	t *testing.T,
	ctx context.Context,
	controlplaneName types.NamespacedName,
	predicate func(controlplane *gwtypes.ControlPlane) bool,
	operatorClient *clientset.Clientset,
) func() bool {
	controlplaneClient := operatorClient.GatewayOperatorV2beta1().ControlPlanes(controlplaneName.Namespace)
	return func() bool {
		controlplane, err := controlplaneClient.Get(ctx, controlplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(controlplane)
	}
}

// DataPlanePredicate is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a certain state.
func DataPlanePredicate(
	t *testing.T,
	ctx context.Context,
	dataplaneName types.NamespacedName,
	predicate func(dataplane *operatorv1beta1.DataPlane) bool,
	operatorClient *clientset.Clientset,
) func() bool {
	dataPlaneClient := operatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneName.Namespace)
	return func() bool {
		dataplane, err := dataPlaneClient.Get(ctx, dataplaneName.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return predicate(dataplane)
	}
}

// HPAPredicate is a helper function for tests that returns a function
// that can be used to check if an HPA has a certain state.
func HPAPredicate(
	t *testing.T,
	ctx context.Context,
	hpaName types.NamespacedName,
	predicate func(hpa *autoscalingv2.HorizontalPodAutoscaler) bool,
	client client.Client,
) func() bool {
	return func() bool {
		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.NoError(t, client.Get(ctx, hpaName, &hpa))
		return predicate(&hpa)
	}
}

// ControlPlaneIsScheduled is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was scheduled.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsScheduled(t *testing.T, ctx context.Context, controlPlane types.NamespacedName, operatorClient *clientset.Clientset) func() bool {
	return controlPlanePredicate(t, ctx, controlPlane, func(c *gwtypes.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgcontrolplane.ConditionTypeProvisioned) {
				return true
			}
		}
		return false
	}, operatorClient)
}

// DataPlaneIsReady is a helper function for tests that returns a function
// that can be used to check if a DataPlane is ready.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneIsReady(t *testing.T, ctx context.Context, dataplane types.NamespacedName, operatorClient *clientset.Clientset) func() bool {
	return DataPlanePredicate(t, ctx, dataplane, func(c *operatorv1beta1.DataPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgdataplane.ReadyType) && condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, operatorClient)
}

// ControlPlaneDetectedNoDataPlane is a helper function for tests that returns a function
// that can be used to check if a ControlPlane detected unset dataplane.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneDetectedNoDataPlane(t *testing.T, ctx context.Context, controlPlane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlPlane, func(c *gwtypes.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgcontrolplane.ConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionFalse &&
				condition.Reason == string(kcfgcontrolplane.ConditionReasonNoDataPlane) {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsProvisioned is a helper function for tests that returns a function
// that can be used to check if a ControlPlane was provisioned.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsProvisioned(t *testing.T, ctx context.Context, controlPlane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlPlane, func(c *gwtypes.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgcontrolplane.ConditionTypeProvisioned) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsNotReady is a helper function for tests. It returns a function
// that can be used to check if a ControlPlane is marked as not Ready.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsNotReady(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *gwtypes.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgdataplane.ReadyType) &&
				condition.Status == metav1.ConditionFalse {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneIsReady is a helper function for tests. It returns a function
// that can be used to check if a ControlPlane is marked as Ready.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneIsReady(t *testing.T, ctx context.Context, controlplane types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplane, func(c *gwtypes.ControlPlane) bool {
		for _, condition := range c.Status.Conditions {
			if condition.Type == string(kcfgdataplane.ReadyType) &&
				condition.Status == metav1.ConditionTrue {
				return true
			}
		}
		return false
	}, clients.OperatorClient)
}

// ControlPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an active deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasActiveDeployment(t *testing.T, ctx context.Context, controlplaneName types.NamespacedName, clients K8sClients) func() bool {
	return controlPlanePredicate(t, ctx, controlplaneName, func(controlplane *gwtypes.ControlPlane) bool {
		deployments := MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas > 0 &&
			deployments[0].Status.AvailableReplicas == *deployments[0].Spec.Replicas
	}, clients.OperatorClient)
}

// ControlPlaneHasClusterRole is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a ClusterRole.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasClusterRole(t *testing.T, ctx context.Context, controlplane *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		clusterRoles := MustListControlPlaneClusterRoles(t, ctx, controlplane, clients)
		t.Logf("%d clusterroles", len(clusterRoles))
		return len(clusterRoles) > 0
	}
}

// ControlPlanesClusterRoleHasPolicyRule is a helper function for tests that returns a function
// that can be used to check if ControlPlane's ClusterRole contains the provided PolicyRule.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlanesClusterRoleHasPolicyRule(t *testing.T, ctx context.Context, controlplane *gwtypes.ControlPlane, clients K8sClients, pr rbacv1.PolicyRule) func() bool {
	return func() bool {
		clusterRoles := MustListControlPlaneClusterRoles(t, ctx, controlplane, clients)
		t.Logf("%d clusterroles", len(clusterRoles))
		if len(clusterRoles) == 0 {
			return false
		}
		t.Logf("got %s clusterrole, checking if it contains the requested PolicyRule", clusterRoles[0].Name)
		return slices.ContainsFunc(clusterRoles[0].Rules, func(e rbacv1.PolicyRule) bool {
			return slices.Equal(e.APIGroups, pr.APIGroups) &&
				slices.Equal(e.ResourceNames, pr.ResourceNames) &&
				slices.Equal(e.Resources, pr.Resources) &&
				slices.Equal(e.Verbs, pr.Verbs) &&
				slices.Equal(e.NonResourceURLs, pr.NonResourceURLs)
		})
	}
}

// ControlPlanesClusterRoleBindingHasSubject is a helper function for tests that returns a function
// that can be used to check if ControlPlane's ClusterRoleBinding contains the provided Subject.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlanesClusterRoleBindingHasSubject(t *testing.T, ctx context.Context, controlplane *gwtypes.ControlPlane, clients K8sClients, subject rbacv1.Subject) func() bool {
	return func() bool {
		clusterRoleBindings := MustListControlPlaneClusterRoleBindings(t, ctx, controlplane, clients)
		t.Logf("%d clusterrolesbindings", len(clusterRoleBindings))
		if len(clusterRoleBindings) == 0 {
			return false
		}
		t.Logf("got %s clusterrolebinding, checking if it contains the requested Subject", clusterRoleBindings[0].Name)
		return slices.ContainsFunc(clusterRoleBindings[0].Subjects, func(e rbacv1.Subject) bool {
			return e.Kind == subject.Kind &&
				e.APIGroup == subject.APIGroup &&
				e.Name == subject.Name &&
				e.Namespace == subject.Namespace
		})
	}
}

// ControlPlaneHasClusterRoleBinding is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has a ClusterRoleBinding.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasClusterRoleBinding(t *testing.T, ctx context.Context, controlplane *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		clusterRoleBindings := MustListControlPlaneClusterRoleBindings(t, ctx, controlplane, clients)
		t.Logf("%d clusterrolebindings", len(clusterRoleBindings))
		return len(clusterRoleBindings) > 0
	}
}

// ControlPlaneCRBContainsCRAndSA is a helper function for tests that returns a function
// that can be used to check if the ClusterRoleBinding of a ControPlane has the reference of ClusterRole belonging to the ControlPlane
// and contains the service account used by the Deployment of the ControlPlane.
func ControlPlaneCRBContainsCRAndSA(t *testing.T, ctx context.Context, controlplane *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		clusterRoleBindings := MustListControlPlaneClusterRoleBindings(t, ctx, controlplane, clients)
		clusterRoles := MustListControlPlaneClusterRoles(t, ctx, controlplane, clients)
		deployments := MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		if len(clusterRoleBindings) != 1 || len(clusterRoles) != 1 || len(deployments) != 1 {
			return false
		}
		clusterRoleBinding := clusterRoleBindings[0]
		clusterRole := clusterRoles[0]
		serviceAccountName := deployments[0].Spec.Template.Spec.ServiceAccountName
		return k8sresources.CompareClusterRoleName(&clusterRoleBinding, clusterRole.Name) &&
			k8sresources.ClusterRoleBindingContainsServiceAccount(&clusterRoleBinding, controlplane.Namespace, serviceAccountName)
	}
}

// ControlPlaneHasAdmissionWebhookService is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an admission webhook Service.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasAdmissionWebhookService(t *testing.T, ctx context.Context, cp *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		services, err := k8sutils.ListServicesForOwner(ctx, clients.MgrClient, cp.Namespace, cp.UID, client.MatchingLabels{
			consts.ControlPlaneServiceLabel: consts.ControlPlaneServiceKindWebhook,
		})
		require.NoError(t, err)
		t.Logf("%d webhook services", len(services))
		return len(services) > 0
	}
}

// ControlPlaneHasAdmissionWebhookCertificateSecret is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an admission webhook certificate Secret.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneHasAdmissionWebhookCertificateSecret(t *testing.T, ctx context.Context, cp *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		services, err := k8sutils.ListSecretsForOwner(ctx, clients.MgrClient, cp.UID, client.MatchingLabels{
			consts.SecretUsedByServiceLabel: consts.ControlPlaneServiceKindWebhook,
		})
		require.NoError(t, err)
		t.Logf("%d webhook secrets", len(services))
		return len(services) > 0
	}
}

// ControlPlaneHasAdmissionWebhookConfiguration is a helper function for tests that returns a function
// that can be used to check if a ControlPlane has an admission webhook configuration.
func ControlPlaneHasAdmissionWebhookConfiguration(t *testing.T, ctx context.Context, cp *gwtypes.ControlPlane, clients K8sClients) func() bool {
	return func() bool {
		managedByLabelSet := k8sutils.GetManagedByLabelSet(cp)
		configs, err := k8sutils.ListValidatingWebhookConfigurations(ctx, clients.MgrClient, client.MatchingLabels(managedByLabelSet))
		require.NoError(t, err)
		t.Logf("%d validating webhook configurations", len(configs))
		return len(configs) > 0
	}
}

// DataPlaneHasActiveDeployment is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active deployment (that is,
// a Deployment that has at least 1 Replica and that all Replicas as marked as Available).
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasActiveDeployment(
	t *testing.T,
	ctx context.Context,
	dataplaneNN types.NamespacedName,
	ret *appsv1.Deployment,
	matchingLabels client.MatchingLabels,
	clients K8sClients,
) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneNN, func(dataplane *operatorv1beta1.DataPlane) bool {
		deployments := MustListDataPlaneDeployments(t, ctx, dataplane, clients, matchingLabels)
		if len(deployments) == 1 &&
			deployments[0].Status.AvailableReplicas == *deployments[0].Spec.Replicas {
			if ret != nil {
				*ret = deployments[0]
			}
			return true
		}
		return false
	}, clients.OperatorClient)
}

// DataPlaneHasHPA is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active HPA.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasHPA(
	t *testing.T,
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
	ret *autoscalingv2.HorizontalPodAutoscaler,
	clients K8sClients,
) func() bool {
	dataplaneName := client.ObjectKeyFromObject(dataplane)
	const dataplaneDeploymentAppLabel = "app"

	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		deployments := MustListDataPlaneDeployments(t, ctx, dataplane, clients, client.MatchingLabels{
			dataplaneDeploymentAppLabel:          dataplane.Name,
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive, // Only live Deployment has an HPA.
		})
		if len(deployments) != 1 {
			return false
		}

		hpas := MustListDataPlaneHPAs(t, ctx, dataplane, clients, client.MatchingLabels{
			dataplaneDeploymentAppLabel:          dataplane.Name,
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		if len(hpas) != 1 {
			return false
		}

		hpa := hpas[0]
		if hpa.Spec.ScaleTargetRef.Name != deployments[0].Name {
			return false
		}

		if ret != nil {
			*ret = hpa
		}

		return true
	}, clients.OperatorClient)
}

// DataPlaneHasDeployment is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a Deployment.
// Optionally the caller can provide a list of assertions that will be checked
// against the found Deployment.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasDeployment(
	t *testing.T,
	ctx context.Context,
	dataplaneName types.NamespacedName,
	ret *appsv1.Deployment,
	clients K8sClients,
	matchingLabels client.MatchingLabels,
	asserts ...func(appsv1.Deployment) bool,
) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		deployments := MustListDataPlaneDeployments(t, ctx, dataplane, clients, matchingLabels)
		if len(deployments) != 1 {
			return false
		}
		deployment := deployments[0]
		for _, a := range asserts {
			if !a(deployment) {
				return false
			}
		}
		if ret != nil {
			*ret = deployment
		}
		return true
	}, clients.OperatorClient)
}

// DataPlaneHasNReadyPods checks if a DataPlane has at least N ready Pods.
func DataPlaneHasNReadyPods(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, clients K8sClients, n int32) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		deployments := MustListDataPlaneDeployments(t, ctx, dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		return len(deployments) == 1 &&
			*deployments[0].Spec.Replicas == n &&
			deployments[0].Status.AvailableReplicas == *deployments[0].Spec.Replicas
	}, clients.OperatorClient)
}

// DataPlaneHasService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a service created.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasService(
	t *testing.T,
	ctx context.Context,
	dataplaneName types.NamespacedName,
	clients K8sClients,
	matchingLabels client.MatchingLabels,
	asserts ...func(corev1.Service) bool,
) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, matchingLabels)
		if len(services) != 1 {
			return false
		}
		for _, a := range asserts {
			if !a(services[0]) {
				return false
			}
		}

		return true
	}, clients.OperatorClient)
}

// DataPlaneHasActiveService is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active proxy service.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasActiveService(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, ret *corev1.Service, clients K8sClients, matchingLabels client.MatchingLabels) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, matchingLabels)
		if len(services) == 1 {
			if ret != nil {
				*ret = services[0]
			}
			return true
		}
		return false
	}, clients.OperatorClient)
}

// DataPlaneHasActiveServiceWithName is a helper function for tests that returns a function
// that can be used to check if a DataPlane has an active proxy service with the specified name.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasActiveServiceWithName(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, ret *corev1.Service, clients K8sClients, matchingLabels client.MatchingLabels, name string) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, matchingLabels)
		if len(services) == 1 && services[0].Name == name {
			if ret != nil {
				*ret = services[0]
			}
			return true
		}
		return false
	}, clients.OperatorClient)
}

// DataPlaneServiceHasNActiveEndpoints is a helper function for tests that returns a function
// that can be used to check if a Service has active endpoints.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneServiceHasNActiveEndpoints(t *testing.T, ctx context.Context, serviceName types.NamespacedName, clients K8sClients, n int) func() bool {
	return func() bool {
		endpointSlices := MustListServiceEndpointSlices(
			t,
			ctx,
			serviceName,
			clients.MgrClient,
		)
		if len(endpointSlices) != 1 {
			return false
		}
		return len(endpointSlices[0].Endpoints) == n
	}
}

// DataPlaneHasServiceAndAddressesInStatus is a helper function for tests that returns
// a function that can be used to check if a DataPlane has:
// - a backing service name in its .Service status field
// - a list of addreses of its backing service in its .Addresses status field
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasServiceAndAddressesInStatus(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, clients K8sClients) func() bool {
	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		services := MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		})
		if len(services) != 1 {
			return false
		}
		service := services[0]
		if dataplane.Status.Service != service.Name {
			t.Logf("DataPlane %q: found %q as backing service, wanted %q",
				dataplane.Name, dataplane.Status.Service, service.Name,
			)
			return false
		}

		var wanted []string
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			if ingress.IP != "" {
				wanted = append(wanted, ingress.IP)
			}
			if ingress.Hostname != "" {
				wanted = append(wanted, ingress.Hostname)
			}
		}
		wanted = append(wanted, service.Spec.ClusterIPs...)

		var addresses []string
		for _, addr := range dataplane.Status.Addresses {
			addresses = append(addresses, addr.Value)
		}

		if len(addresses) != len(wanted) {
			t.Logf("DataPlane %q: found %d addresses %v, wanted %d %v",
				dataplane.Name, len(addresses), addresses, len(wanted), wanted,
			)
			return false
		}

		if !cmp.Equal(addresses, wanted) {
			t.Logf("DataPlane %q: found addresses %v, wanted %v",
				dataplane.Name, addresses, wanted,
			)
			return false
		}

		return true
	}, clients.OperatorClient)
}

// DataPlaneUpdateEventually is a helper function for tests that returns a function
// that can be used to update the DataPlane.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneUpdateEventually(t *testing.T, ctx context.Context, dataplaneNN types.NamespacedName, clients K8sClients, updateFunc func(*operatorv1beta1.DataPlane)) func() bool {
	return func() bool {
		cl := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneNN.Namespace)
		dp, err := cl.Get(ctx, dataplaneNN.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("error getting dataplane: %v", err)
			return false
		}

		updateFunc(dp)

		_, err = cl.Update(ctx, dp, metav1.UpdateOptions{})
		if err != nil {
			t.Logf("error updating dataplane: %v", err)
			return false
		}
		return true
	}
}

// HTTPRouteUpdateEventually is a helper function for tests that returns a function
// that can be used to update the HTTPRoute.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func HTTPRouteUpdateEventually(t *testing.T, ctx context.Context, httpRouteNN types.NamespacedName, clients K8sClients, updateFunc func(*gatewayv1.HTTPRoute)) func() bool {
	return func() bool {
		cl := clients.GatewayClient.GatewayV1().HTTPRoutes(httpRouteNN.Namespace)
		dp, err := cl.Get(ctx, httpRouteNN.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("error getting HTTPRoute: %v", err)
			return false
		}

		updateFunc(dp)

		_, err = cl.Update(ctx, dp, metav1.UpdateOptions{})
		if err != nil {
			t.Logf("error updating HTTPRoute: %v", err)
			return false
		}
		return true
	}
}

// ControlPlaneUpdateEventually is a helper function for tests that returns a function
// that can be used to update the ControlPlane.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func ControlPlaneUpdateEventually(t *testing.T, ctx context.Context, controlplaneNN types.NamespacedName, clients K8sClients, updateFunc func(*gwtypes.ControlPlane)) func() bool {
	return func() bool {
		cl := clients.OperatorClient.GatewayOperatorV2beta1().ControlPlanes(controlplaneNN.Namespace)
		cp, err := cl.Get(ctx, controlplaneNN.Name, metav1.GetOptions{})
		if err != nil {
			t.Logf("error getting controlplane: %v", err)
			return false
		}

		updateFunc(cp)

		_, err = cl.Update(ctx, cp, metav1.UpdateOptions{})
		if err != nil {
			t.Logf("error updating controlplane: %v", err)
			return false
		}
		return true
	}
}

// DataPlaneHasServiceSecret checks if a DataPlane's Service has one owned Secret.
func DataPlaneHasServiceSecret(t *testing.T, ctx context.Context, dpNN, usingSvc types.NamespacedName, ret *corev1.Secret, clients K8sClients) func() bool {
	return DataPlanePredicate(t, ctx, dpNN, func(dp *operatorv1beta1.DataPlane) bool {
		secrets, err := k8sutils.ListSecretsForOwner(ctx, clients.MgrClient, dp.GetUID(), client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.ServiceSecretLabel:            usingSvc.Name,
		})
		if err != nil {
			t.Logf("error listing secrets: %v", err)
			return false
		}
		if len(secrets) == 1 {
			*ret = secrets[0]
			return true
		}
		return false
	}, clients.OperatorClient)
}

// PodDisruptionBudgetRequirement is a function type used to check if a PodDisruptionBudget meets a certain requirement.
type PodDisruptionBudgetRequirement func(policyv1.PodDisruptionBudget) bool

// AnyPodDisruptionBudget returns a function that accepts any PodDisruptionBudget.
func AnyPodDisruptionBudget() PodDisruptionBudgetRequirement {
	return func(policyv1.PodDisruptionBudget) bool {
		return true
	}
}

// DataPlaneHasPodDisruptionBudget is a helper function for tests that returns a function
// that can be used to check if a DataPlane has a PodDisruptionBudget. It expects there is
// only a single PodDisruptionBudget for the DataPlane with the following requirements:
// - it is owned by the DataPlane,
// - its `app` label matches the DP name,
// - its `gateway-operator.konghq.com/managed-by` label is set to `dataplane`.
// Additionally, the caller can provide a requirement function that will be used to verify
// the PodDisruptionBudget (e.g. to check if it has an expected status).
// Should be used in conjunction with require.Eventually or assert.Eventually.
func DataPlaneHasPodDisruptionBudget(
	t *testing.T,
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
	ret *policyv1.PodDisruptionBudget,
	clients K8sClients,
	req PodDisruptionBudgetRequirement,
) func() bool {
	dataplaneName := client.ObjectKeyFromObject(dataplane)
	const dataplaneDeploymentAppLabel = "app"

	return DataPlanePredicate(t, ctx, dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
		pdbs := MustListDataPlanePodDisruptionBudgets(t, ctx, dataplane, clients, client.MatchingLabels{
			dataplaneDeploymentAppLabel:          dataplane.Name,
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		if len(pdbs) != 1 {
			return false
		}

		pdb := pdbs[0]
		if !req(pdb) {
			return false
		}

		if ret != nil {
			*ret = pdb
		}

		return true
	}, clients.OperatorClient)
}

// GatewayClassIsAccepted is a helper function for tests that returns a function
// that can be used to check if a GatewayClass is accepted.
// Should be used in conjunction with require.Eventually or assert.Eventually.
func GatewayClassIsAccepted(t *testing.T, ctx context.Context, gatewayClassName string, clients K8sClients) func() bool {
	gatewayClasses := clients.GatewayClient.GatewayV1().GatewayClasses()

	return func() bool {
		gwc, err := gatewayClasses.Get(t.Context(), gatewayClassName, metav1.GetOptions{})
		if err != nil {
			return false
		}
		for _, cond := range gwc.Status.Conditions {
			if cond.Reason == string(gatewayv1.GatewayClassConditionStatusAccepted) {
				if cond.ObservedGeneration == gwc.Generation {
					return true
				}
			}
		}
		return false
	}
}

// GatewayNotExist is a helper function for tests that returns a function
// to check a if gateway(with specified namespace and name) does not exist.
//
//	Should be used in conjunction with require.Eventually or assert.Eventually.
func GatewayNotExist(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		gateways := clients.GatewayClient.GatewayV1().Gateways(gatewayNSN.Namespace)
		_, err := gateways.Get(ctx, gatewayNSN.Name, metav1.GetOptions{})
		if err != nil {
			return errors.IsNotFound(err)
		}
		return false
	}
}

// GatewayIsAccepted returns a function that checks if a Gateway is scheduled.
func GatewayIsAccepted(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		return gatewayutils.IsAccepted(MustGetGateway(t, ctx, gatewayNSN, clients))
	}
}

// GatewayIsProgrammed returns a function that checks if a Gateway is programmed.
func GatewayIsProgrammed(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		return gatewayutils.IsProgrammed(MustGetGateway(t, ctx, gatewayNSN, clients))
	}
}

// GatewayListenersAreProgrammed returns a function that checks if a Gateway's listeners are programmed.
func GatewayListenersAreProgrammed(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		return gatewayutils.AreListenersProgrammed(MustGetGateway(t, ctx, gatewayNSN, clients))
	}
}

// GatewayDataPlaneIsReady returns a function that checks if a Gateway's DataPlane is ready.
func GatewayDataPlaneIsReady(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) func() bool {
	return func() bool {
		dataplanes := MustListDataPlanesForGateway(t, ctx, gateway, clients)

		if len(dataplanes) == 1 {
			// if the dataplane DeletionTimestamp is set, the dataplane deletion has been requested.
			// Hence we cannot consider it as a valid dataplane that's ready.
			if dataplanes[0].DeletionTimestamp != nil {
				return false
			}
			for _, condition := range dataplanes[0].Status.Conditions {
				if condition.Type == string(kcfgdataplane.ReadyType) &&
					condition.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}
}

// GatewayControlPlaneIsProvisioned returns a function that checks if a Gateway's ControlPlane is provisioned.
func GatewayControlPlaneIsProvisioned(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) func() bool {
	return func() bool {
		controlPlanes := MustListControlPlanesForGateway(t, ctx, gateway, clients)

		if len(controlPlanes) == 1 {
			// if the controlplane DeletionTimestamp is set, the controlplane deletion has been requested.
			// Hence we cannot consider it as a provisioned valid controlplane.
			if controlPlanes[0].DeletionTimestamp != nil {
				return false
			}
			for _, condition := range controlPlanes[0].Status.Conditions {
				if condition.Type == string(kcfgcontrolplane.ConditionTypeProvisioned) &&
					condition.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}
}

// GatewayNetworkPoliciesExist is a helper function for tests that returns a function
// that can be used to check if a Gateway owns a networkpolicy.
// Should be used in conjunction with require.Eventually or assert.Eventually.
// Gateway object argument does need to exist in the cluster, thus, the function
// may be used with Not after the gateway has been deleted, to verify that
// the networkpolicy has been deleted too.
func GatewayNetworkPoliciesExist(t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients) func() bool {
	return func() bool {
		networkpolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		return len(networkpolicies) > 0
	}
}

type ingressRuleT interface {
	netv1.NetworkPolicyIngressRule | netv1.NetworkPolicyEgressRule
}

// GatewayNetworkPolicyForGatewayContainsRules is a helper function for tets that
// returns a function that can be used to check if exactly 1 NetworkPolicy exist
// for Gateway and if it contains all the provided rules.
func GatewayNetworkPolicyForGatewayContainsRules[T ingressRuleT](t *testing.T, ctx context.Context, gateway *gwtypes.Gateway, clients K8sClients, rules ...T) func() bool {
	return func() bool {
		networkpolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}

		if len(networkpolicies) != 1 {
			return false
		}

		netpol := networkpolicies[0]
		for _, rule := range rules {
			switch r := any(rule).(type) {
			case netv1.NetworkPolicyIngressRule:
				if !networkPolicyRuleSliceContainsRule(netpol.Spec.Ingress, r) {
					return false
				}
			case netv1.NetworkPolicyEgressRule:
				if !networkPolicyRuleSliceContainsRule(netpol.Spec.Egress, r) {
					return false
				}
			default:
				t.Logf("NetworkPolicy rule has an unknown type %T", rule)
			}
		}
		return true
	}
}

func networkPolicyRuleSliceContainsRule[T ingressRuleT](rules []T, rule T) bool {
	for _, r := range rules {
		if cmp.Equal(r, rule) {
			return true
		}
	}

	return false
}

// GatewayIPAddressExist checks if a Gateway has IP addresses.
func GatewayIPAddressExist(t *testing.T, ctx context.Context, gatewayNSN types.NamespacedName, clients K8sClients) func() bool {
	return func() bool {
		gateway := MustGetGateway(t, ctx, gatewayNSN, clients)
		if len(gateway.Status.Addresses) > 0 && *gateway.Status.Addresses[0].Type == gatewayv1.IPAddressType {
			return true
		}
		return false
	}
}

// GatewayClassHasSupportedFeatures checks if a GatewayClass has the expected supported features.
func GatewayClassHasSupportedFeatures(t *testing.T, ctx context.Context, gatewayClassName string, clients K8sClients, requiredFeatures ...features.FeatureName) func() bool {
	return func() bool {
		gatewayClass := MustGetGatewayClass(t, ctx, gatewayClassName, clients)
		supportedFeatures := lo.Map(gatewayClass.Status.SupportedFeatures, func(f gatewayv1.SupportedFeature, _ int) features.FeatureName {
			return features.FeatureName(f.Name)
		})
		slices.Sort(supportedFeatures)
		slices.Sort(requiredFeatures)

		return slices.Equal(supportedFeatures, requiredFeatures)
	}
}

// GetResponseBodyContains issues an HTTP request and checks if a response body contains a string.
func GetResponseBodyContains(t *testing.T, clients K8sClients, httpc *http.Client, request *http.Request, responseContains string) func() bool {
	return func() bool {
		resp, err := httpc.Do(request)
		if err != nil {
			return false
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		return strings.Contains(string(body), responseContains)
	}
}

// Not is a helper function for tests that returns a negation of a predicate.
func Not(predicate func() bool) func() bool {
	return func() bool {
		return !predicate()
	}
}

// GetDataPlaneReplicaSets returns all ReplicaSets owned by the DataPlane's deployment.
// Returns nil if the deployment or replicasets cannot be found.
func GetDataPlaneReplicaSets(
	ctx context.Context,
	cli client.Client,
	dp *operatorv1beta1.DataPlane,
) ([]*appsv1.ReplicaSet, error) {
	// First find the deployment for this dataplane
	deployList := &appsv1.DeploymentList{}
	if err := cli.List(ctx, deployList,
		client.InNamespace(dp.Namespace),
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}); err != nil {
		return nil, err
	}

	if len(deployList.Items) != 1 {
		return nil, nil
	}
	deploy := &deployList.Items[0]

	// Find replicasets owned by this deployment
	rsList := &appsv1.ReplicaSetList{}
	if err := cli.List(ctx, rsList,
		client.InNamespace(dp.Namespace),
		client.MatchingLabels(deploy.Spec.Selector.MatchLabels),
	); err != nil {
		return nil, err
	}

	var ownedRS []*appsv1.ReplicaSet
	for i := range rsList.Items {
		rs := &rsList.Items[i]
		for _, owner := range rs.OwnerReferences {
			if owner.UID == deploy.UID {
				ownedRS = append(ownedRS, rs)
				break
			}
		}
	}

	return ownedRS, nil
}
