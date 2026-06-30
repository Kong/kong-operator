package hybridgateway

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	finalizerconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/finalizers"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
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

func TestEnforceState_DependencyGating(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	// Prepare Scheme with needed types.
	s := scheme.Get()

	ns := "ns"

	// Case 1: Target waits for missing Upstream.
	t.Run("target waits for missing upstream", func(t *testing.T) {
		// Desired contains a KongTarget referencing upstream "u1".
		targetGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongTarget"}
		desired := newUnstructured(ns, "t1", targetGVK, map[string]string{})
		_ = unstructured.SetNestedField(desired.Object, map[string]any{
			"upstreamRef": map[string]any{"name": "u1"},
		}, "spec")

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).Build()

		applied, waiting, err := enforceState(ctx, cl, logger, fakeConv)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.True(t, waiting)
	})

	// Case 2: Route waits for not-Programmed Service.
	t.Run("route waits for not programmed service", func(t *testing.T) {
		routeGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"}
		desired := newUnstructured(ns, "r1", routeGVK, nil)
		_ = unstructured.SetNestedField(desired.Object, map[string]any{
			"serviceRef": map[string]any{"namespacedRef": map[string]any{"name": "svc1"}},
		}, "spec")

		// Existing KongService with Programmed=False.
		svc := &configurationv1alpha1.KongService{}
		svc.SetName("svc1")
		svc.SetNamespace(ns)
		// Default conditions include Programmed Unknown; ensure it's not True.

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(svc).Build()

		applied, waiting, err := enforceState(ctx, cl, logger, fakeConv)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.True(t, waiting)
	})

	// Case 3: PluginBinding waits for not-Programmed Route.
	t.Run("pluginbinding waits for not programmed route", func(t *testing.T) {
		kpbGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongPluginBinding"}
		desired := newUnstructured(ns, "b1", kpbGVK, nil)
		_ = unstructured.SetNestedField(desired.Object, map[string]any{
			"routeRef": map[string]any{"name": "route1"},
		}, "spec", "targets")

		route := &configurationv1alpha1.KongRoute{}
		route.SetName("route1")
		route.SetNamespace(ns)
		// Programmed not True by default.

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(route).Build()

		applied, waiting, err := enforceState(ctx, cl, logger, fakeConv)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.True(t, waiting)
	})

}

