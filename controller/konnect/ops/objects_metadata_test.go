package ops_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
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
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
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
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
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
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
				"custom-label":                   "custom-value",
				"another-label":                  "another-value",
			},
		},
		{
			name: "user-provided labels cannot override Kubernetes metadata labels",
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
				ops.KubernetesKindLabelKey:       "user-kind",
				ops.KubernetesGroupLabelKey:      "user-group",
				ops.KubernetesVersionLabelKey:    "user-version",
				ops.KubernetesNameLabelKey:       "user-name",
				ops.KubernetesNamespaceLabelKey:  "user-namespace",
				ops.KubernetesUIDLabelKey:        "user-uid",
				ops.KubernetesGenerationLabelKey: "100",
				ops.ManagedByLabelKey:            "user",
				"custom-label":                   "custom-value",
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKind",
				ops.KubernetesGroupLabelKey:      "test.objects.io",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "test-object",
				ops.KubernetesNamespaceLabelKey:  "test-namespace",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
				"custom-label":                   "custom-value",
			},
		},
		{
			name: "user-provided namespace label is dropped for cluster-scoped objects",
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
			userLabels: map[string]string{
				ops.KubernetesNamespaceLabelKey: "user-namespace",
			},
			expectedLabels: map[string]string{
				ops.KubernetesKindLabelKey:       "TestObjectKind",
				ops.KubernetesGroupLabelKey:      "test.objects.io",
				ops.KubernetesVersionLabelKey:    "v1",
				ops.KubernetesNameLabelKey:       "test-object",
				ops.KubernetesUIDLabelKey:        "test-uid",
				ops.KubernetesGenerationLabelKey: "2",
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
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
				ops.ManagedByLabelKey:            ops.ManagedByKongOperatorLabelValue,
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

func TestWithKubernetesMetadataLabelsPtr(t *testing.T) {
	namespacedObject := testObjectKind{
		Kind:       "TestObjectKind",
		APIVersion: "test.objects.io/v1",
		Name:       "test-object",
		Namespace:  "test-namespace",
		UID:        "test-uid",
		Generation: 2,
	}
	clusterScopedObject := namespacedObject
	clusterScopedObject.Namespace = ""

	metadataLabels := func(obj testObjectKind) map[string]*string {
		labels := map[string]*string{
			ops.KubernetesKindLabelKey:       new("TestObjectKind"),
			ops.KubernetesGroupLabelKey:      new("test.objects.io"),
			ops.KubernetesVersionLabelKey:    new("v1"),
			ops.KubernetesNameLabelKey:       new("test-object"),
			ops.KubernetesUIDLabelKey:        new("test-uid"),
			ops.KubernetesGenerationLabelKey: new("2"),
			ops.ManagedByLabelKey:            new(ops.ManagedByKongOperatorLabelValue),
		}
		if obj.Namespace != "" {
			labels[ops.KubernetesNamespaceLabelKey] = new(obj.Namespace)
		}
		return labels
	}
	withLabels := func(labels map[string]*string, extra map[string]*string) map[string]*string {
		maps.Copy(labels, extra)
		return labels
	}

	testCases := []struct {
		name           string
		obj            testObjectKind
		userLabels     map[string]*string
		expectedLabels map[string]*string
	}{
		{
			name:           "no user-provided labels",
			obj:            namespacedObject,
			expectedLabels: metadataLabels(namespacedObject),
		},
		{
			name: "user-provided labels are added, including nil ones",
			obj:  namespacedObject,
			userLabels: map[string]*string{
				"custom-label":  new("custom-value"),
				"removed-label": nil,
			},
			expectedLabels: withLabels(metadataLabels(namespacedObject), map[string]*string{
				"custom-label":  new("custom-value"),
				"removed-label": nil,
			}),
		},
		{
			name: "user-provided labels cannot override or delete Kubernetes metadata labels",
			obj:  namespacedObject,
			userLabels: map[string]*string{
				ops.KubernetesUIDLabelKey:       new("user-uid"),
				ops.KubernetesNameLabelKey:      nil,
				ops.KubernetesNamespaceLabelKey: nil,
				ops.ManagedByLabelKey:           new("user"),
				"custom-label":                  new("custom-value"),
			},
			expectedLabels: withLabels(metadataLabels(namespacedObject), map[string]*string{
				"custom-label": new("custom-value"),
			}),
		},
		{
			name: "user-provided namespace label is dropped for cluster-scoped objects",
			obj:  clusterScopedObject,
			userLabels: map[string]*string{
				ops.KubernetesNamespaceLabelKey: new("user-namespace"),
			},
			expectedLabels: metadataLabels(clusterScopedObject),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			labels := ops.WithKubernetesMetadataLabelsPtr(&tc.obj, tc.userLabels)
			require.Equal(t, tc.expectedLabels, labels)
		})
	}
}

func TestGenerateTagsForObject(t *testing.T) {
	namespacedObject := func() testObjectKind {
		return testObjectKind{
			Name:       "test-object",
			Namespace:  "test-namespace",
			UID:        "test-uid",
			Generation: 2,
			Kind:       "TestObjectKind",
			APIVersion: "test.objects.io/v1",
		}
	}
	clusterScopedObject := func() testObjectKind {
		return testObjectKind{
			Name:       "test-object",
			UID:        "test-uid",
			Generation: 2,
			Kind:       "TestObjectKind",
			APIVersion: "test.objects.io/v1",
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
				"managed-by:kong-operator",
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
				"managed-by:kong-operator",
			},
		},
		{
			name: "annotation tags are set",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.Annotations = map[string]string{
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
				"managed-by:kong-operator",
				"tag1",
				"tag2",
			},
		},
		{
			name: "additional tags are passed with a duplicate",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.Annotations = map[string]string{
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
				"managed-by:kong-operator",
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
				"managed-by:kong-operator",
			},
		},
		{
			name: "too long tags in annotations are truncated",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.Annotations = map[string]string{
					"konghq.com/tags": "tag1,tag2,long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-tag-that-would-end-here-and-no-more-things-should-be-preserved",
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
				"long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-tag-that-would-end-here",
				"managed-by:kong-operator",
				"tag1",
				"tag2",
			},
		},
		{
			name: "too long tags in additional tags are truncated",
			obj:  namespacedObject(),
			additionalTags: []string{
				"tag1",
				"tag2",
				"long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-tag-that-would-end-here-and-no-more-things-should-be-preserved",
			},
			expectedTags: []string{
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-long-tag-that-would-end-here",
				"managed-by:kong-operator",
				"tag1",
				"tag2",
			},
		},
		{
			name: "when too many tags in total, last from annotations are discarded",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.Annotations = map[string]string{
					"konghq.com/tags": "a,b,c,d,e,f,g,h,i,j,k,l,iwillbediscarded",
				}
				return obj
			}(),
			expectedTags: []string{
				"a",
				"b",
				"c",
				"d",
				"e",
				"f",
				"g",
				"h",
				"i",
				"j",
				"k",
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"l",
				"managed-by:kong-operator",
			},
		},
		{
			name: "when too many tags in total and additional tags are passed, last from annotations are discarded",
			obj: func() testObjectKind {
				obj := namespacedObject()
				obj.Annotations = map[string]string{
					"konghq.com/tags": "a,c,gwillbediscarded,iwillbediscarded,kwillbediscarded,mwillbediscarded",
				}
				return obj
			}(),
			additionalTags: []string{"b", "d", "f", "h", "j", "l", "n", "o", "p", "r"},
			expectedTags: []string{
				"a",
				"b",
				"c",
				"d",
				"f",
				"h",
				"j",
				"k8s-generation:2",
				"k8s-group:test.objects.io",
				"k8s-kind:TestObjectKind",
				"k8s-name:test-object",
				"k8s-namespace:test-namespace",
				"k8s-uid:test-uid",
				"k8s-version:v1",
				"l",
				"managed-by:kong-operator",
				"n",
				"o",
				"p",
				"r",
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
