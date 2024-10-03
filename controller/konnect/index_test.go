package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/controller/konnect/constraints"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestControlPlaneKonnectNamespacedRefAsSlice(t *testing.T) {
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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

		testControlPlaneKonnectNamespacedRefAsSlice(t, tests)
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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
						ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
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

		testControlPlaneKonnectNamespacedRefAsSlice(t, tests)
	})
}

func testControlPlaneKonnectNamespacedRefAsSlice[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	tests []struct {
		name     string
		ent      TEnt
		expected []string
	},
) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := controlPlaneKonnectNamespacedRefAsSlice(tt.ent)
			assert.Equal(t, tt.expected, result)
		})
	}
}
