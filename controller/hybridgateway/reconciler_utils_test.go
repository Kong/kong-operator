package hybridgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8smanagedfields "k8s.io/apimachinery/pkg/util/managedfields"
	kubespec3 "k8s.io/kube-openapi/pkg/spec3"
	validationspec "k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	k8syaml "sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	finalizerconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/finalizers"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/pkg/ssa"
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

func ownedFieldsRaw(t *testing.T, tc k8smanagedfields.TypeConverter, obj *unstructured.Unstructured) []byte {
	t.Helper()
	typed, err := tc.ObjectToTyped(obj)
	require.NoError(t, err)
	set, err := typed.ToFieldSet()
	require.NoError(t, err)
	raw, err := set.ToJSON()
	require.NoError(t, err)
	return raw
}

// hybridGatewayTestCRDManifests lists the real, repo-checked-in CRD manifests
// for the Kong CRD kinds exercised by enforceState's tests below. Building the
// TypeConverter from these manifests (rather than a live cluster) keeps this
// suite fast/offline while still exercising the real CRD OpenAPI schemas.
var hybridGatewayTestCRDManifests = []string{
	"configuration.konghq.com_kongplugins.yaml",
	"configuration.konghq.com_kongtargets.yaml",
	"configuration.konghq.com_kongroutes.yaml",
	"configuration.konghq.com_kongpluginbindings.yaml",
	"configuration.konghq.com_kongservices.yaml",
	"configuration.konghq.com_kongupstreams.yaml",
}

// newTestTypeConverter is memoized (via [sync.OnceValue]) since it is expensive
// to build (reads + parses CRD manifests, builds OpenAPI schemas) and is
// read-only/safe to share across all tests in this file, including parallel
// subtests.
var newTestTypeConverter = sync.OnceValue(func() k8smanagedfields.TypeConverter {
	var specs []*kubespec3.OpenAPI
	for _, file := range hybridGatewayTestCRDManifests {
		path := filepath.Join("..", "..", "config", "crd", "kong-operator", file)
		raw, err := os.ReadFile(path)
		if err != nil {
			panic(fmt.Errorf("failed to read CRD manifest %s: %w", path, err))
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := k8syaml.Unmarshal(raw, crd); err != nil {
			panic(fmt.Errorf("failed to unmarshal CRD manifest %s: %w", path, err))
		}

		for _, v := range crd.Spec.Versions {
			spec, err := aexbuilder.BuildOpenAPIV3(crd, v.Name, aexbuilder.Options{})
			if err != nil {
				panic(fmt.Errorf("failed to build OpenAPI v3 for %s/%s: %w", crd.Name, v.Name, err))
			}
			specs = append(specs, spec)
		}
	}

	merged, err := aexbuilder.MergeSpecsV3(specs...)
	if err != nil {
		panic(fmt.Errorf("failed to merge CRD OpenAPI v3 specs: %w", err))
	}

	schemas := map[string]*validationspec.Schema{}
	if merged.Components != nil {
		maps.Copy(schemas, merged.Components.Schemas)
	}

	tc, err := k8smanagedfields.NewTypeConverter(schemas, false)
	if err != nil {
		panic(fmt.Errorf("failed to create TypeConverter: %w", err))
	}
	return tc
})

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

		applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
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
		// Default conditions include Programmed Unknown; ensure it’s not True.

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(svc).Build()

		applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
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

		applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.True(t, waiting)
	})

	// Case 4: KongService waits for referenced KongCertificate not Programmed.
	t.Run("service waits for not programmed certificate", func(t *testing.T) {
		svcGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
		desired := newUnstructured(ns, "svc1", svcGVK, nil)
		_ = unstructured.SetNestedField(desired.Object, map[string]any{
			"clientCertificateRef": map[string]any{"name": "cert1"},
		}, "spec")

		// KongCertificate exists but Programmed not True.
		cert := &configurationv1alpha1.KongCertificate{}
		cert.SetName("cert1")
		cert.SetNamespace(ns)

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(cert).Build()

		applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
		require.NoError(t, err)
		assert.False(t, applied)
		assert.True(t, waiting)
	})

	// Case 5: KongService proceeds when referenced KongCertificate is Programmed.
	t.Run("service proceeds when certificate is programmed", func(t *testing.T) {
		svcGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
		desired := newUnstructured(ns, "svc-ready", svcGVK, nil)
		_ = unstructured.SetNestedField(desired.Object, map[string]any{
			"clientCertificateRef": map[string]any{"name": "cert-ready"},
		}, "spec")

		// KongCertificate with Programmed=True.
		cert := &configurationv1alpha1.KongCertificate{}
		cert.SetName("cert-ready")
		cert.SetNamespace(ns)
		cert.Status.Conditions = []metav1.Condition{
			{
				Type:               "Programmed",
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Programmed",
			},
		}

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(cert).WithObjects(cert).Build()

		applied, _, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
		require.NoError(t, err)
		// The desired object was applied (created), so applied should be true.
		assert.True(t, applied)
	})

	// Case 6: KongService without clientCertificateRef proceeds immediately.
	t.Run("service without certificate ref proceeds immediately", func(t *testing.T) {
		svcGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
		desired := newUnstructured(ns, "svc-no-cert", svcGVK, nil)

		fakeConv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}}
		cl := fake.NewClientBuilder().WithScheme(s).Build()

		applied, _, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)
		require.NoError(t, err)
		assert.True(t, applied)
	})
}

