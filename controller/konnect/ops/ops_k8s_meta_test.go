package ops_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kong/gateway-operator/controller/konnect/ops"
)

// testObjectKind is a test object type that implements the client.Object interface.
type testObjectKind struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func (b *testObjectKind) DeepCopyObject() runtime.Object {
	return b
}

func TestWithKubernetesMetadataLabels(t *testing.T) {
	testCases := []struct {
		name           string
		obj            testObjectKind
		expectedLabels map[string]string
	}{
		{
			name: "all object's expected fields are set",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKind",
					APIVersion: "test.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Namespace:  "test-namespace",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKind",
				ops.KubernetesGroupLabelKey:      "test.objects.io",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "test-object",
				ops.KubernetesNamespaceLabelKey:  "test-namespace",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
			},
		},
		{
			name: "namespace is not set (cluster-scoped object)",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKind",
					APIVersion: "test.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKind",
				ops.KubernetesGroupLabelKey:      "test.objects.io",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "test-object",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labels := ops.WithKubernetesMetadataLabels(&tc.obj, tc.expectedLabels)
			require.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestGenerateKubernetesMetadataTags(t *testing.T) {
	testCases := []struct {
		name         string
		obj          testObjectKind
		expectedTags []string
	}{
		{
			name: "all object's expected fields are set",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKind",
					APIVersion: "test.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					Namespace:  "test-namespace",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"k8s-namespace:test-namespace",
			},
		},
		{
			name: "namespace is not set (cluster-scoped object)",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKind",
					APIVersion: "test.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-object",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-uid:test-uid",
				"k8s-version:v1",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tags := ops.GenerateKubernetesMetadataTags(&tc.obj)
			require.Equal(t, tc.expectedTags, tags)
		})
	}
}
