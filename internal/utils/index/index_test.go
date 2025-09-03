package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/apis/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/apis/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestIndexKonnectGatewayControlPlaneRef(t *testing.T) {
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectGatewayControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "cp-1",
		},
	}
	cl := fakeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(cp).
		Build()

	t.Run("KongService", func(t *testing.T) {
		tests := []struct {
			name     string
			ent      *configurationv1alpha1.KongService
			expected []string
		}{
			{
				name: "not specifying namespace is supported",
				ent: &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "cp-1",
							},
						},
					},
				},
				expected: []string{"default/cp-1"},
			},
			{
				name: "specifying the same namespace is supported",
				ent: &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "cp-1",
								Namespace: "default",
							},
						},
					},
				},
				expected: []string{"default/cp-1"},
			},
			{
				name: "cross namespace references not supported",
				ent: &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "cp-1",
								Namespace: "different",
							},
						},
					},
				},
				expected: nil,
			},
		}

		testIndexKonnectGatewayControlPlaneRef(t, cl, tests)
	})

	t.Run("KongRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			ent      *configurationv1alpha1.KongRoute
			expected []string
		}{
			{
				name: "not specifying namespace is supported",
				ent: &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "cp-1",
							},
						},
					},
				},
				expected: []string{"default/cp-1"},
			},
			{
				name: "specifying the same namespace is supported",
				ent: &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "cp-1",
								Namespace: "default",
							},
						},
					},
				},
				expected: []string{"default/cp-1"},
			},
			{
				name: "cross namespace references not supported",
				ent: &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "obj1",
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      "cp-1",
								Namespace: "different",
							},
						},
					},
				},
				expected: nil,
			},
		}

		testIndexKonnectGatewayControlPlaneRef(t, cl, tests)
	})
}

func testIndexKonnectGatewayControlPlaneRef[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	cl client.Client,
	tests []struct {
		name     string
		ent      TEnt
		expected []string
	},
) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexKonnectGatewayControlPlaneRef[T, TEnt](cl)(tt.ent)
			assert.Equal(t, tt.expected, result)
		})
	}
}