func TestTranslate(t *testing.T) {
	tests := []struct {
		name          string
		translateRet  int
		translateErr  error
		expectedCount int
		expectError   bool
	}{
		{
			name:          "returns translated count",
			translateRet:  3,
			expectedCount: 3,
		},
		{
			name:         "propagates translate error",
			translateErr: assert.AnError,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &fakeHTTPRouteConverter{translateRet: tt.translateRet, translateErr: tt.translateErr}

			count, err := translate[gwtypes.HTTPRoute](conv, t.Context(), logr.Discard())

			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestEnforceStatus(t *testing.T) {
	tests := []struct {
		name           string
		statusUpdated  bool
		statusStop     bool
		statusErr      error
		expectedUpdate bool
		expectedStop   bool
		expectError    bool
	}{
		{
			name:           "returns converter status result",
			statusUpdated:  true,
			statusStop:     true,
			expectedUpdate: true,
			expectedStop:   true,
		},
		{
			name:        "propagates status error",
			statusErr:   assert.AnError,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &fakeHTTPRouteConverter{statusUpdated: tt.statusUpdated, statusStop: tt.statusStop, statusErr: tt.statusErr}

			updated, stop, err := enforceStatus[gwtypes.HTTPRoute](t.Context(), logr.Discard(), conv)

			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expectedUpdate, updated)
			assert.Equal(t, tt.expectedStop, stop)
		})
	}
}

func TestEnforceState_CoreAndErrorPaths(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()
	kongServiceGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}

	makeDesiredService := func(name string, host any) unstructured.Unstructured {
		u := newUnstructured("default", name, kongServiceGVK, nil)
		_ = unstructured.SetNestedField(u.Object, host, "spec", "host")
		_ = unstructured.SetNestedField(u.Object, int64(80), "spec", "port")
		_ = unstructured.SetNestedField(u.Object, "httproute", "spec", "protocol")
		return u
	}

	tests := []struct {
		name            string
		scheme          *runtime.Scheme
		desired         []unstructured.Unstructured
		outputStoreErr  error
		preexisting     []client.Object
		setupClient     func(t *testing.T, cl client.Client)
		interceptor     *interceptor.Funcs
		wantApplied     bool
		wantWaiting     bool
		wantErrContains string
	}{
		{
			name:            "returns error when output store retrieval fails",
			scheme:          scheme.Get(),
			desired:         nil,
			outputStoreErr:  assert.AnError,
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to get desired objects from converter",
		},
		{
			name:        "returns without changes for empty desired list",
			scheme:      scheme.Get(),
			desired:     nil,
			wantApplied: false,
			wantWaiting: false,
		},
		{
			name:        "creates object when not found",
			scheme:      scheme.Get(),
			desired:     []unstructured.Unstructured{makeDesiredService("svc-create", "create.example")},
			wantApplied: true,
			wantWaiting: false,
		},
		{
			name:    "returns get error for existing lookup failures",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-get-err", "err.example")},
			interceptor: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "svc-get-err" {
						return assert.AnError
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to get object kind KongService obj default/svc-get-err",
		},
		{
			name:    "waits when existing object is marked for deletion",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-deleting", "deleting.example")},
			preexisting: []client.Object{func() client.Object {
				u := makeDesiredService("svc-deleting", "old.example")
				ts := metav1.Now()
				u.SetDeletionTimestamp(&ts)
				u.SetFinalizers([]string{"test-finalizer"})
				return &u
			}()},
			wantApplied: false,
			wantWaiting: true,
		},
		{
			name:    "applies update when managed fields are missing for field manager",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-no-managed", "new.example")},
			preexisting: []client.Object{func() client.Object {
				u := makeDesiredService("svc-no-managed", "old.example")
				return &u
			}()},
			wantApplied: true,
			wantWaiting: false,
		},
		{
			name:    "returns extract managed fields error for unsupported group",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{newUnstructured("default", "bad-group", schema.GroupVersionKind{Group: "invalid.group", Version: "v1", Kind: "Bad"}, nil)},
			preexisting: []client.Object{func() client.Object {
				u := newUnstructured("default", "bad-group", schema.GroupVersionKind{Group: "invalid.group", Version: "v1", Kind: "Bad"}, nil)
				return &u
			}()},
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to extract managed fields",
		},
		{
			name:   "returns conversion error for invalid desired payload",
			scheme: scheme.Get(),
			desired: []unstructured.Unstructured{func() unstructured.Unstructured {
				u := makeDesiredService("svc-convert", "ok.example")
				u.Object["spec"] = map[string]any{"host": make(chan int), "port": int64(80), "protocol": "httproute"}
				return u
			}()},
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to create object kind KongService obj default/svc-convert",
		},
		{
			name:    "returns conflict error during create apply",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-create-conflict", "conflict.example")},
			interceptor: &interceptor.Funcs{
				Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
					return k8serrors.NewConflict(schema.GroupResource{Group: "configuration.konghq.com", Resource: "kongservices"}, "svc-create-conflict", assert.AnError)
				},
			},
			wantErrContains: "conflict during create of object kind KongService obj default/svc-create-conflict",
		},
		{
			name:        "returns update error when apply fails on diff",
			scheme:      scheme.Get(),
			desired:     []unstructured.Unstructured{makeDesiredService("svc-update-err", "new.example")},
			preexisting: []client.Object{func() client.Object { u := makeDesiredService("svc-update-err", "old.example"); return &u }()},
			interceptor: &interceptor.Funcs{
				Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
					return assert.AnError
				},
			},
			wantErrContains: "failed to create object kind KongService obj default/svc-update-err",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(tt.scheme)
			if len(tt.preexisting) > 0 {
				builder = builder.WithObjects(tt.preexisting...)
			}
			if tt.interceptor != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptor)
			}
			cl := builder.Build()

			if tt.setupClient != nil {
				tt.setupClient(t, cl)
			}

			conv := &fakeHTTPRouteConverter{desired: tt.desired, outputStoreErr: tt.outputStoreErr}
			applied, waiting, err := enforceState(ctx, cl, logger, conv)

			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantApplied, applied)
			assert.Equal(t, tt.wantWaiting, waiting)
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
			var err error
			requeue := true
			for requeue {
				requeue, err = cleanOrphanedResources(context.Background(), cl, logger, fakeConv)
				assert.NoError(t, err)
			}
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
	desired        []unstructured.Unstructured
	gvks           []schema.GroupVersionKind
	root           gwtypes.HTTPRoute
	outputStoreErr error
	translateRet   int
	translateErr   error
	statusUpdated  bool
	statusStop     bool
	statusErr      error
}

