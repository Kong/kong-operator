package controlplane

import (
	"testing"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestSpecDeepEqual(t *testing.T) {
	tests := []struct {
		name     string
		spec1    *gwtypes.ControlPlaneOptions
		spec2    *gwtypes.ControlPlaneOptions
		expected bool
	}{
		{
			name:     "both nil",
			spec1:    nil,
			spec2:    nil,
			expected: true,
		},
		{
			name:     "first nil, second not nil",
			spec1:    nil,
			spec2:    &gwtypes.ControlPlaneOptions{},
			expected: false,
		},
		{
			name:     "first not nil, second nil",
			spec1:    &gwtypes.ControlPlaneOptions{},
			spec2:    nil,
			expected: false,
		},
		{
			name:     "both empty",
			spec1:    &gwtypes.ControlPlaneOptions{},
			spec2:    &gwtypes.ControlPlaneOptions{},
			expected: true,
		},
		{
			name: "same controllers",
			spec1: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			expected: true,
		},
		{
			name: "different controllers order",
			spec1: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
					{
						Name:  "controller2",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller2",
						State: operatorv2beta1.ControllerStateEnabled,
					},
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			expected: false,
		},
		{
			name: "different controllers length",
			spec1: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller2",
						State: operatorv2beta1.ControllerStateEnabled,
					},
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			expected: false,
		},
		{
			name: "different controllers content",
			spec1: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
					{
						Name:  "controller3",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			expected: false,
		},
		{
			name: "one has controllers, other doesn't",
			spec1: &gwtypes.ControlPlaneOptions{
				Controllers: []gwtypes.ControlPlaneController{
					{
						Name:  "controller1",
						State: operatorv2beta1.ControllerStateEnabled,
					},
				},
			},
			spec2:    &gwtypes.ControlPlaneOptions{},
			expected: false,
		},
		{
			name: "same feature gates",
			spec1: &gwtypes.ControlPlaneOptions{
				FeatureGates: []gwtypes.ControlPlaneFeatureGate{
					{
						Name:  "feature1",
						State: operatorv2beta1.FeatureGateStateEnabled,
					},
				},
			},
			spec2: &gwtypes.ControlPlaneOptions{
				FeatureGates: []gwtypes.ControlPlaneFeatureGate{
					{
						Name:  "feature1",
						State: operatorv2beta1.FeatureGateStateEnabled,
					},
				},
			},
			expected: true,
		},
		{
			name: "one has feature gates, other doesn't",
			spec1: &gwtypes.ControlPlaneOptions{
				FeatureGates: []gwtypes.ControlPlaneFeatureGate{
					{
						Name:  "feature1",
						State: operatorv2beta1.FeatureGateStateEnabled,
					},
				},
			},
			spec2:    &gwtypes.ControlPlaneOptions{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SpecDeepEqual(tt.spec1, tt.spec2)
			if result != tt.expected {
				t.Errorf("SpecDeepEqual() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
