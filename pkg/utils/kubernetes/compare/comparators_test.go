package compare

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

func TestDataPlaneResourceOptionsDeepEqual(t *testing.T) {
	testCases := []struct {
		name   string
		opts1  *operatorv1beta1.DataPlaneResources
		opts2  *operatorv1beta1.DataPlaneResources
		expect bool
	}{
		{
			name:   "nil values are equal",
			opts1:  nil,
			opts2:  nil,
			expect: true,
		},
		{
			name:   "empty values are equal",
			opts1:  &operatorv1beta1.DataPlaneResources{},
			opts2:  &operatorv1beta1.DataPlaneResources{},
			expect: true,
		},
		{
			name: "different minAvailable implies different resources",
			opts1: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MinAvailable: lo.ToPtr(intstr.FromInt32(1)),
					},
				},
			},
			opts2: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MinAvailable: lo.ToPtr(intstr.FromInt32(2)),
					},
				},
			},
			expect: false,
		},
		{
			name: "different maxUnavailable implies different resources",
			opts1: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MaxUnavailable: lo.ToPtr(intstr.FromInt32(1)),
					},
				},
			},
			opts2: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MaxUnavailable: lo.ToPtr(intstr.FromInt32(2)),
					},
				},
			},
			expect: false,
		},
		{
			name: "same PDB specs are equal",
			opts1: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MinAvailable: lo.ToPtr(intstr.FromInt32(1)),
					},
				},
			},
			opts2: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MinAvailable: lo.ToPtr(intstr.FromInt32(1)),
					},
				},
			},
			expect: true,
		},
		{
			name:  "one nil and one non-nil are not equal",
			opts1: nil,
			opts2: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
					Spec: operatorv1beta1.PodDisruptionBudgetSpec{
						MinAvailable: lo.ToPtr(intstr.FromInt32(1)),
					},
				},
			},
			expect: false,
		},
		{
			name: "nil PDB and empty PDB are not equal",
			opts1: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: nil,
			},
			opts2: &operatorv1beta1.DataPlaneResources{
				PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{},
			},
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := DataPlaneResourceOptionsDeepEqual(tc.opts1, tc.opts2)
			require.Equal(t, tc.expect, result)
		})
	}
}
