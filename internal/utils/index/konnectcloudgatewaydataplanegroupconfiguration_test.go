package index

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/test/helpers/generate"
)

func Test_konnectCloudGatewayNetworkDataPlaneGroupConfigurationRef(t *testing.T) {
	tests := []struct {
		name string
		obj  client.Object
		want []string
	}{
		{
			name: "nil object returns nil",
			obj:  nil,
			want: nil,
		},
		{
			name: "wrong type returns nil",
			obj:  &konnectv1alpha1.KonnectCloudGatewayNetwork{},
			want: nil,
		},
		{
			name: "no dataplane groups returns empty slice",
			obj: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{},
			},
			want: []string{},
		},
		{
			name: "dataplane group with valid ref type returns name",
			obj: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
					DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "test-network"},
							},
						},
					},
				},
			},
			want: []string{"test-network"},
		},
		{
			name: "dataplane group with different ref type returns empty slice",
			obj: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
					DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type: "another-type",
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "test-network",
								},
							},
						},
					},
				},
			},
			want: []string{},
		},
		{
			name: "dataplane group with konnect ID network ref type returns empty slice",
			obj: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
					DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type:      commonv1alpha1.ObjectRefTypeKonnectID,
								KonnectID: lo.ToPtr(generate.KonnectID(t)),
							},
						},
					},
				},
			},
			want: []string{},
		},
		{
			name: "multiple groups return names only for valid ref type",
			obj: &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
				Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
					DataplaneGroups: []konnectv1alpha1.KonnectConfigurationDataPlaneGroup{
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "network-1",
								},
							},
						},
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type: "another-type",
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "network-2",
								},
							},
						},
						{
							NetworkRef: commonv1alpha1.ObjectRef{
								Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "network-3",
								},
							},
						},
					},
				},
			},
			want: []string{"network-1", "network-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := konnectCloudGatewayNetworkDataPlaneGroupConfigurationRef(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}
