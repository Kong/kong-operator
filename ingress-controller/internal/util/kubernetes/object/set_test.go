package object

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestObjectConfigurationStatusSet(t *testing.T) {
	t.Log("generating some objects to test the object set")
	ing1 := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  corev1.NamespaceDefault,
			Name:       "test-ingress-1",
			Generation: 1,
		},
	}
	ing1.SetGroupVersionKind(ingGVK)
	ing2 := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  corev1.NamespaceDefault,
			Name:       "test-ingress-2",
			Generation: 1,
		},
	}
	ing2.SetGroupVersionKind(ingGVK)
	ing3 := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "other-namespace",
			Name:       "test-ingress-1",
			Generation: 1,
		},
	}
	ing3.SetGroupVersionKind(ingGVK)

	t.Log("verifying creation of an object configure status set")
	set := &ConfigurationStatusSet{}
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing1))
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing2))
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing3))

	t.Log("verifying object configure status set insertion")
	set.Insert(ing1, true)
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing1))
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing2))
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing3))
	set.Insert(ing2, false)
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing1))
	require.Equal(t, ConfigurationStatusFailed, set.Get(ing2))
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing3))
	set.Insert(ing3, true)
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing1))
	require.Equal(t, ConfigurationStatusFailed, set.Get(ing2))
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing3))
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing1))
	require.Equal(t, ConfigurationStatusFailed, set.Get(ing2))
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing3))
	t.Log("updating generation of some objects")
	ing1.Generation = 2
	require.Equal(t, ConfigurationStatusUnknown, set.Get(ing1))
	require.Equal(t, ConfigurationStatusFailed, set.Get(ing2))
	require.Equal(t, ConfigurationStatusSucceeded, set.Get(ing3))
}

// -----------------------------------------------------------------------------
// Testing Utilities
// -----------------------------------------------------------------------------

// initialized objects don't have GVK's, so we fake those for unit tests.
var (
	ingGVK = schema.GroupVersionKind{
		Group:   "networking.k8s.io",
		Version: "v1",
		Kind:    "Ingress",
	}
)
