package hybridgateway

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

func newUnstructured(ns, name string, gvk schema.GroupVersionKind, labels map[string]string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetGroupVersionKind(gvk)
	u.SetLabels(labels)
	return u
}

func TestPruneDesiredObj(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		wantMeta map[string]any
		wantSpec map[string]any
	}{
		{
			name: "removes name and namespace",
			input: map[string]any{
				"metadata": map[string]any{
					"name":      "test-name",
					"namespace": "test-namespace",
					"labels":    map[string]any{"foo": "bar"},
				},
				"spec": map[string]any{"field": "value"},
			},
			wantMeta: map[string]any{"labels": map[string]any{"foo": "bar"}},
			wantSpec: map[string]any{"field": "value"},
		},
		{
			name: "prunes empty metadata",
			input: map[string]any{
				"metadata": map[string]any{},
			},
			wantMeta: nil,
			wantSpec: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := unstructured.Unstructured{Object: tt.input}
			u := pruneDesiredObj(obj)
			metadata, hasMeta := u.Object["metadata"]
			if tt.wantMeta == nil {
				assert.False(t, hasMeta, "metadata should be missing or nil")
			} else {
				metaMap, ok := metadata.(map[string]any)
				assert.True(t, ok, "metadata should be a map")
				assert.Equal(t, tt.wantMeta, metaMap)
			}
			if tt.wantSpec != nil {
				assert.Equal(t, tt.wantSpec, u.Object["spec"])
			} else {
				_, hasSpec := u.Object["spec"]
				assert.False(t, hasSpec)
			}
		})
	}
}

