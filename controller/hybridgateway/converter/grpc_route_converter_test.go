package converter

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestGRPCRouteConverter_GetOutputStore(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	validUpstream := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "upstream-1",
			Namespace: "default",
		},
	}
	validService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service-1",
			Namespace: "default",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	converter := newGRPCRouteConverter(&gwtypes.GRPCRoute{}, fakeClient, false, "").(*grpcRouteConverter)
	converter.outputStore = []client.Object{validUpstream, validService}

	objects, err := converter.GetOutputStore(ctx, logger)
	require.NoError(t, err)
	require.Len(t, objects, 2, "GetOutputStore must return exactly len(outputStore) objects, no leading zero-value entries")
	assert.Equal(t, "upstream-1", objects[0].GetName())
	assert.Equal(t, "service-1", objects[1].GetName())
	for _, obj := range objects {
		assert.NotEmpty(t, obj.GetName(), "no zero-value/empty entries expected")
	}
}

func TestGRPCRouteConverter_DesiredResourcesReady(t *testing.T) {
	ctx := t.Context()
	const (
		ns           = "default"
		routeName    = "route-1"
		svcName      = "svc-1"
		konnectSvcID = "konnect-svc-id-abc"
	)

	routeGVK := configurationv1alpha1.GroupVersion.WithKind("KongRoute")

	desiredRoute := func(name string) *configurationv1alpha1.KongRoute {
		r := &configurationv1alpha1.KongRoute{}
		r.Name = name
		r.Namespace = ns
		r.SetGroupVersionKind(routeGVK)
		return r
	}

	clusterRoute := func(name, serviceRefName, boundServiceID string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(routeGVK)
		u.SetName(name)
		u.SetNamespace(ns)
		if serviceRefName != "" {
			_ = unstructured.SetNestedField(u.Object, serviceRefName, "spec", "serviceRef", "namespacedRef", "name")
		}
		if boundServiceID != "" {
			_ = unstructured.SetNestedField(u.Object, boundServiceID, "status", "konnect", "serviceID")
		}
		return u
	}

	kongService := func(name string, programmed bool, konnectID string) *configurationv1alpha1.KongService {
		svc := &configurationv1alpha1.KongService{}
		svc.Name = name
		svc.Namespace = ns
		if programmed {
			svc.Status.Conditions = []metav1.Condition{{
				Type:               "Programmed",
				Status:             metav1.ConditionTrue,
				Reason:             "Programmed",
				LastTransitionTime: metav1.Now(),
			}}
		}
		if konnectID != "" {
			svc.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: konnectID},
			}
		}
		return svc
	}

	baseRoute := &gwtypes.GRPCRoute{ObjectMeta: metav1.ObjectMeta{Name: "test-route", Namespace: ns}}

	tests := []struct {
		name            string
		outputStore     []client.Object
		existingObjs    []client.Object
		interceptorFn   *interceptor.Funcs
		wantReady       bool
		wantErrContains string
	}{
		{
			name:            "GetOutputStore error is propagated",
			outputStore:     []client.Object{&badObject{Name: "bad"}},
			wantReady:       false,
			wantErrContains: "failed to get desired objects for readiness check",
		},
		{
			name:        "empty output store → ready",
			outputStore: nil,
			wantReady:   true,
		},
		{
			name:        "desired KongRoute not yet in cluster → defer (NotFound)",
			outputStore: []client.Object{desiredRoute(routeName)},
			wantReady:   false,
		},
		{
			name:        "Get KongRoute returns non-NotFound error → propagate",
			outputStore: []client.Object{desiredRoute(routeName)},
			interceptorFn: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*unstructured.Unstructured); ok && key.Name == routeName {
						return fmt.Errorf("simulated get error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			wantReady:       false,
			wantErrContains: "failed to get KongRoute",
		},
		{
			name:         "KongRoute found, no serviceRef (serviceless route) → ready",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, "", "")},
			wantReady:    true,
		},
		{
			name:         "KongRoute found with serviceRef, KongService not found → defer",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, svcName, "")},
			wantReady:    false,
		},
		{
			name:         "KongService found but not Programmed → defer",
			outputStore:  []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{clusterRoute(routeName, svcName, ""), kongService(svcName, false, "")},
			wantReady:    false,
		},
		{
			name:        "KongService Programmed, Konnect ID set, but route bound to old service ID → defer",
			outputStore: []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{
				clusterRoute(routeName, svcName, "old-service-id"),
				kongService(svcName, true, konnectSvcID),
			},
			wantReady: false,
		},
		{
			name:        "KongRoute bound to correct service ID → ready",
			outputStore: []client.Object{desiredRoute(routeName)},
			existingObjs: []client.Object{
				clusterRoute(routeName, svcName, konnectSvcID),
				kongService(svcName, true, konnectSvcID),
			},
			wantReady: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme.Get())
			if len(tt.existingObjs) > 0 {
				builder = builder.WithObjects(tt.existingObjs...)
			}
			if tt.interceptorFn != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptorFn)
			}
			cl := builder.Build()

			conv := newGRPCRouteConverter(baseRoute, cl, false, "").(*grpcRouteConverter)
			conv.outputStore = tt.outputStore

			ready, err := conv.DesiredResourcesReady(ctx, logr.Discard())

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
