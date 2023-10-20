package reduce

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/internal/consts"
)

func TestFilterSecrets(t *testing.T) {
	testCases := []struct {
		name            string
		secrets         []corev1.Secret
		filteredSecrets []corev1.Secret
	}{
		{
			name: "the older secret must be filtered out",
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "6/30/1990",
						CreationTimestamp: metav1.Date(1990, time.June, 30, 0, 0, 0, 0, time.UTC),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			filteredSecrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filteredSecrets := filterSecrets(tc.secrets)
			require.Equal(t, filteredSecrets, tc.filteredSecrets)
		})
	}
}

func TestFilterServiceAccounts(t *testing.T) {
	testCases := []struct {
		name                   string
		serviceAccount         []corev1.ServiceAccount
		filteredServiceAccount []corev1.ServiceAccount
	}{
		{
			name: "the older serviceAccount must be filtered out",
			serviceAccount: []corev1.ServiceAccount{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "6/30/1990",
						CreationTimestamp: metav1.Date(1990, time.June, 30, 0, 0, 0, 0, time.UTC),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			filteredServiceAccount: []corev1.ServiceAccount{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filteredSecrets := filterServiceAccounts(tc.serviceAccount)
			require.Equal(t, filteredSecrets, tc.filteredServiceAccount)
		})
	}
}

func TestFilterDeployments(t *testing.T) {
	testCases := []struct {
		name                string
		deployments         []appsv1.Deployment
		filteredDeployments []appsv1.Deployment
	}{
		{
			name: "the older deployment must be filtered out",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
		{
			name: "the deployment with more AvailableReplicas must be filtered out",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1-available-replicas",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-available-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-available-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
					},
				},
			},
		},
		{
			name: "the deployment with more ReadyReplicas must be filtered out",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1-ready-replicas",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     0,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     0,
					},
				},
			},
		},
		{
			name: "the deployment with legacy managed-by labels must be filtered out",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "no-labels",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
			},
		},
		{
			name: "the deployment with legacy managed-by labels must be filtered out even when it has more ready replicas",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     0,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     1,
					},
				},
			},
		},
		{
			name: "the deployment with legacy managed-by labels must be filtered out even when it has more ready replicas",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     0,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
		},
		{
			name: "deployments with legacy managed-by labels must be filtered out",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels-2",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "0-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 0,
						ReadyReplicas:     0,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1-ready-replicas",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels-2",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
		},
		{
			name: "if all deployments use legacy managed-by labels then return all but one so that it gets updated instead of deleted",
			deployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels-2",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
			filteredDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "legacy-managed-by-labels",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filteredDeployments := filterDeployments(tc.deployments)
			require.Equal(t, tc.filteredDeployments, filteredDeployments)
		})
	}
}

func TestFilterServices(t *testing.T) {
	testCases := []struct {
		name             string
		services         []corev1.Service
		endpointSlices   map[string][]discoveryv1.EndpointSlice
		filteredServices []corev1.Service
	}{
		{
			name: "service1 has more Loadbalancer addresses allocated than service 2",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "placeholderIP",
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
			filteredServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
		},
		{
			name: "service2 has more endpointSlices than service1",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
			endpointSlices: map[string][]discoveryv1.EndpointSlice{
				"service1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
				},
				"service2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice2",
						},
					},
				},
			},
			filteredServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
					},
				},
			},
		},
		{
			name: "service1 has more ready endpoints than service0 and service2",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
			endpointSlices: map[string][]discoveryv1.EndpointSlice{
				"service1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Conditions: discoveryv1.EndpointConditions{
									Ready: lo.ToPtr(true),
								},
							},
						},
					},
				},
				"service2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
				},
			},
			filteredServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
			},
		},
		{
			name: "service1 (legacy) is deleted regardless of anything else",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
			endpointSlices: map[string][]discoveryv1.EndpointSlice{
				"service1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Conditions: discoveryv1.EndpointConditions{
									Ready: lo.ToPtr(true),
								},
							},
						},
					},
				},
				"service2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
				},
			},
			filteredServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
			},
		},
		{
			name: "if all services are using legacy managed-by labels then return all but one so that it gets updated instead of deleted",
			services: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service0",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
			},
			endpointSlices: map[string][]discoveryv1.EndpointSlice{
				"service0": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Conditions: discoveryv1.EndpointConditions{
									Ready: lo.ToPtr(true),
								},
							},
						},
					},
				},
				"service1": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
				},
				"service2": {
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "endpointSlice1",
						},
					},
				},
			},
			filteredServices: []corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service0",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service1",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabelLegacy: "dataplane",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			filteredServices := filterServices(tc.services, tc.endpointSlices)
			require.Equal(t, filteredServices, tc.filteredServices)
		})
	}
}