func TestTranslate(t *testing.T) {
	tests := []struct {
		name               string
		translateRet       int
		translateErr       error
		expectedCount      int
		expectError        bool
		expectMalformedErr bool
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
		{
			name: "preserves malformed annotation sentinel through aggregated translate error",
			translateErr: fmt.Errorf("translation failed with 1 errors: %w", errors.Join(
				fmt.Errorf("failed to translate KongService for rule: %w", hgerrors.ErrMalformedAnnotation),
			)),
			expectError:        true,
			expectMalformedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := &fakeHTTPRouteConverter{translateRet: tt.translateRet, translateErr: tt.translateErr}

			count, err := translate[gwtypes.HTTPRoute](conv, t.Context(), logr.Discard())

			if tt.expectError {
				require.Error(t, err)
				if tt.expectMalformedErr {
					assert.ErrorIs(t, err, hgerrors.ErrMalformedAnnotation)
				}
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
			// Regression test: even when the preexisting object's values already
			// match desired, if hybridGatewayStateFieldManager has no managed-fields
			// entry yet on it (e.g. it's owned by a different field manager),
			// enforceState must still apply so ownership of the relevant fields is
			// claimed for hybridGatewayStateFieldManager. Otherwise the object would
			// never gain a managed-fields entry for our manager until a real value
			// changes, leaving SSA conflict detection ineffective for it in the
			// meantime.
			name:    "applies (claims ownership) when values match but no managed-fields entry exists for our manager",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-claim", "same.example")},
			preexisting: []client.Object{func() client.Object {
				u := makeDesiredService("svc-claim", "same.example")
				u.SetManagedFields([]metav1.ManagedFieldsEntry{{
					Manager:    "foreign-manager",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: kongServiceGVK.GroupVersion().String(),
					FieldsType: "FieldsV1",
					FieldsV1:   ssa.FieldsWithRawBytes(ownedFieldsRaw(t, newTestTypeConverter(), &u)),
				}})
				return &u
			}()},
			wantApplied: true,
			wantWaiting: false,
		},
		{
			name:    "returns typed conversion error for unsupported group",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{newUnstructured("default", "bad-group", schema.GroupVersionKind{Group: "invalid.group", Version: "v1", Kind: "Bad"}, nil)},
			preexisting: []client.Object{func() client.Object {
				u := newUnstructured("default", "bad-group", schema.GroupVersionKind{Group: "invalid.group", Version: "v1", Kind: "Bad"}, nil)
				return &u
			}()},
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to convert existing object to TypedValue",
		},
		{
			name:            "returns conversion error for invalid desired payload",
			scheme:          scheme.Get(),
			desired:         []unstructured.Unstructured{makeDesiredService("svc-convert", int64(12345))},
			preexisting:     []client.Object{func() client.Object { u := makeDesiredService("svc-convert", "ok.example"); return &u }()},
			wantApplied:     false,
			wantWaiting:     false,
			wantErrContains: "failed to convert desired object to TypedValue",
		},
		{
			name:    "returns conflict error during create apply",
			scheme:  scheme.Get(),
			desired: []unstructured.Unstructured{makeDesiredService("svc-create-conflict", "conflict.example")},
			interceptor: &interceptor.Funcs{
				Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
					return apierrors.NewConflict(schema.GroupResource{Group: "configuration.konghq.com", Resource: "kongservices"}, "svc-create-conflict", assert.AnError)
				},
			},
			wantErrContains: "conflict during apply of object kind KongService obj default/svc-create-conflict",
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
			wantErrContains: "failed to apply object kind KongService obj default/svc-update-err",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(tt.scheme).WithReturnManagedFields()
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
			applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, conv)

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

func TestEnforceState_HybridGatewaysAnnotationConverges(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()
	tc := newTestTypeConverter()
	gvk := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1", Kind: "KongPlugin"}
	route := gwtypes.HTTPRoute{
		TypeMeta:   metav1.TypeMeta{APIVersion: gatewayv1.GroupVersion.String(), Kind: "HTTPRoute"},
		ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "ns"},
	}
	routeRef := client.ObjectKeyFromObject(&route).String()

	makePlugin := func(gateway string) unstructured.Unstructured {
		u := newUnstructured("ns", "shared-plugin", gvk, map[string]string{
			consts.GatewayOperatorManagedByLabel: "HTTPRoute",
		})
		u.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridGatewaysAnnotation:        gateway,
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: routeRef,
		})
		_ = unstructured.SetNestedField(u.Object, "request-transformer", "plugin")
		_ = unstructured.SetNestedStringSlice(u.Object, []string{"X-Test:true"}, "config", "add", "headers")
		return u
	}
	existing := makePlugin("ns/gateway-a")
	existing.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    hybridGatewayStateFieldManager,
		Operation:  metav1.ManagedFieldsOperationApply,
		APIVersion: gvk.GroupVersion().String(),
		FieldsType: "FieldsV1",
		FieldsV1:   ssa.FieldsWithRawBytes(ownedFieldsRaw(t, tc, &existing)),
	}})
	var appliedObject map[string]any
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithReturnManagedFields().
		WithObjects(&existing).
		WithInterceptorFuncs(interceptor.Funcs{
			Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
				raw, err := json.Marshal(obj)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(raw, &appliedObject))
				return nil
			},
		}).
		Build()

	desiredForGatewayB := makePlugin("ns/gateway-b")
	conv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desiredForGatewayB}, root: route}
	applied, waiting, err := enforceState(ctx, cl, tc, logger, conv)
	require.NoError(t, err)
	require.True(t, applied)
	require.False(t, waiting)

	appliedAnnotations, _, err := unstructured.NestedStringMap(appliedObject, "metadata", "annotations")
	require.NoError(t, err)
	assert.Equal(
		t,
		"ns/gateway-a,ns/gateway-b",
		appliedAnnotations[consts.GatewayOperatorHybridGatewaysAnnotation],
	)

	converged := makePlugin("ns/gateway-a,ns/gateway-b")
	converged.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    hybridGatewayStateFieldManager,
		Operation:  metav1.ManagedFieldsOperationApply,
		APIVersion: gvk.GroupVersion().String(),
		FieldsType: "FieldsV1",
		FieldsV1:   ssa.FieldsWithRawBytes(ownedFieldsRaw(t, tc, &converged)),
	}})
	cl = fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithReturnManagedFields().
		WithObjects(&converged).
		Build()
	desiredForGatewayA := makePlugin("ns/gateway-a")
	conv.desired = []unstructured.Unstructured{desiredForGatewayA}
	applied, waiting, err = enforceState(ctx, cl, tc, logger, conv)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.False(t, waiting)
}