func TestCleanOrphanedResources(t *testing.T) {
	gvks := []schema.GroupVersionKind{
		{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"},
		{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"},
	}
	root := &gwtypes.HTTPRoute{}
	root.SetName("httproute-owner")
	root.SetNamespace("ns")
	root.SetGroupVersionKind(schema.GroupVersionKind{Group: gwtypes.GroupName, Version: "v1alpha2", Kind: "HTTPRoute"})
	ownerLabels := metadata.BuildLabels(root, nil)

	tests := []struct {
		name         string
		desiredNames map[schema.GroupVersionKind][]string
		orphanNames  map[schema.GroupVersionKind][]string
		wantNames    map[schema.GroupVersionKind][]string
		gvks         []schema.GroupVersionKind
	}{
		{
			name:         "KongRoute orphans cleaned",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
		},
		{
			name:         "KongService orphans cleaned",
			gvks:         []schema.GroupVersionKind{gvks[1]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[1]: {"service1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[1]: {"service2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[1]: {"service1"}},
		},
		{
			name:         "No orphans present",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
		},
		{
			name:         "Multiple orphans and multiple desired resources",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2", "route3"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route4", "route5"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2", "route3"}},
		},
		{
			name:         "No desired, only orphans",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {}},
		},
		{
			name:         "Desired and orphan have overlapping names",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route2", "route3"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2"}},
		},
		{
			name:         "Orphans in different namespace are not deleted",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2"}}, // route2 is in a different namespace, should not be deleted
		},
		{
			name:         "Orphan with label mismatch is not deleted",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1", "route2"}}, // route2 has wrong labels, should not be deleted
		},
		{
			name: "Multiple GVKs in one test",
			gvks: []schema.GroupVersionKind{gvks[0], gvks[1]},
			desiredNames: map[schema.GroupVersionKind][]string{
				gvks[0]: {"routeA"},
				gvks[1]: {"serviceA"},
			},
			orphanNames: map[schema.GroupVersionKind][]string{
				gvks[0]: {"routeB"},
				gvks[1]: {"serviceB"},
			},
			wantNames: map[schema.GroupVersionKind][]string{
				gvks[0]: {"routeA"},
				gvks[1]: {"serviceA"},
			},
		},
		{
			name:         "No resources at all",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {}},
		},
		{
			name:         "Orphan with extra fields is deleted",
			gvks:         []schema.GroupVersionKind{gvks[0]},
			desiredNames: map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
			orphanNames:  map[schema.GroupVersionKind][]string{gvks[0]: {"route2"}},
			wantNames:    map[schema.GroupVersionKind][]string{gvks[0]: {"route1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var desired, orphans []unstructured.Unstructured
			for _, gvk := range tt.gvks {
				desiredSet := make(map[string]struct{})
				for _, name := range tt.desiredNames[gvk] {
					desired = append(desired, newUnstructured("ns", name, gvk, ownerLabels))
					desiredSet[name] = struct{}{}
				}
				for _, name := range tt.orphanNames[gvk] {
					ns := "ns"
					labels := ownerLabels
					extraFields := false
					if tt.name == "Orphans in different namespace are not deleted" {
						ns = "other-ns"
					}
					if tt.name == "Orphan with label mismatch is not deleted" {
						labels = map[string]string{"unrelated": "true"}
					}
					if tt.name == "Orphan with extra fields is deleted" && name == "route2" {
						extraFields = true
					}
					if _, exists := desiredSet[name]; !exists {
						obj := newUnstructured(ns, name, gvk, labels)

						annotations := obj.GetAnnotations()
						if annotations == nil {
							annotations = make(map[string]string)
						}
						annotations[consts.GatewayOperatorHybridRoutesAnnotation] = "ns/httproute-owner"
						obj.SetAnnotations(annotations)

						if extraFields {
							annotations["extra"] = "field"
							obj.SetAnnotations(annotations)
						}
						orphans = append(orphans, obj)
					}
				}
			}
			var allObjs []client.Object
			for i := range desired {
				allObjs = append(allObjs, &desired[i])
			}
			for i := range orphans {
				allObjs = append(allObjs, &orphans[i])
			}
			scheme := runtime.NewScheme()
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(allObjs...).Build()
			fakeConv := &fakeHTTPRouteConverter{
				desired: desired,
				gvks:    tt.gvks,
				root:    *root,
			}
			logger := logr.Discard()
			_, err := cleanOrphanedResources(context.Background(), cl, logger, fakeConv)
			assert.NoError(t, err)
			for _, gvk := range tt.gvks {
				list := &unstructured.UnstructuredList{}
				list.SetGroupVersionKind(gvk)
				err = cl.List(context.Background(), list)
				assert.NoError(t, err)
				var nsNames, otherNsNames []string
				for _, item := range list.Items {
					if item.GetNamespace() == "ns" {
						nsNames = append(nsNames, item.GetName())
					} else if item.GetNamespace() == "other-ns" {
						otherNsNames = append(otherNsNames, item.GetName())
					}
				}
				switch tt.name {
				case "Orphans in different namespace are not deleted":
					assert.ElementsMatch(t, tt.wantNames[gvk][:1], nsNames)
					assert.ElementsMatch(t, tt.wantNames[gvk][1:], otherNsNames)
				case "Orphan with label mismatch is not deleted":
					assert.ElementsMatch(t, tt.wantNames[gvk], nsNames)
				default:
					assert.ElementsMatch(t, tt.wantNames[gvk], nsNames)
				}
			}
		})
	}
}

// Minimal fake converter for HTTPRoute

type fakeHTTPRouteConverter struct {
	desired []unstructured.Unstructured
	gvks    []schema.GroupVersionKind
	root    gwtypes.HTTPRoute
}

func (f *fakeHTTPRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	return f.desired, nil
}
func (f *fakeHTTPRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind { return f.gvks }
func (f *fakeHTTPRouteConverter) GetRootObject() gwtypes.HTTPRoute           { return f.root }
func (f *fakeHTTPRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	return len(f.desired), nil
}
func (f *fakeHTTPRouteConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHTTPRouteConverter) UpdateSharedRouteStatus([]unstructured.Unstructured) error {
	return nil
}

func (f *fakeHTTPRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (bool, error) {
	return false, nil
}