func (f *fakeHTTPRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	if f.outputStoreErr != nil {
		return nil, f.outputStoreErr
	}
	return f.desired, nil
}
func (f *fakeHTTPRouteConverter) GetOutputStoreLen(ctx context.Context, logger logr.Logger) int {
	return len(f.desired)
}
func (f *fakeHTTPRouteConverter) GetExpectedGVKs() []schema.GroupVersionKind { return f.gvks }
func (f *fakeHTTPRouteConverter) GetRootObject() gwtypes.HTTPRoute           { return f.root }
func (f *fakeHTTPRouteConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	if f.translateErr != nil {
		return 0, f.translateErr
	}
	if f.translateRet != 0 {
		return f.translateRet, nil
	}
	return len(f.desired), nil
}
func (f *fakeHTTPRouteConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	return nil, nil
}
func (f *fakeHTTPRouteConverter) UpdateSharedRouteStatus([]unstructured.Unstructured) error {
	return nil
}

func (f *fakeHTTPRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	if f.statusErr != nil {
		return false, false, f.statusErr
	}
	return f.statusUpdated, f.statusStop, nil
}

func (f *fakeHTTPRouteConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (bool, error) {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return true, nil
	}

	annotationValue, exists := annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	if !exists {
		return true, nil
	}

	// Check if the annotation contains our root object
	expectedAnnotation := fmt.Sprintf("%s/%s", f.root.GetNamespace(), f.root.GetName())
	if annotationValue != expectedAnnotation {
		return true, nil
	}

	// Annotation exists and matches our root - allow deletion
	return false, nil
}

func TestShouldProcessObject_HTTPRoute(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create a test Gateway with KonnectExtension (managed by us).
	ourGateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-gateway",
			Namespace: "default",
			UID:       "our-gateway-uid",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "kong",
		},
	}

	// KonnectExtension for our Gateway
	ourKonnectExtension := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"gateway-operator.konghq.com/managed-by": "gateway",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
					Name:       "our-gateway",
					UID:        "our-gateway-uid",
				},
			},
		},
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "our-cp",
						},
					},
				},
			},
		},
	}

	// KonnectGatewayControlPlane for our Gateway
	ourControlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-cp",
			Namespace: "default",
		},
	}

	// Create a test Gateway without KonnectExtension (not managed by us).
	otherGateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-gateway",
			Namespace: "default",
			UID:       "other-gateway-uid",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "other-class",
		},
	}

	testCases := []struct {
		name             string
		setupRoute       func() *gwtypes.HTTPRoute
		clientObjects    []client.Object
		interceptorFuncs *interceptor.Funcs
		expectedResult   bool
		description      string
	}{
		{
			name: "object with finalizer should be processed",
			setupRoute: func() *gwtypes.HTTPRoute {
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
					},
				}
				return route
			},
			clientObjects:    []client.Object{},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Objects with our finalizer should be processed regardless of Gateway reference.",
		},
		{
			name: "object without finalizer but referencing our Gateway should be processed",
			setupRoute: func() *gwtypes.HTTPRoute {
				gatewayName := gwtypes.ObjectName("our-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name: gatewayName,
								},
							},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{ourGateway, ourKonnectExtension, ourControlPlane},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Objects without finalizer but referencing our Gateway should be processed.",
		},
		{
			name: "object without finalizer referencing other Gateway should be skipped",
			setupRoute: func() *gwtypes.HTTPRoute {
				gatewayName := gwtypes.ObjectName("other-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name: gatewayName,
								},
							},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{otherGateway},
			interceptorFuncs: nil,
			expectedResult:   false,
			description:      "Objects without finalizer referencing unsupported Gateway should be skipped.",
		},
		{
			name: "object without finalizer and no Gateway reference should be skipped",
			setupRoute: func() *gwtypes.HTTPRoute {
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{},
			interceptorFuncs: nil,
			expectedResult:   false,
			description:      "Objects without finalizer and no Gateway reference should be skipped.",
		},
		{
			name: "object with finalizer referencing other Gateway should still be processed",
			setupRoute: func() *gwtypes.HTTPRoute {
				gatewayName := gwtypes.ObjectName("other-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name: gatewayName,
								},
							},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{otherGateway},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Objects with finalizer should be processed for cleanup even if referencing other Gateway.",
		},
		{
			name: "object without finalizer referencing mix of our and other Gateway should be processed",
			setupRoute: func() *gwtypes.HTTPRoute {
				ourGatewayName := gwtypes.ObjectName("our-gateway")
				otherGatewayName := gwtypes.ObjectName("other-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: otherGatewayName},
								{Name: ourGatewayName},
							},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{ourGateway, ourKonnectExtension, ourControlPlane, otherGateway},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Objects referencing at least one supported Gateway should be processed.",
		},
		{
			name: "object without finalizer referencing non-existent Gateway should be skipped",
			setupRoute: func() *gwtypes.HTTPRoute {
				gatewayName := gwtypes.ObjectName("non-existent-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
				return route
			},
			clientObjects:    []client.Object{},
			interceptorFuncs: nil,
			expectedResult:   false,
			description:      "Objects referencing non-existent Gateway should be skipped.",
		},
		{
			name: "object without finalizer with API error when fetching Gateway should be skipped",
			setupRoute: func() *gwtypes.HTTPRoute {
				gatewayName := gwtypes.ObjectName("test-gateway")
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
				return route
			},
			clientObjects: []client.Object{},
			interceptorFuncs: &interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*gwtypes.Gateway); ok {
						return assert.AnError // Simulate an unexpected API error.
					}
					return client.Get(ctx, key, obj, opts...)
				},
			},
			expectedResult: false,
			description:    "Objects with API error when fetching Gateway should be skipped.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			route := tc.setupRoute()
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gwtypes.GroupName,
				Version: "v1",
				Kind:    "HTTPRoute",
			})

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
				&gwtypes.HTTPRoute{}, &gwtypes.Gateway{}, &gwtypes.GatewayClass{},
			)
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: "konnect.konghq.com", Version: "v1alpha2"},
				&konnectv1alpha2.KonnectExtension{},
				&konnectv1alpha2.KonnectExtensionList{},
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				&konnectv1alpha2.KonnectGatewayControlPlaneList{},
			)
			require.NoError(t, gatewayv1.Install(scheme))

			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.clientObjects...)
			if tc.interceptorFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tc.interceptorFuncs)
			}
			cl := builder.Build()

			shouldProcess := shouldProcessObject[gwtypes.HTTPRoute](ctx, cl, route, logger)
			assert.Equal(t, tc.expectedResult, shouldProcess, tc.description)
		})
	}
}