func TestCleanOrphanedResources(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	routeGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"}
	serviceGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}

	root := &gwtypes.HTTPRoute{
		TypeMeta:   metav1.TypeMeta{APIVersion: gatewayv1.GroupVersion.String(), Kind: "HTTPRoute"},
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "ns"},
	}
	ownerLabels := metadata.BuildLabels(root, nil)
	ownerAnnotation := fmt.Sprintf("%s/%s", root.Namespace, root.Name)

	// makeOrphan creates an object that is owned (has owner labels) and has the route
	// annotation that the fake HandleOrphanedResource uses to allow deletion.
	makeOrphan := func(name string, gvk schema.GroupVersionKind) unstructured.Unstructured {
		obj := newUnstructured("ns", name, gvk, ownerLabels)
		obj.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: ownerAnnotation,
		})
		return obj
	}

	// makeDeletingOrphan is an orphan that is already being deleted (has DeletionTimestamp).
	makeDeletingOrphan := func(name string, gvk schema.GroupVersionKind) unstructured.Unstructured {
		obj := makeOrphan(name, gvk)
		ts := metav1.Now()
		obj.SetDeletionTimestamp(&ts)
		obj.SetFinalizers([]string{"example.com/test"})
		return obj
	}

	// makeDesired creates an object that represents a desired resource in the cluster.
	makeDesired := func(name string, gvk schema.GroupVersionKind) unstructured.Unstructured {
		return newUnstructured("ns", name, gvk, ownerLabels)
	}

	// listNames returns the names of all objects in the cluster for a given GVK.
	listNames := func(t *testing.T, cl client.Client, gvk schema.GroupVersionKind) []string {
		t.Helper()
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		require.NoError(t, cl.List(ctx, list))
		names := make([]string, len(list.Items))
		for i, item := range list.Items {
			names[i] = item.GetName()
		}
		return names
	}

	tests := []struct {
		name                 string
		gvks                 []schema.GroupVersionKind
		desired              []unstructured.Unstructured // what the converter reports as desired
		existing             []unstructured.Unstructured // what's in the cluster
		runUntilDone         bool                        // loop until requeue=false
		outputStoreErr       error
		interceptorFn        *interceptor.Funcs
		handleOrphanedResErr bool
		wantRequeue          bool
		wantErrContains      string
		wantRemaining        map[schema.GroupVersionKind][]string
	}{
		// --- basic cleanup ---
		{
			name:        "no objects → requeue=false",
			gvks:        []schema.GroupVersionKind{routeGVK},
			wantRequeue: false,
		},
		{
			name:          "no orphans → requeue=false",
			gvks:          []schema.GroupVersionKind{routeGVK},
			desired:       []unstructured.Unstructured{makeDesired("r1", routeGVK)},
			existing:      []unstructured.Unstructured{makeDesired("r1", routeGVK)},
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {"r1"}},
		},
		{
			name:          "single orphan is deleted",
			gvks:          []schema.GroupVersionKind{routeGVK},
			desired:       []unstructured.Unstructured{makeDesired("r1", routeGVK)},
			existing:      []unstructured.Unstructured{makeDesired("r1", routeGVK), makeOrphan("r-orphan", routeGVK)},
			runUntilDone:  true,
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {"r1"}},
		},
		{
			name:          "multiple orphans in one GVK are all deleted",
			gvks:          []schema.GroupVersionKind{routeGVK},
			desired:       []unstructured.Unstructured{makeDesired("r1", routeGVK)},
			existing:      []unstructured.Unstructured{makeDesired("r1", routeGVK), makeOrphan("orphan-a", routeGVK), makeOrphan("orphan-b", routeGVK)},
			runUntilDone:  true,
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {"r1"}},
		},
		{
			name:          "all cluster objects are orphans → all deleted",
			gvks:          []schema.GroupVersionKind{routeGVK},
			existing:      []unstructured.Unstructured{makeOrphan("orphan-a", routeGVK), makeOrphan("orphan-b", routeGVK)},
			runUntilDone:  true,
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {}},
		},
		{
			name:    "desired name overlaps with orphan name: desired-set object is kept",
			gvks:    []schema.GroupVersionKind{routeGVK},
			desired: []unstructured.Unstructured{makeDesired("r1", routeGVK), makeDesired("r2", routeGVK)},
			existing: []unstructured.Unstructured{
				makeDesired("r1", routeGVK),
				makeDesired("r2", routeGVK),
				makeOrphan("r3", routeGVK), // orphan not in desired
			},
			runUntilDone:  true,
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {"r1", "r2"}},
		},
		{
			name:    "object without owner labels is not matched by selector and not deleted",
			gvks:    []schema.GroupVersionKind{routeGVK},
			desired: []unstructured.Unstructured{makeDesired("r1", routeGVK)},
			existing: []unstructured.Unstructured{
				makeDesired("r1", routeGVK),
				newUnstructured("ns", "unrelated", routeGVK, map[string]string{"other": "label"}),
			},
			wantRequeue:   false,
			wantRemaining: map[schema.GroupVersionKind][]string{routeGVK: {"r1", "unrelated"}},
		},
		// --- multi-GVK eventual cleanup ---
		{
			name: "multiple GVKs: all orphans are eventually cleaned",
			gvks: []schema.GroupVersionKind{routeGVK, serviceGVK},
			desired: []unstructured.Unstructured{
				makeDesired("r1", routeGVK),
				makeDesired("svc1", serviceGVK),
			},
			existing: []unstructured.Unstructured{
				makeDesired("r1", routeGVK), makeOrphan("r-orphan", routeGVK),
				makeDesired("svc1", serviceGVK), makeOrphan("svc-orphan", serviceGVK),
			},
			runUntilDone: true,
			wantRequeue:  false,
			wantRemaining: map[schema.GroupVersionKind][]string{
				routeGVK:   {"r1"},
				serviceGVK: {"svc1"},
			},
		},
		// --- gating: orphanCleanupOptions{waitForDeletes: true} ---
		{
			// After deleting the orphan in GVK1, the function returns immediately without
			// processing GVK2. A second call is needed to clean GVK2 orphans.
			name:        "orphan deleted in GVK1 stops processing GVK2 in same pass",
			gvks:        []schema.GroupVersionKind{routeGVK, serviceGVK},
			existing:    []unstructured.Unstructured{makeOrphan("r-orphan", routeGVK), makeOrphan("svc-orphan", serviceGVK)},
			wantRequeue: true,
			wantRemaining: map[schema.GroupVersionKind][]string{
				routeGVK:   {},
				serviceGVK: {"svc-orphan"},
			},
		},
		{
			// An in-deletion object in GVK1 also triggers an early return, blocking GVK2.
			name:        "in-deletion orphan in GVK1 stops processing GVK2",
			gvks:        []schema.GroupVersionKind{routeGVK, serviceGVK},
			existing:    []unstructured.Unstructured{makeDeletingOrphan("r-deleting", routeGVK), makeOrphan("svc-orphan", serviceGVK)},
			wantRequeue: true,
			wantRemaining: map[schema.GroupVersionKind][]string{
				routeGVK:   {"r-deleting"},
				serviceGVK: {"svc-orphan"},
			},
		},
		// --- error paths ---
		{
			name:            "GetOutputStore error is propagated",
			gvks:            []schema.GroupVersionKind{routeGVK},
			outputStoreErr:  assert.AnError,
			wantErrContains: "failed to get desired objects from converter for cleanup",
		},
		{
			name: "cl.List error is propagated",
			gvks: []schema.GroupVersionKind{routeGVK},
			interceptorFn: &interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return assert.AnError
				},
			},
			wantErrContains: "unable to list objects with gvk",
		},
		{
			name:     "cl.Delete error is propagated",
			gvks:     []schema.GroupVersionKind{routeGVK},
			existing: []unstructured.Unstructured{makeOrphan("r-orphan", routeGVK)},
			interceptorFn: &interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					return assert.AnError
				},
			},
			wantErrContains: "failed to delete orphaned resource",
		},
		{
			name:                 "HandleOrphanedResource error is propagated",
			gvks:                 []schema.GroupVersionKind{routeGVK},
			existing:             []unstructured.Unstructured{makeOrphan("r-orphan", routeGVK)},
			handleOrphanedResErr: true,
			wantErrContains:      "failed to handle orphaned resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allObjs := make([]client.Object, len(tt.existing))
			for i := range tt.existing {
				allObjs[i] = &tt.existing[i]
			}
			builder := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(allObjs...)
			if tt.interceptorFn != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptorFn)
			}
			cl := builder.Build()

			var requeue bool
			var err error
			for {
				if tt.handleOrphanedResErr {
					conv := &fakeHTTPRouteConverterWithHandleErr{
						fakeHTTPRouteConverter: fakeHTTPRouteConverter{
							gvks:           tt.gvks,
							root:           *root,
							desired:        tt.desired,
							outputStoreErr: tt.outputStoreErr,
						},
					}
					requeue, err = cleanOrphanedResources(ctx, cl, logger, conv, orphanCleanupOptions{waitForDeletes: true})
				} else {
					conv := &fakeHTTPRouteConverter{
						gvks:           tt.gvks,
						root:           *root,
						desired:        tt.desired,
						outputStoreErr: tt.outputStoreErr,
					}
					requeue, err = cleanOrphanedResources(ctx, cl, logger, conv, orphanCleanupOptions{waitForDeletes: true})
				}
				if !tt.runUntilDone || !requeue || err != nil {
					break
				}
			}

			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRequeue, requeue)

			for gvk, wantNames := range tt.wantRemaining {
				assert.ElementsMatch(t, wantNames, listNames(t, cl, gvk),
					"remaining names for GVK %s", gvk)
			}
		})
	}
}

