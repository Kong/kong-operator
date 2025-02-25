package extensions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/pkg/consts"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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
								Kind:  konnectv1alpha1.KonnectExtensionKind,
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Status:  metav1.ConditionTrue,
				Type:    string(consts.AcceptedExtensionsType),
				Reason:  string(consts.AcceptedExtensionsReason),
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
				Type:               string(consts.AcceptedExtensionsType),
				Reason:             string(consts.NotSupportedExtensionsReason),
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
								Kind:  konnectv1alpha1.KonnectExtensionKind,
							},
							{
								Group: konnectv1alpha1.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha1.KonnectExtensionKind,
							},
						},
					},
				},
			},
			expected: &metav1.Condition{
				Status:  metav1.ConditionFalse,
				Type:    string(consts.AcceptedExtensionsType),
				Reason:  string(consts.NotSupportedExtensionsReason),
				Message: "Extension konnect.konghq.com/KonnectExtension is duplicated",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := ValidateExtensions(tt.dataplane)
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