func TestShouldProcessObject_Gateway(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// KonnectExtension for managed Gateway
	konnectExtension := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"gateway-operator.konghq.com/managed-by": "gateway",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
					Name:       "test-gateway",
					UID:        "test-gateway-uid",
				},
			},
		},
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "test-cp",
						},
					},
				},
			},
		},
	}

	// KonnectGatewayControlPlane for managed Gateway
	controlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cp",
			Namespace: "default",
		},
	}

	testCases := []struct {
		name             string
		setupGateway     func() *gwtypes.Gateway
		clientObjects    []client.Object
		interceptorFuncs *interceptor.Funcs
		expectedResult   bool
		description      string
	}{
		{
			name: "gateway with finalizer should be processed",
			setupGateway: func() *gwtypes.Gateway {
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-gateway",
						Namespace:  "default",
						UID:        "test-gateway-uid",
						Finalizers: []string{finalizerconst.HybridGatewayFinalizer},
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				}
				return gateway
			},
			clientObjects:    []client.Object{konnectExtension, controlPlane},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Gateway with our finalizer should be processed.",
		},
		{
			name: "gateway without finalizer but with our GatewayClass should be processed",
			setupGateway: func() *gwtypes.Gateway {
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-gateway",
						Namespace:  "default",
						UID:        "test-gateway-uid",
						Finalizers: []string{},
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				}
				return gateway
			},
			clientObjects:    []client.Object{konnectExtension, controlPlane},
			interceptorFuncs: nil,
			expectedResult:   true,
			description:      "Gateway using our GatewayClass should be processed.",
		},
		{
			name: "gateway without finalizer and other GatewayClass should be skipped",
			setupGateway: func() *gwtypes.Gateway {
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-gateway",
						Namespace:  "default",
						UID:        "test-gateway-uid",
						Finalizers: []string{},
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "other-class",
					},
				}
				return gateway
			},
			clientObjects:    []client.Object{},
			interceptorFuncs: nil,
			expectedResult:   false,
			description:      "Gateway using other GatewayClass should be skipped.",
		},
		{
			name: "gateway without finalizer and non-existent GatewayClass should be skipped",
			setupGateway: func() *gwtypes.Gateway {
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-gateway",
						Namespace:  "default",
						UID:        "test-gateway-uid",
						Finalizers: []string{},
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "non-existent",
					},
				}
				return gateway
			},
			clientObjects:    []client.Object{},
			interceptorFuncs: nil,
			expectedResult:   false,
			description:      "Gateway with non-existent GatewayClass should be skipped (not found case).",
		},
		{
			name: "gateway without finalizer with API error when fetching GatewayClass should be skipped",
			setupGateway: func() *gwtypes.Gateway {
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-gateway",
						Namespace:  "default",
						UID:        "test-gateway-uid",
						Finalizers: []string{},
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-class",
					},
				}
				return gateway
			},
			clientObjects: []client.Object{},
			interceptorFuncs: &interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return assert.AnError
				},
			},
			expectedResult: false,
			description:    "Gateway with API error when fetching GatewayClass should be skipped.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := tc.setupGateway()
			gateway.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gwtypes.GroupName,
				Version: "v1",
				Kind:    "Gateway",
			})

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
				&gwtypes.Gateway{}, &gwtypes.GatewayClass{},
			)
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: "konnect.konghq.com", Version: "v1alpha2"},
				&konnectv1alpha2.KonnectExtension{},
				&konnectv1alpha2.KonnectExtensionList{},
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				&konnectv1alpha2.KonnectGatewayControlPlaneList{},
			)
			require.NoError(t, gatewayv1.Install(scheme))

			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.clientObjects...)
			if tc.interceptorFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tc.interceptorFuncs)
			}
			cl := builder.Build()

			shouldProcess := shouldProcessObject[gwtypes.Gateway](ctx, cl, gateway, logger)
			assert.Equal(t, tc.expectedResult, shouldProcess, tc.description)
		})
	}
}