func TestCleanOrphanedResourcesWaitBehavior(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()
	gvks := []schema.GroupVersionKind{
		{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"},
		{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"},
	}
	root := &gwtypes.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gatewayv1.GroupVersion.String(),
			Kind:       "HTTPRoute",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "httproute-owner",
			Namespace: "ns",
		},
	}
	ownerLabels := metadata.BuildLabels(root, nil)

	deletingRoute := newUnstructured("ns", "deleting-route", gvks[0], ownerLabels)
	deletingTimestamp := metav1.NewTime(time.Now())
	deletingRoute.SetDeletionTimestamp(&deletingTimestamp)
	deletingRoute.SetFinalizers([]string{"example.com/finalizer"})

	service := newUnstructured("ns", "service", gvks[1], ownerLabels)
	for _, obj := range []*unstructured.Unstructured{&deletingRoute, &service} {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation] = "ns/httproute-owner"
		obj.SetAnnotations(annotations)
	}

	newClient := func() client.Client {
		return fake.NewClientBuilder().
			WithScheme(runtime.NewScheme()).
			WithObjects(deletingRoute.DeepCopy(), service.DeepCopy()).
			Build()
	}
	fakeConv := &fakeHTTPRouteConverter{
		gvks: gvks,
		root: *root,
	}

	t.Run("normal cleanup waits for deleting child before later GVKs", func(t *testing.T) {
		cl := newClient()
		requeue, err := cleanOrphanedResources[gwtypes.HTTPRoute, *gwtypes.HTTPRoute](ctx, cl, logger, fakeConv, orphanCleanupOptions{waitForDeletes: true})
		require.NoError(t, err)
		require.True(t, requeue)

		serviceList := &unstructured.UnstructuredList{}
		serviceList.SetGroupVersionKind(gvks[1])
		require.NoError(t, cl.List(ctx, serviceList))
		require.Len(t, serviceList.Items, 1)
	})

	t.Run("cleanup requeues when orphan delete conflicts", func(t *testing.T) {
		conflictGVK := []schema.GroupVersionKind{
			{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"},
		}
		conflictService := newUnstructured("ns", "service", conflictGVK[0], ownerLabels)
		conflictService.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/httproute-owner",
		})
		cl := fake.NewClientBuilder().
			WithScheme(runtime.NewScheme()).
			WithObjects(conflictService.DeepCopy()).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == conflictService.GetName() {
						return apierrors.NewConflict(
							schema.GroupResource{Group: "configuration.konghq.com", Resource: "kongservices"},
							obj.GetName(),
							assert.AnError,
						)
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).
			Build()

		conflictConv := &fakeHTTPRouteConverter{
			gvks: conflictGVK,
			root: *root,
		}

		requeue, err := cleanOrphanedResources[gwtypes.HTTPRoute, *gwtypes.HTTPRoute](ctx, cl, logger, conflictConv, orphanCleanupOptions{waitForDeletes: true})
		require.NoError(t, err)
		require.True(t, requeue)

		serviceList := &unstructured.UnstructuredList{}
		serviceList.SetGroupVersionKind(conflictGVK[0])
		require.NoError(t, cl.List(ctx, serviceList))
		require.Len(t, serviceList.Items, 1)
	})
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

	annotationValue, exists := annotations[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation]
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
	ctx := t.Context()
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

