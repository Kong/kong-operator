package manager

import (
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/platform"
)

func TestSetByObjectFor(t *testing.T) {
	t.Run("valid selector for Secret", func(t *testing.T) {
		byObject := make(map[client.Object]cache.ByObject)
		selector := "test-label"

		err := setByObjectFor[corev1.Secret](selector, byObject)
		require.NoError(t, err)

		// Verify the label selector was set correctly.
		expectedReq, err := labels.NewRequirement(selector, selection.In, []string{"true", "internal"})
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
		expectedReq, err := labels.NewRequirement(selector, selection.In, []string{"true", "internal"})
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
		secretReq, err := labels.NewRequirement(secretSelector, selection.In, []string{"true", "internal"})
		require.NoError(t, err)
		secretExpectedSelector := labels.NewSelector().Add(*secretReq)

		configMapReq, err := labels.NewRequirement(configMapSelector, selection.In, []string{"true", "internal"})
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

func TestResolveUserIDAssignedByPlatform(t *testing.T) {
	openShiftSCCGVR := schema.GroupVersionResource{
		Group:    "security.openshift.io",
		Version:  "v1",
		Resource: "securitycontextconstraints",
	}

	restMapperServingSCC := func() apimeta.RESTMapper {
		mapper := apimeta.NewDefaultRESTMapper(nil)
		mapper.Add(openShiftSCCGVR.GroupVersion().WithKind("SecurityContextConstraints"), apimeta.RESTScopeRoot)
		return mapper
	}

	type erroringMapper struct {
		apimeta.RESTMapper
	}

	testCases := []struct {
		name        string
		platform    platform.Platform
		mapper      apimeta.RESTMapper
		expected    bool
		expectedErr bool
	}{
		{
			name:     "kubernetes platform never assigns the user",
			platform: platform.Kubernetes,
			mapper:   restMapperServingSCC(),
			expected: false,
		},
		{
			name:     "openshift platform always assigns the user",
			platform: platform.Openshift,
			mapper:   apimeta.NewDefaultRESTMapper(nil),
			expected: true,
		},
		{
			name:     "auto detects OpenShift",
			platform: platform.Auto,
			mapper:   restMapperServingSCC(),
			expected: true,
		},
		{
			name:     "auto detects a cluster without SCCs",
			platform: platform.Auto,
			mapper:   apimeta.NewDefaultRESTMapper(nil),
			expected: false,
		},
		{
			name:     "auto falls back to Kubernetes when the probe fails",
			platform: platform.Auto,
			mapper:   erroringMapper{apimeta.NewDefaultRESTMapper(nil)},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assigned, err := resolveUserIDAssignedByPlatform(logr.Discard(), tc.platform, tc.mapper)
			if tc.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expected, assigned)
		})
	}
}