func TestRemoveFinalizerIfNotManaged_HTTPRoute(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create a supported Gateway (managed by KonnectExtension)
	supportedGateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "supported-gateway",
			Namespace: "default",
			UID:       "supported-gateway-uid",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "kong",
		},
	}

	// KonnectExtension for the supported Gateway
	konnectExtension := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "supported-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"gateway-operator.konghq.com/managed-by": "gateway",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
					Name:       "supported-gateway",
					UID:        "supported-gateway-uid",
				},
			},
		},
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "supported-cp",
						},
					},
				},
			},
		},
	}

	// KonnectGatewayControlPlane for the supported Gateway
	controlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "supported-cp",
			Namespace: "default",
		},
	}

	// Create an unsupported Gateway (no KonnectExtension)
	unsupportedGateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unsupported-gateway",
			Namespace: "default",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "other",
		},
	}

	tests := []struct {
		name                 string
		httpRoute            *gwtypes.HTTPRoute
		existingObjects      []client.Object
		interceptorFuncs     *interceptor.Funcs
		expectedRemoved      bool
		expectError          bool
		verifyFinalizer      bool
		expectedHasFinalizer bool
	}{
		{
			name: "no finalizer present - returns false",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "unsupported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			expectedRemoved: false,
			expectError:     false,
		},
		{
			name: "finalizer present and object is managed - keeps finalizer",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-route",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "supported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			expectedRemoved:      false,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: true,
		},
		{
			name: "finalizer present and object not managed - removes finalizer",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-route",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "unsupported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			expectedRemoved:      true,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: false,
		},
		{
			name: "finalizer present, not managed, object already deleted - returns false",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-route",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "unsupported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			interceptorFuncs: &interceptor.Funcs{
				Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return k8serrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "test-route")
				},
			},
			expectedRemoved: false,
			expectError:     false,
		},
		{
			name: "finalizer present, not managed, patch fails - returns error",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-route",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridHTTPRouteFinalizer},
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "unsupported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			interceptorFuncs: &interceptor.Funcs{
				Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return assert.AnError
				},
			},
			expectedRemoved: false,
			expectError:     true,
		},
		{
			name: "finalizer present with multiple finalizers, not managed - removes only our finalizer",
			httpRoute: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
					Finalizers: []string{
						"some-other-finalizer",
						finalizerconst.HybridHTTPRouteFinalizer,
						"yet-another-finalizer",
					},
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "unsupported-gateway",
							},
						},
					},
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
				supportedGateway,
				unsupportedGateway,
			},
			expectedRemoved:      true,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: false, // our finalizer should be removed, other finalizers remain
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the client with existing objects
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tt.existingObjects...)

			// Add the HTTPRoute to the client
			clientBuilder = clientBuilder.WithObjects(tt.httpRoute)

			// Add interceptor if provided
			if tt.interceptorFuncs != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(*tt.interceptorFuncs)
			}

			cl := clientBuilder.Build()

			// Call the function
			removed, err := removeFinalizerIfNotManaged[gwtypes.HTTPRoute](ctx, cl, tt.httpRoute, logger)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedRemoved, removed)

			// Verify finalizer state if requested
			if tt.verifyFinalizer {
				// Get the updated object from the client
				updated := &gwtypes.HTTPRoute{}
				err := cl.Get(ctx, client.ObjectKeyFromObject(tt.httpRoute), updated)
				require.NoError(t, err)

				assert.Equal(t, tt.expectedHasFinalizer, slices.Contains(updated.GetFinalizers(), finalizerconst.HybridHTTPRouteFinalizer), "finalizer presence mismatch")
			}
		})
	}
}

