package reduce_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
)

func TestReduceClusterRoleBindings(t *testing.T) {
	testCases := []struct {
		name                   string
		clusterRoleBindings    []rbacv1.ClusterRoleBinding
		expectedDeletedCount   int
		expectedRemainingNames []string
	}{
		{
			name:                   "empty list returns no error",
			clusterRoleBindings:    []rbacv1.ClusterRoleBinding{},
			expectedDeletedCount:   0,
			expectedRemainingNames: []string{},
		},
		{
			name: "single ClusterRoleBinding is preserved",
			clusterRoleBindings: []rbacv1.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "crb-1",
					},
				},
			},
			expectedDeletedCount:   0,
			expectedRemainingNames: []string{"crb-1"},
		},
		{
			name: "multiple ClusterRoleBindings with different creation timestamps - oldest is preserved",
			clusterRoleBindings: []rbacv1.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-1",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-2",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-3",
						CreationTimestamp: metav1.NewTime(time.Date(2022, 12, 30, 0, 0, 0, 0, time.UTC)),
					},
				},
			},
			expectedDeletedCount:   2,
			expectedRemainingNames: []string{"crb-3"},
		},
		{
			name: "multiple ClusterRoleBindings, one with managed by label remains",
			clusterRoleBindings: []rbacv1.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-1",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-2",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-3",
						CreationTimestamp: metav1.NewTime(time.Date(2022, 12, 30, 0, 0, 0, 0, time.UTC)),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
			},
			expectedDeletedCount:   2,
			expectedRemainingNames: []string{"crb-3"},
		},
		{
			name: "multiple ClusterRoleBindings, one with managed by label remains",
			clusterRoleBindings: []rbacv1.ClusterRoleBinding{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-1",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-2",
						CreationTimestamp: metav1.NewTime(time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "crb-3",
						CreationTimestamp: metav1.NewTime(time.Date(2022, 12, 30, 0, 0, 0, 0, time.UTC)),
						Labels: map[string]string{
							consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
						},
					},
				},
			},
			expectedDeletedCount:   2,
			expectedRemainingNames: []string{"crb-3"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{}
			for i := range tc.clusterRoleBindings {
				objs = append(objs, &tc.clusterRoleBindings[i])
			}

			cl := fake.NewClientBuilder().WithObjects(objs...).Build()
			err := reduce.ReduceClusterRoleBindings(t.Context(), cl, tc.clusterRoleBindings)
			require.NoError(t, err)

			// Verify which ClusterRoleBindings remain
			var remainingBindings rbacv1.ClusterRoleBindingList
			require.NoError(t, cl.List(t.Context(), &remainingBindings))

			require.Len(t, remainingBindings.Items, len(tc.expectedRemainingNames))

			// Check the expected ClusterRoleBinding(s) remain
			for _, name := range tc.expectedRemainingNames {
				remaining := false
				for _, crb := range remainingBindings.Items {
					if crb.Name == name {
						remaining = true
						break
					}
				}
				assert.True(t, remaining, "Expected ClusterRoleBinding %s to remain but it was deleted", name)
			}

			// Also verify expected number of deleted ClusterRoleBindings
			assert.Equal(t, tc.expectedDeletedCount, len(tc.clusterRoleBindings)-len(remainingBindings.Items))
		})
	}
}
