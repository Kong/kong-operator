package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestGetAPIAuthRef_EventGatewayChildren(t *testing.T) {
	t.Run("KonnectEventDataPlaneCertificate", func(t *testing.T) {
		testGetAPIAuthRefForEventGatewayChild(t, func(ref commonv1alpha1.ObjectRef) *konnectv1alpha1.KonnectEventDataPlaneCertificate {
			return &konnectv1alpha1.KonnectEventDataPlaneCertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-cert",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectEventDataPlaneCertificateSpec{
					GatewayRef: ref,
				},
			}
		})
	})

	t.Run("EventGatewayBackendCluster", func(t *testing.T) {
		testGetAPIAuthRefForEventGatewayChild(t, func(ref commonv1alpha1.ObjectRef) *konnectv1alpha1.EventGatewayBackendCluster {
			return &konnectv1alpha1.EventGatewayBackendCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backend-cluster",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
					GatewayRef: ref,
				},
			}
		})
	})
}

func testGetAPIAuthRefForEventGatewayChild[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	builder func(commonv1alpha1.ObjectRef) TEnt,
) {
	t.Helper()

	tests := []struct {
		name      string
		ref       commonv1alpha1.ObjectRef
		expectErr bool
	}{
		{
			name: "namespaced ref",
			ref: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "event-gateway",
				},
			},
		},
		{
			name: "konnect id ref is unsupported",
			ref: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new("gateway-konnect-id"),
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(
					&konnectv1alpha1.KonnectAPIAuthConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "api-auth",
							Namespace: "default",
						},
					},
					&konnectv1alpha1.KonnectEventGateway{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "event-gateway",
							Namespace: "default",
						},
						Spec: konnectv1alpha1.KonnectEventGatewaySpec{
							KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
								APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
									Name: "api-auth",
								},
							},
						},
						Status: konnectv1alpha1.KonnectEventGatewayStatus{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "gateway-konnect-id",
							},
						},
					},
				).Build()

			ent := builder(tc.ref)
			nn, err := getAPIAuthRef(t.Context(), cl, ent)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.NamespacedName{Name: "api-auth", Namespace: "default"}, nn)
		})
	}
}

func TestGetAPIAuthRef_EventGatewayVirtualCluster(t *testing.T) {
	tests := []struct {
		name      string
		bcRef     commonv1alpha1.ObjectRef
		expectErr bool
	}{
		{
			name: "namespaced ref",
			bcRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "backend-cluster",
				},
			},
		},
		{
			name: "konnect id ref is unsupported",
			bcRef: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new("backend-cluster-konnect-id"),
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(
					&konnectv1alpha1.KonnectAPIAuthConfiguration{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "api-auth",
							Namespace: "default",
						},
					},
					&konnectv1alpha1.KonnectEventGateway{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "event-gateway",
							Namespace: "default",
						},
						Spec: konnectv1alpha1.KonnectEventGatewaySpec{
							KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
								APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
									Name: "api-auth",
								},
							},
						},
					},
					&konnectv1alpha1.EventGatewayBackendCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "backend-cluster",
							Namespace: "default",
						},
						Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
							GatewayRef: commonv1alpha1.ObjectRef{
								Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
								NamespacedRef: &commonv1alpha1.NamespacedRef{
									Name: "event-gateway",
								},
							},
						},
					},
				).Build()

			virtualCluster := &konnectv1alpha1.EventGatewayVirtualCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "event-virtual-cluster",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
					EventGatewayBackendClusterRef: tc.bcRef,
				},
			}

			nn, err := getAPIAuthRef(t.Context(), cl, virtualCluster)
			if tc.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, types.NamespacedName{Name: "api-auth", Namespace: "default"}, nn)
		})
	}
}