func TestRemoveFinalizerIfNotManaged_Gateway(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// KonnectExtension for the managed Gateway (with UID test-gateway-uid)
	konnectExtension := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"gateway-operator.konghq.com/managed-by": "gateway",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
					Name:       "test-gateway",
					UID:        "test-gateway-uid",
				},
			},
		},
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
				ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
					Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
							Name: "test-cp",
						},
					},
				},
			},
		},
	}

	// KonnectGatewayControlPlane for the supported Gateway
	controlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cp",
			Namespace: "default",
		},
	}

	tests := []struct {
		name                 string
		gateway              *gatewayv1.Gateway
		existingObjects      []client.Object
		interceptorFuncs     *interceptor.Funcs
		expectedRemoved      bool
		expectError          bool
		verifyFinalizer      bool
		expectedHasFinalizer bool
	}{
		{
			name: "no finalizer present - returns false",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			expectedRemoved: false,
			expectError:     false,
		},
		{
			name: "finalizer present and gateway is managed - keeps finalizer",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-gateway",
					Namespace:  "default",
					UID:        "test-gateway-uid",
					Finalizers: []string{finalizerconst.HybridGatewayFinalizer},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			expectedRemoved:      false,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: true,
		},
		{
			name: "finalizer present and gateway not managed - removes finalizer",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-gateway",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridGatewayFinalizer},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			expectedRemoved:      true,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: false,
		},
		{
			name: "finalizer present, not managed, object already deleted - returns false",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-gateway",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridGatewayFinalizer},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			interceptorFuncs: &interceptor.Funcs{
				Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return k8serrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "gateways"}, "test-gateway")
				},
			},
			expectedRemoved: false,
			expectError:     false,
		},
		{
			name: "finalizer present, not managed, patch fails - returns error",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-gateway",
					Namespace:  "default",
					Finalizers: []string{finalizerconst.HybridGatewayFinalizer},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			interceptorFuncs: &interceptor.Funcs{
				Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return assert.AnError
				},
			},
			expectedRemoved: false,
			expectError:     true,
		},
		{
			name: "finalizer present with multiple finalizers, not managed - removes only our finalizer",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					Finalizers: []string{
						"some-other-finalizer",
						finalizerconst.HybridGatewayFinalizer,
						"yet-another-finalizer",
					},
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: "other",
				},
			},
			existingObjects: []client.Object{
				konnectExtension,
				controlPlane,
			},
			expectedRemoved:      true,
			expectError:          false,
			verifyFinalizer:      true,
			expectedHasFinalizer: false, // our finalizer should be removed, other finalizers remain
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the client with existing objects
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(tt.existingObjects...)

			// Add the Gateway to the client
			clientBuilder = clientBuilder.WithObjects(tt.gateway)

			// Add interceptor if provided
			if tt.interceptorFuncs != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(*tt.interceptorFuncs)
			}

			cl := clientBuilder.Build()

			// Call the function
			removed, err := removeFinalizerIfNotManaged[gwtypes.Gateway](ctx, cl, tt.gateway, logger)

			// Verify expectations
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedRemoved, removed)

			// Verify finalizer state if requested
			if tt.verifyFinalizer {
				// Get the updated object from the client
				updated := &gatewayv1.Gateway{}
				err := cl.Get(ctx, client.ObjectKeyFromObject(tt.gateway), updated)
				require.NoError(t, err)

				assert.Equal(t, tt.expectedHasFinalizer, slices.Contains(updated.GetFinalizers(), finalizerconst.HybridGatewayFinalizer), "finalizer presence mismatch")

			}
		})
	}
}

func TestDesiredHasUpstreamNamed(t *testing.T) {
	upstream := func(name string) unstructured.Unstructured {
		u := unstructured.Unstructured{}
		u.SetName(name)
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongUpstream"})
		return u
	}
	notUpstream := func(name string) unstructured.Unstructured {
		u := unstructured.Unstructured{}
		u.SetName(name)
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"})
		return u
	}

	tests := []struct {
		name    string
		desired []unstructured.Unstructured
		search  string
		want    bool
	}{
		{
			name:    "empty list returns false",
			desired: nil,
			search:  "u1",
			want:    false,
		},
		{
			name:    "matching upstream returns true",
			desired: []unstructured.Unstructured{upstream("u1"), upstream("u2")},
			search:  "u1",
			want:    true,
		},
		{
			name:    "name present under different kind returns false",
			desired: []unstructured.Unstructured{notUpstream("u1")},
			search:  "u1",
			want:    false,
		},
		{
			name:    "name not in list returns false",
			desired: []unstructured.Unstructured{upstream("u2")},
			search:  "u1",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := make(map[string]struct{}, len(tt.desired))
			for _, obj := range tt.desired {
				if obj.GetKind() == "KongUpstream" {
					names[obj.GetName()] = struct{}{}
				}
			}
			assert.Equal(t, tt.want, desiredHasUpstreamNamed(names, tt.search))
		})
	}
}

