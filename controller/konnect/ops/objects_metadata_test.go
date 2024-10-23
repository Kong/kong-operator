package ops_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/controller/konnect/ops"
)

// testObjectKind is a test object type that implements the client.Object interface.
type testObjectKind struct {
	metav1.TypeMeta
	metav1.ObjectMeta
}

func TestWithKubernetesMetadataLabels(t *testing.T) {
	testCases := []struct {
		name           string
		obj            testObjectKind
		userLabels     map[string]string
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
		{
			name: "user-provided labels are added",
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
			userLabels: map[string]string{
				"custom-label":  "custom-value",
				"another-label": "another-value",
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKind",
				ops.KubernetesGroupLabelKey:      "test.objects.io",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "test-object",
				ops.KubernetesNamespaceLabelKey:  "test-namespace",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
				"custom-label":                   "custom-value",
				"another-label":                  "another-value",
			},
		},
		{
			name: "too long kind, group, name, and namespace are truncated",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKindWithAVeryLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongName",
					APIVersion: "testlonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglong.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "testobjectverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglong",
					Namespace:  "testnamespaceverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglong",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKindWithAVeryLongLongLongLongLongLongLongLongLongLong",
				ops.KubernetesGroupLabelKey:      "testlonglonglonglonglonglonglonglonglonglonglonglonglonglonglon",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "testobjectverylonglonglonglonglonglonglonglonglonglonglonglongl",
				ops.KubernetesNamespaceLabelKey:  "testnamespaceverylonglonglonglonglonglonglonglonglonglonglonglo",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labels := ops.WithKubernetesMetadataLabels(&tc.obj, tc.userLabels)
			require.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestGenerateTagsForObject(t *testing.T) {
	namespacedObject := func() testObjectKind {
		return testObjectKind{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-object",
				Namespace:  "test-namespace",
				UID:        "test-uid",
				Generation: 2,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "TestObjectKind",
				APIVersion: "test.objects.io/v1",
			},
		}
	}
	clusterScopedObject := func() testObjectKind {
		return testObjectKind{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-object",
				UID:        "test-uid",
				Generation: 2,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "TestObjectKind",
				APIVersion: "test.objects.io/v1",
			},
		}
	}

	testCases := []struct {
		name           string
		obj            testObjectKind
		additionalTags []string
		expectedTags   []string
	}{
		{
			name: "all object's expected fields are set",
			obj:  namespacedObject(),
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
			},
		},
		{
			name: "namespace is not set (cluster-scoped object)",
			obj:  clusterScopedObject(),
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-uid:test-uid",
				"k8s-version:v1",
			},
		},
		{
			name: "annotation tags are set",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.ObjectMeta.Annotations = map[string]string{
					"konghq.com/tags": "tag1,tag2",
				}
				return obj
			}(),
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"tag1",
				"tag2",
			},
		},
		{
			name: "additional tags are passed with a duplicate",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.ObjectMeta.Annotations = map[string]string{
					"konghq.com/tags": "tag1,tag2,duplicate-tag",
				}
				return obj
			}(),
			additionalTags: []string{"tag3", "duplicate-tag"},
			expectedTags: []string{
				"duplicate-tag",
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"tag1",
				"tag2",
				"tag3",
			},
		},
		{
			name: "too long kind, group, name, and namespace are truncated",
			obj: testObjectKind{
				TypeMeta: metav1.TypeMeta{
					Kind:       "TestObjectKindWithAVeryLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongName",
					APIVersion: "testlonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglong.objects.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "testobjectverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglongname",
					Namespace:  "testnamespaceverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglongnamespace",
					UID:        "test-uid",
					Generation: 2,
				},
			},
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:testlonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglo",
				"k8s-kind:TestObjectKindWithAVeryLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLongLong",
				"k8s-name:testobjectverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglongl",
				"k8s-namespace:testnamespaceverylonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglonglongl",
				"k8s-uid:test-uid",
				"k8s-version:v1",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tags := ops.GenerateTagsForObject(&tc.obj, tc.additionalTags...)
			require.Equal(t, tc.expectedTags, tags)
		})
	}
}