// TestShouldProcessObject_TCPRoute guards against regressions of the missing
// *gwtypes.TCPRoute case in referencesSupportedGateway: without it, a fresh
// TCPRoute (no finalizer yet) referencing a supported Gateway was never picked
// up for processing, so it never got a finalizer or a status.
func TestShouldProcessObject_TCPRoute(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

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

	ourControlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-cp",
			Namespace: "default",
		},
	}

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
		name           string
		setupRoute     func() *gwtypes.TCPRoute
		clientObjects  []client.Object
		expectedResult bool
		description    string
	}{
		{
			name: "object with finalizer should be processed",
			setupRoute: func() *gwtypes.TCPRoute {
				return &gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{finalizerconst.HybridTCPRouteFinalizer},
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: true,
			description:    "Objects with our finalizer should be processed regardless of Gateway reference.",
		},
		{
			name: "object without finalizer but referencing our Gateway should be processed",
			setupRoute: func() *gwtypes.TCPRoute {
				gatewayName := gwtypes.ObjectName("our-gateway")
				return &gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{ourGateway, ourKonnectExtension, ourControlPlane},
			expectedResult: true,
			description:    "Objects without finalizer but referencing our Gateway should be processed.",
		},
		{
			name: "object without finalizer referencing other Gateway should be skipped",
			setupRoute: func() *gwtypes.TCPRoute {
				gatewayName := gwtypes.ObjectName("other-gateway")
				return &gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.TCPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{otherGateway},
			expectedResult: false,
			description:    "Objects without finalizer referencing unsupported Gateway should be skipped.",
		},
		{
			name: "object without finalizer and no Gateway reference should be skipped",
			setupRoute: func() *gwtypes.TCPRoute {
				return &gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: false,
			description:    "Objects without finalizer and no Gateway reference should be skipped.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			route := tc.setupRoute()
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gwtypes.GroupName,
				Version: "v1",
				Kind:    "TCPRoute",
			})

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
				&gwtypes.TCPRoute{}, &gwtypes.Gateway{}, &gwtypes.GatewayClass{},
			)
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: "konnect.konghq.com", Version: "v1alpha2"},
				&konnectv1alpha2.KonnectExtension{},
				&konnectv1alpha2.KonnectExtensionList{},
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				&konnectv1alpha2.KonnectGatewayControlPlaneList{},
			)
			require.NoError(t, gatewayv1.Install(scheme))

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.clientObjects...).
				Build()

			shouldProcess := shouldProcessObject[gwtypes.TCPRoute](ctx, cl, route, logger)
			assert.Equal(t, tc.expectedResult, shouldProcess, tc.description)
		})
	}
}

