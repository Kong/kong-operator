package extensions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestValidateExtensions(t *testing.T) {
	tests := []struct {
		name      string
		dataplane *operatorv1beta1.DataPlane
		expected  *metav1.Condition
	}{
		{
			name: "no extensions",
			dataplane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{},
			},
			expected: nil,
		},
		{
			name: "all extensions accepted",
			dataplane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha1.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
							},
							{
								Group: operatorv1alpha1.SchemeGroupVersion.Group,
								Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Status:  metav1.ConditionTrue,
				Type:    string(kcfgkonnect.AcceptedExtensionsType),
				Reason:  string(kcfgkonnect.AcceptedExtensionsReason),
				Message: "All extensions are accepted",
			},
		},
		{
			name: "unsupported extension",
			dataplane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: "unsupported.group",
								Kind:  "UnsupportedKind",
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(kcfgkonnect.AcceptedExtensionsType),
				Reason:             string(kcfgkonnect.NotSupportedExtensionsReason),
				Message:            "Extension unsupported.group/UnsupportedKind is not supported",
				ObservedGeneration: 1,
			},
		},
		{
			name: "duplicated extension",
			dataplane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha1.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
							},
							{
								Group: konnectv1alpha1.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Status:  metav1.ConditionFalse,
				Type:    string(kcfgkonnect.AcceptedExtensionsType),
				Reason:  string(kcfgkonnect.NotSupportedExtensionsReason),
				Message: "Extension konnect.konghq.com/KonnectExtension is duplicated",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := validateExtensions(tt.dataplane)
			if tt.expected == nil {
				assert.Nil(t, condition)
			} else {
				assert.Equal(t, tt.expected.Status, condition.Status)
				assert.Equal(t, tt.expected.Type, condition.Type)
				assert.Equal(t, tt.expected.Reason, condition.Reason)
				assert.Equal(t, tt.expected.Message, condition.Message)
			}
		})
	}
}