func TestUpstreamTargetsProgrammed(t *testing.T) {
	ctx := t.Context()
	s := scheme.Get()
	ns := "ns"

	programmedCondition := metav1.Condition{
		Type:               "Programmed",
		Status:             metav1.ConditionTrue,
		Reason:             "Programmed",
		LastTransitionTime: metav1.Now(),
	}

	makeDesiredTarget := func(name, upstreamName string) unstructured.Unstructured {
		u := unstructured.Unstructured{}
		u.SetName(name)
		u.SetNamespace(ns)
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongTarget"})
		_ = unstructured.SetNestedField(u.Object, map[string]any{"name": upstreamName}, "spec", "upstreamRef")
		return u
	}

	makeTarget := func(name string, programmed bool) *configurationv1alpha1.KongTarget {
		tgt := &configurationv1alpha1.KongTarget{}
		tgt.SetName(name)
		tgt.SetNamespace(ns)
		if programmed {
			tgt.Status.Conditions = []metav1.Condition{programmedCondition}
		}
		return tgt
	}

	tests := []struct {
		name            string
		targets         []unstructured.Unstructured // pre-filtered targets for the upstream under test
		existing        []client.Object
		wantReady       bool
		wantErrContains string
		interceptorFn   *interceptor.Funcs
	}{
		{
			name:      "nil targets returns ready",
			targets:   nil,
			wantReady: true,
		},
		{
			name:      "empty targets slice returns ready",
			targets:   []unstructured.Unstructured{},
			wantReady: true,
		},
		{
			name:      "target not in cluster returns not ready",
			targets:   []unstructured.Unstructured{makeDesiredTarget("t1", "my-upstream")},
			existing:  nil,
			wantReady: false,
		},
		{
			name:      "target in cluster but not Programmed returns not ready",
			targets:   []unstructured.Unstructured{makeDesiredTarget("t1", "my-upstream")},
			existing:  []client.Object{makeTarget("t1", false)},
			wantReady: false,
		},
		{
			name:      "target in cluster and Programmed returns ready",
			targets:   []unstructured.Unstructured{makeDesiredTarget("t1", "my-upstream")},
			existing:  []client.Object{makeTarget("t1", true)},
			wantReady: true,
		},
		{
			name: "multiple targets all Programmed returns ready",
			targets: []unstructured.Unstructured{
				makeDesiredTarget("t1", "my-upstream"),
				makeDesiredTarget("t2", "my-upstream"),
			},
			existing:  []client.Object{makeTarget("t1", true), makeTarget("t2", true)},
			wantReady: true,
		},
		{
			name: "multiple targets, one not Programmed returns not ready",
			targets: []unstructured.Unstructured{
				makeDesiredTarget("t1", "my-upstream"),
				makeDesiredTarget("t2", "my-upstream"),
			},
			existing:  []client.Object{makeTarget("t1", true), makeTarget("t2", false)},
			wantReady: false,
		},
		{
			name:    "Get error for existing target is propagated",
			targets: []unstructured.Unstructured{makeDesiredTarget("t1", "my-upstream")},
			interceptorFn: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongTarget); ok {
						return assert.AnError
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantReady:       false,
			wantErrContains: "failed to get KongTarget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(s)
			if len(tt.existing) > 0 {
				builder = builder.WithObjects(tt.existing...)
			}
			if tt.interceptorFn != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptorFn)
			}
			cl := builder.Build()

			ready, err := upstreamTargetsProgrammed(ctx, cl, tt.targets)

			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantReady, ready)
		})
	}
}

