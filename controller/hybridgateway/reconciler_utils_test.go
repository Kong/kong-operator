package hybridgateway

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func TestGetOwnedResources(t *testing.T) {
	sc := scheme.Get()
	require.NoError(t, configurationv1alpha1.AddToScheme(sc))

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kongservice",
			Namespace: "test-namespace",
			UID:       "12345",
		},
	}
	testCases := []struct {
		name                      string
		owner                     *corev1.Service
		existingObjects           []client.Object
		hash                      string
		expectedMapKeysWithLength map[string]int
	}{
		{
			name:  "single owned resource",
			owner: service,
			existingObjects: []client.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-1",
						Namespace: "test-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "test-host",
						},
					},
				},
			},
			expectedMapKeysWithLength: map[string]int{utils.Hash64(configurationv1alpha1.KongServiceSpec{KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{Host: "test-host"}}): 1},
		},
		{
			name:  "multiple owned resources with different specs",
			owner: service,
			existingObjects: []client.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-1",
						Namespace: "test-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "test-host-1",
						},
					},
				},
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-2",
						Namespace: "test-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "test-host-2",
						},
					},
				},
			},
			expectedMapKeysWithLength: map[string]int{
				utils.Hash64(configurationv1alpha1.KongServiceSpec{KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{Host: "test-host-1"}}): 1,
				utils.Hash64(configurationv1alpha1.KongServiceSpec{KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{Host: "test-host-2"}}): 1,
			},
		},
		{
			name:  "resource in different namespace is ignored",
			owner: service,
			existingObjects: []client.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-3",
						Namespace: "other-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "test-host",
						},
					},
				},
			},
			expectedMapKeysWithLength: map[string]int{},
		},
		{
			name:  "multiple resources with identical specs",
			owner: service,
			existingObjects: []client.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-4a",
						Namespace: "test-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "same-host",
						},
					},
				},
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "owned-resource-4b",
						Namespace: "test-namespace",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Host: "same-host",
						},
					},
				},
			},
			expectedMapKeysWithLength: map[string]int{
				utils.Hash64(configurationv1alpha1.KongServiceSpec{KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{Host: "same-host"}}): 2,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, obj := range tc.existingObjects {
				ownerRef := k8sutils.GenerateOwnerReferenceForObject(tc.owner)
				obj.SetOwnerReferences([]metav1.OwnerReference{ownerRef})
				kongService := configurationv1alpha1.KongService{}
				assert.NoError(t, sc.Convert(obj, &kongService, nil))
				labels := map[string]string{
					consts.GatewayOperatorManagedByLabel:          consts.ServiceManagedByLabel,
					consts.GatewayOperatorManagedByNameLabel:      tc.owner.Name,
					consts.GatewayOperatorManagedByNamespaceLabel: tc.owner.Namespace,
					consts.GatewayOperatorHashSpecLabel:           utils.Hash64(kongService.Spec),
				}
				obj.SetLabels(labels)
			}
			cl := fake.NewClientBuilder().
				WithScheme(sc).
				WithObjects(tc.owner).
				WithObjects(tc.existingObjects...).
				Build()
			sharedStatusMap := route.NewSharedStatusMap()
			conv, err := converter.NewConverter(*tc.owner, cl, sharedStatusMap)
			assert.NoError(t, err)
			objects, err := conv.ListExistingObjects(context.Background())
			assert.NoError(t, err)
			resourceMap := mapOwnedResources(tc.owner, objects)
			require.ElementsMatch(t, lo.Keys(tc.expectedMapKeysWithLength), lo.Keys(resourceMap))
			for n, objs := range resourceMap {
				require.Equal(t, tc.expectedMapKeysWithLength[n], len(objs.resources))
			}
		})
	}
}

