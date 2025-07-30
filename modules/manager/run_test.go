package manager

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSetByObjectFor(t *testing.T) {
	t.Run("valid selector for Secret", func(t *testing.T) {
		byObject := make(map[client.Object]cache.ByObject)
		selector := "test-label"

		err := setByObjectFor[corev1.Secret](selector, byObject)
		require.NoError(t, err)

		// Verify the label selector was set correctly
		expectedReq, err := labels.NewRequirement(selector, selection.Equals, []string{"true"})
		require.NoError(t, err)
		expectedSelector := labels.NewSelector().Add(*expectedReq)

		var secret corev1.Secret
		require.Contains(t, byObject, &secret)
		require.True(
			t,
			func() bool {
				for obj, byObj := range byObject {
					s, ok := obj.(*corev1.Secret)
					if !ok || s == nil {
						continue
					}

					if !reflect.DeepEqual(expectedSelector, byObj.Label) {
						continue
					}

					return true
				}
				return false
			}(),
		)
	})

	t.Run("valid selector for ConfigMap", func(t *testing.T) {
		byObject := make(map[client.Object]cache.ByObject)
		selector := "config-label"

		err := setByObjectFor[corev1.ConfigMap](selector, byObject)
		require.NoError(t, err)

		var configMap corev1.ConfigMap
		require.Contains(t, byObject, &configMap)

		// Verify the label selector was set correctly
		expectedReq, err := labels.NewRequirement(selector, selection.Equals, []string{"true"})
		require.NoError(t, err)
		expectedSelector := labels.NewSelector().Add(*expectedReq)

		var cm corev1.ConfigMap
		require.Contains(t, byObject, &cm)
		require.True(
			t,
			func() bool {
				for obj, byObj := range byObject {
					s, ok := obj.(*corev1.ConfigMap)
					if !ok || s == nil {
						continue
					}

					if !reflect.DeepEqual(expectedSelector, byObj.Label) {
						continue
					}

					return true
				}
				return false
			}(),
		)
	})

	t.Run("invalid selector", func(t *testing.T) {
		byObject := make(map[client.Object]cache.ByObject)
		// Using an invalid label selector (contains invalid characters)
		selector := "_invalid"

		err := setByObjectFor[corev1.Secret](selector, byObject)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to make label requirement for secrets")

		// Verify nothing was added to byObject on error
		require.Empty(t, byObject)
	})

	t.Run("multiple objects with different selectors", func(t *testing.T) {
		byObject := make(map[client.Object]cache.ByObject)

		// Add Secret with one selector
		secretSelector := "secret-label"
		err := setByObjectFor[corev1.Secret](secretSelector, byObject)
		require.NoError(t, err)

		// Add ConfigMap with different selector
		configMapSelector := "configmap-label"
		err = setByObjectFor[corev1.ConfigMap](configMapSelector, byObject)
		require.NoError(t, err)

		// Verify both objects are in byObject
		require.Len(t, byObject, 2)

		require.Contains(t, byObject, (&corev1.Secret{}))
		require.Contains(t, byObject, (&corev1.ConfigMap{}))

		// Verify each has the correct selector
		secretReq, err := labels.NewRequirement(secretSelector, selection.Equals, []string{"true"})
		require.NoError(t, err)
		secretExpectedSelector := labels.NewSelector().Add(*secretReq)

		configMapReq, err := labels.NewRequirement(configMapSelector, selection.Equals, []string{"true"})
		require.NoError(t, err)
		configMapExpectedSelector := labels.NewSelector().Add(*configMapReq)

		require.True(
			t,
			func() bool {
				for obj, byObj := range byObject {
					s, ok := obj.(*corev1.Secret)
					if !ok || s == nil {
						continue
					}

					if !reflect.DeepEqual(secretExpectedSelector, byObj.Label) {
						continue
					}

					return true
				}
				return false
			}(),
		)
		require.True(
			t,
			func() bool {
				for obj, byObj := range byObject {
					s, ok := obj.(*corev1.ConfigMap)
					if !ok || s == nil {
						continue
					}

					if !reflect.DeepEqual(configMapExpectedSelector, byObj.Label) {
						continue
					}

					return true
				}
				return false
			}(),
		)
	})
}