func TestEnforceState_UpstreamGating(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()
	s := scheme.Get()
	ns := "ns"

	programmedCondition := metav1.Condition{
		Type:               "Programmed",
		Status:             metav1.ConditionTrue,
		Reason:             "Programmed",
		LastTransitionTime: metav1.Now(),
	}

	svcGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
	tgtGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongTarget"}
	upGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongUpstream"}
	routeGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"}
	kpbGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongPluginBinding"}

	makeObj := func(name string, gvk schema.GroupVersionKind, spec map[string]any) unstructured.Unstructured {
		u := newUnstructured(ns, name, gvk, nil)
		if spec != nil {
			_ = unstructured.SetNestedField(u.Object, spec, "spec")
		}
		return u
	}

	tests := []struct {
		name        string
		desired     []unstructured.Unstructured
		existing    []client.Object
		wantApplied bool
		wantWaiting bool
		wantErr     bool
	}{
		{
			// The service is gated (waiting=true) but the upstream prerequisite itself is
			// still created in the same pass (applied=true): the loop skips the service
			// but processes the upstream object that follows it.
			name: "KongService waits when its host upstream is missing from cluster",
			desired: []unstructured.Unstructured{
				makeObj("svc1", svcGVK, map[string]any{"host": "upstream1"}),
				makeObj("upstream1", upGVK, nil),
			},
			existing:    nil,
			wantApplied: true,
			wantWaiting: true,
		},
		{
			// Upstream exists but isn't Programmed yet. The service is gated; the
			// upstream is updated (no managed-fields yet → apply), so applied=true.
			name: "KongService waits when its host upstream is not Programmed",
			desired: []unstructured.Unstructured{
				makeObj("svc1", svcGVK, map[string]any{"host": "upstream1"}),
				makeObj("upstream1", upGVK, nil),
			},
			existing: func() []client.Object {
				up := &configurationv1alpha1.KongUpstream{}
				up.SetName("upstream1")
				up.SetNamespace(ns)
				return []client.Object{up}
			}(),
			wantApplied: true,
			wantWaiting: true,
		},
		{
			// Upstream is Programmed but targets are not yet. The service is gated on
			// targets; the target itself is still processed in the same pass (applied=true).
			name: "KongService waits when upstream is Programmed but targets are not",
			desired: []unstructured.Unstructured{
				makeObj("svc1", svcGVK, map[string]any{"host": "upstream1"}),
				makeObj("upstream1", upGVK, nil),
				makeObj("tgt1", tgtGVK, map[string]any{"upstreamRef": map[string]any{"name": "upstream1"}}),
			},
			existing: func() []client.Object {
				up := &configurationv1alpha1.KongUpstream{}
				up.SetName("upstream1")
				up.SetNamespace(ns)
				up.Status.Conditions = []metav1.Condition{programmedCondition}
				// KongTarget exists but is not Programmed.
				tgt := &configurationv1alpha1.KongTarget{}
				tgt.SetName("tgt1")
				tgt.SetNamespace(ns)
				return []client.Object{up, tgt}
			}(),
			wantApplied: true,
			wantWaiting: true,
		},
		{
			name: "KongService skips upstream gate when host is not a desired upstream (external)",
			desired: []unstructured.Unstructured{
				makeObj("svc1", svcGVK, map[string]any{"host": "external.example.com"}),
			},
			existing:    nil,
			wantApplied: true, // proceeds immediately, no upstream to wait for
			wantWaiting: false,
		},
		{
			name: "KongTarget waits when upstream exists but is not Programmed",
			desired: []unstructured.Unstructured{
				makeObj("tgt1", tgtGVK, map[string]any{"upstreamRef": map[string]any{"name": "upstream1"}}),
			},
			existing: func() []client.Object {
				up := &configurationv1alpha1.KongUpstream{}
				up.SetName("upstream1")
				up.SetNamespace(ns)
				return []client.Object{up}
			}(),
			wantApplied: false,
			wantWaiting: true,
		},
		{
			name: "KongRoute waits when service is missing",
			desired: []unstructured.Unstructured{
				makeObj("r1", routeGVK, map[string]any{
					"serviceRef": map[string]any{"namespacedRef": map[string]any{"name": "svc1"}},
				}),
			},
			existing:    nil,
			wantApplied: false,
			wantWaiting: true,
		},
		{
			name: "KongPluginBinding waits when route is missing",
			desired: []unstructured.Unstructured{
				makeObj("b1", kpbGVK, map[string]any{
					"targets": map[string]any{"routeRef": map[string]any{"name": "r1"}},
				}),
			},
			existing:    nil,
			wantApplied: false,
			wantWaiting: true,
		},
		{
			name: "KongPluginBinding waits when referenced KongService is not Programmed",
			desired: []unstructured.Unstructured{
				makeObj("b1", kpbGVK, map[string]any{
					"targets": map[string]any{
						"serviceRef": map[string]any{"name": "svc1", "kind": "KongService"},
					},
				}),
			},
			existing: func() []client.Object {
				svc := &configurationv1alpha1.KongService{}
				svc.SetName("svc1")
				svc.SetNamespace(ns)
				return []client.Object{svc}
			}(),
			wantApplied: false,
			wantWaiting: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(s)
			if len(tt.existing) > 0 {
				builder = builder.WithObjects(tt.existing...)
			}
			cl := builder.Build()

			fakeConv := &fakeHTTPRouteConverter{desired: tt.desired}
			applied, waiting, err := enforceState(ctx, cl, logger, fakeConv)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantApplied, applied)
			assert.Equal(t, tt.wantWaiting, waiting)
		})
	}
}