// TestShouldProcessObject_UDPRoute guards against regressions of the missing
// *gwtypes.UDPRoute case in referencesSupportedGateway: without it, a fresh
// UDPRoute (no finalizer yet) referencing a supported Gateway was never picked
// up for processing, so it never got a finalizer or a status.
func TestShouldProcessObject_UDPRoute(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

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

	ourControlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-cp",
			Namespace: "default",
		},
	}

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
		name           string
		setupRoute     func() *gwtypes.UDPRoute
		clientObjects  []client.Object
		expectedResult bool
		description    string
	}{
		{
			name: "object with finalizer should be processed",
			setupRoute: func() *gwtypes.UDPRoute {
				return &gwtypes.UDPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{finalizerconst.HybridUDPRouteFinalizer},
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: true,
			description:    "Objects with our finalizer should be processed regardless of Gateway reference.",
		},
		{
			name: "object without finalizer but referencing our Gateway should be processed",
			setupRoute: func() *gwtypes.UDPRoute {
				gatewayName := gwtypes.ObjectName("our-gateway")
				return &gwtypes.UDPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.UDPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{ourGateway, ourKonnectExtension, ourControlPlane},
			expectedResult: true,
			description:    "Objects without finalizer but referencing our Gateway should be processed.",
		},
		{
			name: "object without finalizer referencing other Gateway should be skipped",
			setupRoute: func() *gwtypes.UDPRoute {
				gatewayName := gwtypes.ObjectName("other-gateway")
				return &gwtypes.UDPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.UDPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{otherGateway},
			expectedResult: false,
			description:    "Objects without finalizer referencing unsupported Gateway should be skipped.",
		},
		{
			name: "object without finalizer and no Gateway reference should be skipped",
			setupRoute: func() *gwtypes.UDPRoute {
				return &gwtypes.UDPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: false,
			description:    "Objects without finalizer and no Gateway reference should be skipped.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			route := tc.setupRoute()
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gwtypes.GroupName,
				Version: "v1",
				Kind:    "UDPRoute",
			})

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
				&gwtypes.UDPRoute{}, &gwtypes.Gateway{}, &gwtypes.GatewayClass{},
			)
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: "konnect.konghq.com", Version: "v1alpha2"},
				&konnectv1alpha2.KonnectExtension{},
				&konnectv1alpha2.KonnectExtensionList{},
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				&konnectv1alpha2.KonnectGatewayControlPlaneList{},
			)
			require.NoError(t, gatewayv1.Install(scheme))

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.clientObjects...).
				Build()

			shouldProcess := shouldProcessObject[gwtypes.UDPRoute](ctx, cl, route, logger)
			assert.Equal(t, tc.expectedResult, shouldProcess, tc.description)
		})
	}
}

// TestShouldProcessObject_GRPCRoute guards against regressions of the *gwtypes.GRPCRoute
// case in referencesSupportedGateway.
func TestShouldProcessObject_GRPCRoute(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

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

	ourControlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "our-cp",
			Namespace: "default",
		},
	}

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
		name           string
		setupRoute     func() *gwtypes.GRPCRoute
		clientObjects  []client.Object
		expectedResult bool
		description    string
	}{
		{
			name: "object with finalizer should be processed",
			setupRoute: func() *gwtypes.GRPCRoute {
				return &gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-route",
						Namespace:  "default",
						Finalizers: []string{finalizerconst.HybridGRPCRouteFinalizer},
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: true,
			description:    "Objects with our finalizer should be processed regardless of Gateway reference.",
		},
		{
			name: "object without finalizer but referencing our Gateway should be processed",
			setupRoute: func() *gwtypes.GRPCRoute {
				gatewayName := gwtypes.ObjectName("our-gateway")
				return &gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{ourGateway, ourKonnectExtension, ourControlPlane},
			expectedResult: true,
			description:    "Objects without finalizer but referencing our Gateway should be processed.",
		},
		{
			name: "object without finalizer referencing other Gateway should be skipped",
			setupRoute: func() *gwtypes.GRPCRoute {
				gatewayName := gwtypes.ObjectName("other-gateway")
				return &gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.GRPCRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gatewayName},
							},
						},
					},
				}
			},
			clientObjects:  []client.Object{otherGateway},
			expectedResult: false,
			description:    "Objects without finalizer referencing unsupported Gateway should be skipped.",
		},
		{
			name: "object without finalizer and no Gateway reference should be skipped",
			setupRoute: func() *gwtypes.GRPCRoute {
				return &gwtypes.GRPCRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
				}
			},
			clientObjects:  []client.Object{},
			expectedResult: false,
			description:    "Objects without finalizer and no Gateway reference should be skipped.",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			route := tc.setupRoute()
			route.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gwtypes.GroupName,
				Version: "v1",
				Kind:    "GRPCRoute",
			})

			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: gatewayv1.GroupVersion.Group, Version: gatewayv1.GroupVersion.Version},
				&gwtypes.GRPCRoute{}, &gwtypes.Gateway{}, &gwtypes.GatewayClass{},
			)
			scheme.AddKnownTypes(
				schema.GroupVersion{Group: "konnect.konghq.com", Version: "v1alpha2"},
				&konnectv1alpha2.KonnectExtension{},
				&konnectv1alpha2.KonnectExtensionList{},
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				&konnectv1alpha2.KonnectGatewayControlPlaneList{},
			)
			require.NoError(t, gatewayv1.Install(scheme))

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.clientObjects...).
				Build()

			shouldProcess := shouldProcessObject[gwtypes.GRPCRoute](ctx, cl, route, logger)
			assert.Equal(t, tc.expectedResult, shouldProcess, tc.description)
		})
	}
}

func TestShouldProcessObject_Gateway(t *testing.T) {
	ctx := t.Context()
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
	ctx := t.Context()
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
			applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, fakeConv)

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

