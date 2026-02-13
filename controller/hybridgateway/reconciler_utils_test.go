package hybridgateway

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	finalizerconst "github.com/kong/kong-operator/controller/hybridgateway/const/finalizers"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
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
	desired []unstructured.Unstructured
	gvks    []schema.GroupVersionKind
	root    gwtypes.HTTPRoute
}

func (f *fakeHTTPRouteConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	return f.desired, nil
}
func (f *fakeHTTPRouteConverter) GetOutputStoreLen(ctx context.Context, logger logr.Logger) int {
	return len(f.desired)
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

func (f *fakeHTTPRouteConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	return false, false, nil
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
					return apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "httproutes"}, "test-route")
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
					return apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "gateways"}, "test-gateway")
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
