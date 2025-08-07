package controlplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestEnsureWatchNamespaceGrantsForNamespace(t *testing.T) {
	tests := []struct {
		name                string
		namespace           string
		controlPlane        *ControlPlane
		existingGrants      []client.Object
		expectedError       bool
		expectedErrorString string
	}{
		{
			name:      "grant exists for controlplane",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:      "no grants exist in namespace",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants:      []client.Object{},
			expectedError:       true,
			expectedErrorString: "WatchNamespaceGrant in Namespace target-ns to ControlPlane in Namespace cp-ns not found",
		},
		{
			name:      "grant exists but for different namespace",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "ControlPlane",
								Namespace: "different-ns",
							},
						},
					},
				},
			},
			expectedError:       true,
			expectedErrorString: "WatchNamespaceGrant in Namespace target-ns to ControlPlane in Namespace cp-ns not found",
		},
		{
			name:      "grant exists but for different kind",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "DataPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			expectedError:       true,
			expectedErrorString: "WatchNamespaceGrant in Namespace target-ns to ControlPlane in Namespace cp-ns not found",
		},
		{
			name:      "grant exists but for different group",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "different.group",
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			expectedError:       true,
			expectedErrorString: "WatchNamespaceGrant in Namespace target-ns to ControlPlane in Namespace cp-ns not found",
		},
		{
			name:      "multiple grants exist, one matches",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "different.group",
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-2",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name:      "grant with multiple from entries, one matches",
			namespace: "target-ns",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
			},
			existingGrants: []client.Object{
				&operatorv1alpha1.WatchNamespaceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
						From: []operatorv1alpha1.WatchNamespaceGrantFrom{
							{
								Group:     "different.group",
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
							{
								Group:     operatorv1beta1.SchemeGroupVersion.Group,
								Kind:      "ControlPlane",
								Namespace: "cp-ns",
							},
						},
					},
				},
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tt.existingGrants...).
				Build()

			err := ensureWatchNamespaceGrantsForNamespace(t.Context(), cl, tt.controlPlane, tt.namespace)

			if tt.expectedError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrorString)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateWatchNamespaces(t *testing.T) {
	tests := []struct {
		name            string
		controlPlane    *ControlPlane
		watchNamespaces []string
		expectedError   bool
		errorContains   string
	}{
		{
			name: "nil watchNamespaces spec - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: nil,
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2"},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeAll - empty operator watchNamespaces - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeAll,
						},
					},
				},
			},
			watchNamespaces: []string{},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeAll - non-empty operator watchNamespaces - should fail",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeAll,
						},
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2"},
			expectedError:   true,
			errorContains:   "ControlPlane's watchNamespaces is set to 'All', but operator is only allowed on: [ns1 ns2]",
		},
		{
			name: "WatchNamespacesTypeOwn - operator allows own namespace - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeOwn,
						},
					},
				},
			},
			watchNamespaces: []string{"cp-ns", "other-ns"},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeOwn - empty operator watchNamespaces - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeOwn,
						},
					},
				},
			},
			watchNamespaces: []string{},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeOwn - operator doesn't allow own namespace - should fail",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeOwn,
						},
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2"},
			expectedError:   true,
			errorContains:   "ControlPlane's watchNamespaces is set to 'Own' (current ControlPlane namespace: cp-ns), but operator is only allowed on: [ns1 ns2]",
		},
		{
			name: "WatchNamespacesTypeList - empty operator watchNamespaces - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeList,
							List: []string{"ns1", "ns2"},
						},
					},
				},
			},
			watchNamespaces: []string{},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeList - all requested namespaces allowed - should pass",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeList,
							List: []string{"ns1", "ns2"},
						},
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2", "ns3"},
			expectedError:   false,
		},
		{
			name: "WatchNamespacesTypeList - partial match - should fail",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeList,
							List: []string{"ns1", "ns2", "ns3"},
						},
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2"},
			expectedError:   true,
			errorContains:   "ControlPlane's watchNamespaces requests [ns1 ns2 ns3], but operator is only allowed on: [ns1 ns2]",
		},
		{
			name: "WatchNamespacesTypeList - no match - should fail",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeList,
							List: []string{"ns1", "ns2"},
						},
					},
				},
			},
			watchNamespaces: []string{"ns3", "ns4"},
			expectedError:   true,
			errorContains:   "ControlPlane's watchNamespaces requests [ns1 ns2], but operator is only allowed on: [ns3 ns4]",
		},
		{
			name: "WatchNamespacesTypeList - single namespace not allowed - should fail",
			controlPlane: &ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cp",
					Namespace: "cp-ns",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						WatchNamespaces: &operatorv2beta1.WatchNamespaces{
							Type: operatorv2beta1.WatchNamespacesTypeList,
							List: []string{"forbidden-ns"},
						},
					},
				},
			},
			watchNamespaces: []string{"ns1", "ns2"},
			expectedError:   true,
			errorContains:   "ControlPlane's watchNamespaces requests [forbidden-ns], but operator is only allowed on: [ns1 ns2]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWatchNamespaces(tt.controlPlane, tt.watchNamespaces)

			if tt.expectedError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}