func TestEnforceState_KongRouteRequiresLiveServiceRouteAnnotation(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()
	s := scheme.Get()
	ns := "ns"
	root := gwtypes.HTTPRoute{
		TypeMeta:   metav1.TypeMeta{APIVersion: gatewayv1.GroupVersion.String(), Kind: "HTTPRoute"},
		ObjectMeta: metav1.ObjectMeta{Name: "httproute-owner", Namespace: ns},
	}
	routeRef := ns + "/httproute-owner"

	programmedCondition := metav1.Condition{
		Type:               "Programmed",
		Status:             metav1.ConditionTrue,
		Reason:             "Programmed",
		LastTransitionTime: metav1.Now(),
	}

	svcGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongService"}
	routeGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongRoute"}

	makeDesiredService := func(name string) unstructured.Unstructured {
		u := newUnstructured(ns, name, svcGVK, nil)
		_ = unstructured.SetNestedField(u.Object, "example.com", "spec", "host")
		_ = unstructured.SetNestedField(u.Object, int64(80), "spec", "port")
		_ = unstructured.SetNestedField(u.Object, "httproute", "spec", "protocol")
		u.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: "ns/other-route," + routeRef,
		})
		return u
	}
	makeDesiredRoute := func(name, serviceName string) unstructured.Unstructured {
		u := newUnstructured(ns, name, routeGVK, nil)
		_ = unstructured.SetNestedField(u.Object, map[string]any{
			"namespacedRef": map[string]any{"name": serviceName},
		}, "spec", "serviceRef")
		return u
	}
	makeProgrammedService := func(name string, annotation string) *configurationv1alpha1.KongService {
		svc := &configurationv1alpha1.KongService{}
		svc.SetName(name)
		svc.SetNamespace(ns)
		svc.SetAnnotations(map[string]string{
			consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation: annotation,
		})
		svc.Status.Conditions = []metav1.Condition{programmedCondition}
		return svc
	}

	desired := []unstructured.Unstructured{
		makeDesiredService("svc1"),
		makeDesiredRoute("route1", "svc1"),
		makeDesiredService("svc2"),
		makeDesiredRoute("route2", "svc2"),
	}
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			makeProgrammedService("svc1", routeRef),
			makeProgrammedService("svc2", "ns/other-route"),
		).
		Build()

	conv := &fakeHTTPRouteConverter{desired: desired, root: root}
	applied, waiting, err := enforceState(ctx, cl, newTestTypeConverter(), logger, conv)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.True(t, waiting)

	var svc2 configurationv1alpha1.KongService
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "svc2"}, &svc2))
	assert.Equal(t, "ns/other-route,"+routeRef, svc2.GetAnnotations()[consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation])

	var route2 configurationv1alpha1.KongRoute
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "route2"}, &route2))
}

func TestEnsureHybridRouteAnnotationOnKongService_Idempotent(t *testing.T) {
	ctx := t.Context()
	s := scheme.Get()
	ns := "ns"
	annotationKey := consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation
	routeRef := ns + "/httproute-owner"

	svc := &configurationv1alpha1.KongService{}
	svc.SetName("svc1")
	svc.SetNamespace(ns)
	svc.SetAnnotations(map[string]string{
		annotationKey: "ns/other-route",
	})

	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(svc).
		Build()

	// First call adds routeRef to the annotation.
	require.NoError(t, ensureHybridRouteAnnotationOnKongService(ctx, cl, svc, annotationKey, routeRef))
	assert.Equal(t, "ns/other-route,"+routeRef, svc.GetAnnotations()[annotationKey])

	var got configurationv1alpha1.KongService
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "svc1"}, &got))
	assert.Equal(t, "ns/other-route,"+routeRef, got.GetAnnotations()[annotationKey])

	// Second call with the already-annotated object is a no-op: no duplicate
	// entry, no patch against a stale resourceVersion.
	require.NoError(t, ensureHybridRouteAnnotationOnKongService(ctx, cl, svc, annotationKey, routeRef))
	assert.Equal(t, "ns/other-route,"+routeRef, svc.GetAnnotations()[annotationKey])

	// Calling again with a freshly-fetched copy (simulating a re-reconcile)
	// must also be a no-op and must not error out on the optimistic lock.
	require.NoError(t, ensureHybridRouteAnnotationOnKongService(ctx, cl, &got, annotationKey, routeRef))
	assert.Equal(t, "ns/other-route,"+routeRef, got.GetAnnotations()[annotationKey])

	var final configurationv1alpha1.KongService
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: "svc1"}, &final))
	assert.Equal(t, "ns/other-route,"+routeRef, final.GetAnnotations()[annotationKey])
}

// fakeHTTPRouteConverterWithHandleErr wraps fakeHTTPRouteConverter and returns an error
// from HandleOrphanedResource so we can test error propagation in cleanOrphanedResources.
type fakeHTTPRouteConverterWithHandleErr struct {
	fakeHTTPRouteConverter
}

func (f *fakeHTTPRouteConverterWithHandleErr) HandleOrphanedResource(_ context.Context, _ logr.Logger, _ *unstructured.Unstructured) (bool, error) {
	return false, assert.AnError
}

