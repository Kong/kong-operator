package reduce

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
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
		t.Run(tc.name, func(t *testing.T) {
			filteredSecrets := filterSecrets(tc.secrets)
			require.Equal(t, tc.filteredSecrets, filteredSecrets)
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
	}

	for _, tc := range testCases {
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
						Name: "service0",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "service2",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filteredServices := filterServices(tc.services, tc.endpointSlices)
			require.Equal(t, tc.filteredServices, filteredServices)
		})
	}
}

func TestFilterHPA(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name          string
		hpas          []autoscalingv2.HorizontalPodAutoscaler
		expectedNames []string
	}{
		{
			name: "the newer ones must be returned to be deleted",
			hpas: []autoscalingv2.HorizontalPodAutoscaler{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "older",
						CreationTimestamp: metav1.NewTime(now.Add(-time.Second)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "newer",
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "newer-2",
						CreationTimestamp: metav1.NewTime(now.Add(time.Second)),
					},
				},
			},
			expectedNames: []string{"newer", "newer-2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := FilterHPAs(tc.hpas)
			filteredNames := lo.Map(filtered, func(hpa autoscalingv2.HorizontalPodAutoscaler, _ int) string {
				return hpa.Name
			})
			require.ElementsMatch(t, filteredNames, tc.expectedNames)
		})
	}
}

func TestFilterPodDisruptionBudgets(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name          string
		pdbs          []policyv1.PodDisruptionBudget
		expectedNames []string
	}{
		{
			name: "the newer ones must be returned to be deleted",
			pdbs: []policyv1.PodDisruptionBudget{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "older",
						CreationTimestamp: metav1.NewTime(now.Add(-time.Second)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "newer",
						CreationTimestamp: metav1.NewTime(now),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "newer-2",
						CreationTimestamp: metav1.NewTime(now.Add(time.Second)),
					},
				},
			},
			expectedNames: []string{"newer", "newer-2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := FilterPodDisruptionBudgets(tc.pdbs)
			filteredNames := lo.Map(filtered, func(pdb policyv1.PodDisruptionBudget, _ int) string {
				return pdb.Name
			})
			require.ElementsMatch(t, filteredNames, tc.expectedNames)
		})
	}
}

func TestFilterKongPluginBindings(t *testing.T) {
	testCases := []struct {
		name         string
		kpbs         []configurationv1alpha1.KongPluginBinding
		filteredKpbs []configurationv1alpha1.KongPluginBinding
	}{
		{
			name: "the Programmed binding must be filtered out regardless of the creation timestamp",
			kpbs: []configurationv1alpha1.KongPluginBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
					Status: configurationv1alpha1.KongPluginBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Programmed",
								Status: "True",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
			filteredKpbs: []configurationv1alpha1.KongPluginBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "12/31/1995",
						CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
		{
			name: "the Programmed binding must be filtered out",
			kpbs: []configurationv1alpha1.KongPluginBinding{
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
					Status: configurationv1alpha1.KongPluginBindingStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Programmed",
								Status: "True",
							},
						},
					},
				},
			},
			filteredKpbs: []configurationv1alpha1.KongPluginBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
		{
			name: "the oldest binding must be filtered out if it's not Programmed",
			kpbs: []configurationv1alpha1.KongPluginBinding{
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
			filteredKpbs: []configurationv1alpha1.KongPluginBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "1/1/2000",
						CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filteredDeployments := filterKongPluginBindings(tc.kpbs)
			require.Equal(t, tc.filteredKpbs, filteredDeployments)
		})
	}
}

func TestFilterKongCredentials(t *testing.T) {
	t.Run("BasicAuth", func(t *testing.T) {
		testCases := []struct {
			name          string
			creds         []configurationv1alpha1.KongCredentialBasicAuth
			expectedCreds []configurationv1alpha1.KongCredentialBasicAuth
		}{
			{
				name: "the Programmed credential must be filtered out regardless of the creation timestamp",
				creds: []configurationv1alpha1.KongCredentialBasicAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "1/1/2000",
							CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						},
						Status: configurationv1alpha1.KongCredentialBasicAuthStatus{
							Conditions: []metav1.Condition{
								{
									Type:   "Programmed",
									Status: "True",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "12/31/1995",
							CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						},
					},
				},
				expectedCreds: []configurationv1alpha1.KongCredentialBasicAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "12/31/1995",
							CreationTimestamp: metav1.Date(1995, time.December, 31, 0, 0, 0, 0, time.UTC),
						},
					},
				},
			},
			{
				name: "the Programmed credential must be filtered out",
				creds: []configurationv1alpha1.KongCredentialBasicAuth{
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
						Status: configurationv1alpha1.KongCredentialBasicAuthStatus{
							Conditions: []metav1.Condition{
								{
									Type:   "Programmed",
									Status: "True",
								},
							},
						},
					},
				},
				expectedCreds: []configurationv1alpha1.KongCredentialBasicAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "1/1/2000",
							CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						},
					},
				},
			},
			{
				name: "the oldest credential must be filtered out if it's not Programmed",
				creds: []configurationv1alpha1.KongCredentialBasicAuth{
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
				expectedCreds: []configurationv1alpha1.KongCredentialBasicAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:              "1/1/2000",
							CreationTimestamp: metav1.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
						},
					},
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				filteredCreds := filterKongCredentials(tc.creds)
				require.Equal(t, tc.expectedCreds, filteredCreds)
			})
		}
	})
}