func TestHasOwnerRef(t *testing.T) {
	ownerRef := metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "Service",
		Name:       "test-owner",
		UID:        "12345",
	}

	makeUnstructuredWithOwnerRefs := func(ownerRefs []metav1.OwnerReference) unstructured.Unstructured {
		obj := unstructured.Unstructured{}
		obj.SetAPIVersion("v1")
		obj.SetKind("Service")
		obj.SetName("test-resource")
		obj.SetNamespace("test-namespace")
		refs := make([]map[string]any, len(ownerRefs))
		for i, ref := range ownerRefs {
			refs[i] = map[string]any{
				"apiVersion": ref.APIVersion,
				"kind":       ref.Kind,
				"name":       ref.Name,
				"uid":        string(ref.UID),
			}
		}
		if len(refs) > 0 {
			_ = unstructured.SetNestedSlice(obj.Object, lo.ToAnySlice(refs), "metadata", "ownerReferences")
		}
		return obj
	}

	t.Run("returns true when ownerRef matches", func(t *testing.T) {
		u := makeUnstructuredWithOwnerRefs([]metav1.OwnerReference{ownerRef})
		assert.True(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns false when ownerRef does not match", func(t *testing.T) {
		otherRef := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "other-owner",
			UID:        "54321",
		}
		u := makeUnstructuredWithOwnerRefs([]metav1.OwnerReference{otherRef})
		assert.False(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns true when one of multiple ownerRefs matches", func(t *testing.T) {
		otherRef := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "other-owner",
			UID:        "54321",
		}
		u := makeUnstructuredWithOwnerRefs([]metav1.OwnerReference{otherRef, ownerRef})
		assert.True(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns false when no ownerReferences present", func(t *testing.T) {
		u := unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("Service")
		u.SetName("test-resource")
		u.SetNamespace("test-namespace")
		assert.False(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns false when ownerReferences is not a slice", func(t *testing.T) {
		u := unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("Service")
		u.SetName("test-resource")
		u.SetNamespace("test-namespace")
		_ = unstructured.SetNestedField(u.Object, "not-a-slice", "metadata", "ownerReferences")
		assert.False(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns false when ownerReferences slice contains non-map entries", func(t *testing.T) {
		u := unstructured.Unstructured{}
		u.SetAPIVersion("v1")
		u.SetKind("Service")
		u.SetName("test-resource")
		u.SetNamespace("test-namespace")
		_ = unstructured.SetNestedSlice(u.Object, []any{"not-a-map"}, "metadata", "ownerReferences")
		assert.False(t, hasOwnerRef(u, ownerRef))
	})

	t.Run("returns false when ownerReferences slice is empty", func(t *testing.T) {
		u := makeUnstructuredWithOwnerRefs([]metav1.OwnerReference{})
		assert.False(t, hasOwnerRef(u, ownerRef))
	})
}

func TestReduce(t *testing.T) {
	sharedStatusMap := route.NewSharedStatusMap()
	serviceConverter, err := converter.NewConverter(corev1.Service{}, nil, sharedStatusMap)
	require.NoError(t, err)
	now := time.Now()

	testCases := []struct {
		name                      string
		kind                      string
		kongServices              []unstructured.Unstructured
		expectedResourcesTodelete []string
	}{
		{
			name: "all unprogrammed, keep youngest",
			kind: "KongService",
			kongServices: []unstructured.Unstructured{
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-1")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-10 * time.Minute)})
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-2")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-5 * time.Minute)})
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-3")
					usvc.SetCreationTimestamp(metav1.Time{Time: now})
					return usvc
				}(),
			},
			expectedResourcesTodelete: []string{"svc-1", "svc-2"},
		},
		{
			name: "all programmed, keep youngest programmed",
			kind: "KongService",
			kongServices: []unstructured.Unstructured{
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-1")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-10 * time.Minute)})
					usvc.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Programmed",
								"status": "True",
							},
						},
					}
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-2")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-5 * time.Minute)})
					usvc.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Programmed",
								"status": "True",
							},
						},
					}
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-3")
					usvc.SetCreationTimestamp(metav1.Time{Time: now})
					usvc.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Programmed",
								"status": "True",
							},
						},
					}
					return usvc
				}(),
			},
			expectedResourcesTodelete: []string{"svc-1", "svc-2"},
		},
		{
			name: "single resource, nothing to delete",
			kind: "KongService",
			kongServices: []unstructured.Unstructured{
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-1")
					usvc.SetCreationTimestamp(metav1.Time{Time: now})
					return usvc
				}(),
			},
			expectedResourcesTodelete: []string{},
		},
		{
			name:                      "empty input, nothing to delete",
			kind:                      "KongService",
			kongServices:              []unstructured.Unstructured{},
			expectedResourcesTodelete: []string{},
		},
		{
			name: "mixed programmed and not programmed, keep youngest programmed",
			kind: "KongService",
			kongServices: []unstructured.Unstructured{
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-1")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-10 * time.Minute)})
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-2")
					usvc.SetCreationTimestamp(metav1.Time{Time: now.Add(-5 * time.Minute)})
					usvc.Object["status"] = map[string]any{
						"conditions": []any{
							map[string]any{
								"type":   "Programmed",
								"status": "True",
							},
						},
					}
					return usvc
				}(),
				func() unstructured.Unstructured {
					usvc := unstructured.Unstructured{}
					usvc.SetKind("KongService")
					usvc.SetName("svc-3")
					usvc.SetCreationTimestamp(metav1.Time{Time: now})
					return usvc
				}(),
			},
			expectedResourcesTodelete: []string{"svc-1", "svc-3"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			obj := unstructured.Unstructured{}
			obj.SetKind(tc.kind)
			var resourcesToDelete []string
			for _, obj := range reduceDuplicates(tc.kongServices, serviceConverter.Reduce(obj)...) {
				resourcesToDelete = append(resourcesToDelete, obj.GetName())
			}
			require.ElementsMatch(t, tc.expectedResourcesTodelete, resourcesToDelete)
		})
	}
}