func TestRemoveFinalizerIfNotManaged_Gateway(t *testing.T) {
	ctx := t.Context()
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

func TestHybridRouteAnnotationInfo(t *testing.T) {
	tests := []struct {
		name    string
		wantKey string
		wantRef string
		runFn   func() (string, string)
	}{
		{
			name:    "HTTPRoute returns HTTPRoute annotation key and ns/name",
			wantKey: consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation,
			wantRef: "ns/route-a",
			runFn: func() (string, string) {
				return hybridRouteAnnotationInfo(gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{Name: "route-a", Namespace: "ns"},
				})
			},
		},
		{
			name:    "TLSRoute returns TLSRoute annotation key and ns/name",
			wantKey: consts.GatewayOperatorHybridRoutesTLSRouteAnnotation,
			wantRef: "ns/tls-route",
			runFn: func() (string, string) {
				return hybridRouteAnnotationInfo(gwtypes.TLSRoute{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-route", Namespace: "ns"},
				})
			},
		},
		{
			name:    "TCPRoute returns TCPRoute annotation key and ns/name",
			wantKey: consts.GatewayOperatorHybridRoutesTCPRouteAnnotation,
			wantRef: "ns/tcp-route",
			runFn: func() (string, string) {
				return hybridRouteAnnotationInfo(gwtypes.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{Name: "tcp-route", Namespace: "ns"},
				})
			},
		},
		{
			name:    "UDPRoute returns UDPRoute annotation key and ns/name",
			wantKey: consts.GatewayOperatorHybridRoutesUDPRouteAnnotation,
			wantRef: "ns/udp-route",
			runFn: func() (string, string) {
				return hybridRouteAnnotationInfo(gwtypes.UDPRoute{
					ObjectMeta: metav1.ObjectMeta{Name: "udp-route", Namespace: "ns"},
				})
			},
		},
		{
			name:    "Gateway returns empty strings",
			wantKey: "",
			wantRef: "",
			runFn: func() (string, string) {
				return hybridRouteAnnotationInfo(gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "ns"},
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ref := tt.runFn()
			assert.Equal(t, tt.wantKey, key)
			assert.Equal(t, tt.wantRef, ref)
		})
	}
}

func TestMergeHybridRouteAnnotation(t *testing.T) {
	upstreamGVK := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1alpha1", Kind: "KongUpstream"}
	annotationKey := consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation
	routeRef := "ns/route-a"

	tests := []struct {
		name           string
		existingAnns   map[string]string
		desiredAnns    map[string]string
		wantAnnotation string
	}{
		{
			name:           "empty existing sets annotation to routeRef",
			existingAnns:   nil,
			desiredAnns:    nil,
			wantAnnotation: "ns/route-a",
		},
		{
			name:           "existing has other routes, appends routeRef",
			existingAnns:   map[string]string{annotationKey: "ns/other-route"},
			desiredAnns:    nil,
			wantAnnotation: "ns/other-route,ns/route-a",
		},
		{
			name:           "routeRef already present, annotation is unchanged",
			existingAnns:   map[string]string{annotationKey: "ns/other-route,ns/route-a"},
			desiredAnns:    nil,
			wantAnnotation: "ns/other-route,ns/route-a",
		},
		{
			name:           "desired already has unrelated annotations, only hybrid-routes key is set",
			existingAnns:   nil,
			desiredAnns:    map[string]string{"unrelated": "keep"},
			wantAnnotation: "ns/route-a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := newUnstructured("ns", "up-1", upstreamGVK, nil)
			if tt.desiredAnns != nil {
				desired.SetAnnotations(tt.desiredAnns)
			}
			existing := newUnstructured("ns", "up-1", upstreamGVK, nil)
			if tt.existingAnns != nil {
				existing.SetAnnotations(tt.existingAnns)
			}
			mergeHybridRouteAnnotation(&desired, &existing, annotationKey, routeRef)
			assert.Equal(t, tt.wantAnnotation, desired.GetAnnotations()[annotationKey])
		})
	}
}

func TestMergeHybridGatewayAnnotation(t *testing.T) {
	gvk := schema.GroupVersionKind{Group: "configuration.konghq.com", Version: "v1", Kind: "KongPlugin"}
	annotationKey := consts.GatewayOperatorHybridGatewaysAnnotation

	tests := []struct {
		name           string
		existing       string
		desired        string
		wantAnnotation string
	}{
		{
			name:           "empty existing keeps desired gateway",
			desired:        "ns/gateway-a",
			wantAnnotation: "ns/gateway-a",
		},
		{
			name:           "existing gateway is preserved",
			existing:       "ns/gateway-a",
			desired:        "ns/gateway-b",
			wantAnnotation: "ns/gateway-a,ns/gateway-b",
		},
		{
			name:           "existing desired gateway is not duplicated",
			existing:       "ns/gateway-a,ns/gateway-b",
			desired:        "ns/gateway-b",
			wantAnnotation: "ns/gateway-a,ns/gateway-b",
		},
		{
			name:           "all desired gateways are merged",
			existing:       "ns/gateway-a",
			desired:        "ns/gateway-b,ns/gateway-c",
			wantAnnotation: "ns/gateway-a,ns/gateway-b,ns/gateway-c",
		},
		{
			name:           "desired object without gateway annotation is unchanged",
			existing:       "ns/gateway-a",
			wantAnnotation: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := newUnstructured("ns", "plugin", gvk, nil)
			if tt.existing != "" {
				existing.SetAnnotations(map[string]string{annotationKey: tt.existing})
			}
			desired := newUnstructured("ns", "plugin", gvk, nil)
			if tt.desired != "" {
				desired.SetAnnotations(map[string]string{annotationKey: tt.desired})
			}

			mergeHybridGatewayAnnotation(&desired, &existing)

			assert.Equal(t, tt.wantAnnotation, desired.GetAnnotations()[annotationKey])
		})
	}
}

func TestRequeueOnConflict(t *testing.T) {
	logger := logr.Discard()
	conflict := apierrors.NewConflict(
		schema.GroupResource{Group: "configuration.konghq.com", Resource: "kongservices"},
		"svc",
		assert.AnError,
	)

	result, handled := requeueOnConflict(fmt.Errorf("wrapped conflict: %w", conflict), logger, "conflict")
	require.True(t, handled)
	assert.Equal(t, ctrlconsts.RequeueWithoutBackoff, result.RequeueAfter)

	result, handled = requeueOnConflict(assert.AnError, logger, "conflict")
	require.False(t, handled)
	assert.Empty(t, result)
}
