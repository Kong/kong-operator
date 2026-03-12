package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueKonnectEventGatewayForKonnectAPIAuthConfiguration(t *testing.T) {
	auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-auth",
			Namespace: "default",
		},
	}

	t.Run("non-KonnectAPIAuthConfiguration object returns nil", func(t *testing.T) {
		cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).Build()
		f := enqueueKonnectEventGatewayForKonnectAPIAuthConfiguration(cl)
		require.Nil(t, f(t.Context(), &konnectv1alpha1.KonnectEventGateway{}))
	})

	tests := []struct {
		name     string
		gateways []konnectv1alpha1.KonnectEventGateway
		expected []ctrl.Request
	}{
		{
			name:     "no gateways",
			gateways: nil,
			expected: nil,
		},
		{
			name: "single gateway references auth",
			gateways: []konnectv1alpha1.KonnectEventGateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{NamespacedName: types.NamespacedName{Name: "eg-1", Namespace: "default"}},
			},
		},
		{
			name: "multiple gateways only one references auth",
			gateways: []konnectv1alpha1.KonnectEventGateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "other-auth",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{NamespacedName: types.NamespacedName{Name: "eg-1", Namespace: "default"}},
			},
		},
		{
			name: "multiple gateways all reference auth",
			gateways: []konnectv1alpha1.KonnectEventGateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{NamespacedName: types.NamespacedName{Name: "eg-1", Namespace: "default"}},
				{NamespacedName: types.NamespacedName{Name: "eg-2", Namespace: "default"}},
			},
		},
		{
			name: "gateway in different namespace is not enqueued",
			gateways: []konnectv1alpha1.KonnectEventGateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eg-1",
						Namespace: "other-ns",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
								Name: "test-auth",
							},
						},
					},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get())
			for i := range tt.gateways {
				builder = builder.WithObjects(&tt.gateways[i])
			}
			for _, opt := range index.OptionsForKonnectEventGateway() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}
			cl := builder.Build()

			f := enqueueKonnectEventGatewayForKonnectAPIAuthConfiguration(cl)
			require.Equal(t, tt.expected, f(t.Context(), auth))
		})
	}
}
