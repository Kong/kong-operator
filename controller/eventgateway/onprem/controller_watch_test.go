/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const ns = "default"

func onPremGatewayRef(name string) *commonv1alpha1.GatewayRef {
	return &commonv1alpha1.GatewayRef{
		Group: "eventgateway.konghq.com",
		Kind:  "OnPremEventGateway",
		Name:  name,
	}
}

func konnectGatewayRef(name string) *commonv1alpha1.GatewayRef {
	return &commonv1alpha1.GatewayRef{Name: name}
}

func namespacedRef(name string) commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: name},
	}
}

func backendCluster(namespace, name string, gatewayRef *commonv1alpha1.GatewayRef) *configurationv1alpha1.EventGatewayBackendCluster {
	return &configurationv1alpha1.EventGatewayBackendCluster{
		Name:      name,
		Namespace: namespace,
		Spec:      configurationv1alpha1.EventGatewayBackendClusterSpec{ParentRef: gatewayRef},
	}
}

func virtualCluster(name, backendClusterName string) *configurationv1alpha1.EventGatewayVirtualCluster {
	return &configurationv1alpha1.EventGatewayVirtualCluster{
		Name:      name,
		Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: namespacedRef(backendClusterName),
		},
	}
}

func listener(name string, gatewayRef *commonv1alpha1.GatewayRef) *configurationv1alpha1.EventGatewayListener {
	return &configurationv1alpha1.EventGatewayListener{
		Name:      name,
		Namespace: ns,
		Spec:      configurationv1alpha1.EventGatewayListenerSpec{ParentRef: gatewayRef},
	}
}

// TestMapEntityToOnPremEventGateway covers the walk from each Event Gateway
// entity kind up to the OnPremEventGateway it belongs to.
func TestMapEntityToOnPremEventGateway(t *testing.T) {
	onPremBackend := backendCluster(ns, "onprem-backend", onPremGatewayRef("onprem-gw"))
	konnectBackend := backendCluster(ns, "konnect-backend", konnectGatewayRef("konnect-gw"))
	onPremVirtualCluster := virtualCluster("onprem-vc", onPremBackend.Name)
	konnectVirtualCluster := virtualCluster("konnect-vc", konnectBackend.Name)
	onPremListener := listener("onprem-listener", onPremGatewayRef("onprem-gw"))
	konnectListener := listener("konnect-listener", konnectGatewayRef("konnect-gw"))

	// A backend cluster in another namespace, attached to another
	// OnPremEventGateway, with the same name as the Konnect one in this
	// namespace: the walk must follow the namespace of the reference.
	otherNamespaceBackend := backendCluster("other", konnectBackend.Name, onPremGatewayRef("other-gw"))
	crossNamespaceVirtualCluster := virtualCluster("cross-namespace-vc", konnectBackend.Name)
	crossNamespaceVirtualCluster.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new("other")

	crossNamespaceGatewayBackend := backendCluster(ns, "cross-namespace-gateway-backend", onPremGatewayRef("other-gw"))
	crossNamespaceGatewayBackend.Spec.ParentRef.Namespace = new("other")

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(
			onPremBackend, konnectBackend, onPremVirtualCluster, konnectVirtualCluster,
			onPremListener, konnectListener, otherNamespaceBackend,
		).
		Build()

	onPremGateway := types.NamespacedName{Namespace: ns, Name: "onprem-gw"}

	tests := []struct {
		name string
		obj  client.Object
		want *types.NamespacedName
	}{
		{
			name: "backend cluster",
			obj:  onPremBackend,
			want: &onPremGateway,
		},
		{
			name: "data plane certificate",
			obj: &configurationv1alpha1.EventGatewayDataPlaneCertificate{
				Name: "cert", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{ParentRef: onPremGatewayRef("onprem-gw")},
			},
			want: &onPremGateway,
		},
		{
			name: "listener",
			obj:  onPremListener,
			want: &onPremGateway,
		},
		{
			name: "schema registry",
			obj: &configurationv1alpha1.EventGatewaySchemaRegistry{
				Name: "registry", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewaySchemaRegistrySpec{ParentRef: onPremGatewayRef("onprem-gw")},
			},
			want: &onPremGateway,
		},
		{
			name: "static key",
			obj: &configurationv1alpha1.EventGatewayStaticKey{
				Name: "key", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayStaticKeySpec{ParentRef: onPremGatewayRef("onprem-gw")},
			},
			want: &onPremGateway,
		},
		{
			name: "TLS trust bundle",
			obj: &configurationv1alpha1.EventGatewayTLSTrustBundle{
				Name: "bundle", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{ParentRef: onPremGatewayRef("onprem-gw")},
			},
			want: &onPremGateway,
		},
		{
			name: "listener policy",
			obj: &configurationv1alpha1.EventGatewayListenerPolicy{
				Name: "listener-policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{EventGatewayListenerRef: namespacedRef(onPremListener.Name)},
			},
			want: &onPremGateway,
		},
		{
			name: "virtual cluster",
			obj:  onPremVirtualCluster,
			want: &onPremGateway,
		},
		{
			name: "virtual cluster policy",
			obj: &configurationv1alpha1.EventGatewayVirtualClusterPolicy{
				Name: "policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicySpec{EventGatewayVirtualClusterRef: namespacedRef(onPremVirtualCluster.Name)},
			},
			want: &onPremGateway,
		},
		{
			name: "virtual cluster consume policy",
			obj: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
				Name: "consume-policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{EventGatewayVirtualClusterRef: namespacedRef(onPremVirtualCluster.Name)},
			},
			want: &onPremGateway,
		},
		{
			name: "virtual cluster produce policy",
			obj: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
				Name: "produce-policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{EventGatewayVirtualClusterRef: namespacedRef(onPremVirtualCluster.Name)},
			},
			want: &onPremGateway,
		},
		{
			name: "gateway that does not exist (yet)",
			obj:  backendCluster(ns, "backend", onPremGatewayRef("missing-gw")),
			want: &types.NamespacedName{Namespace: ns, Name: "missing-gw"},
		},
		{
			name: "gateway in another namespace",
			obj:  crossNamespaceGatewayBackend,
			want: &types.NamespacedName{Namespace: "other", Name: "other-gw"},
		},
		{
			name: "parent entity in another namespace",
			obj:  crossNamespaceVirtualCluster,
			want: &types.NamespacedName{Namespace: "other", Name: "other-gw"},
		},
		{
			name: "root entity of a KonnectEventGateway",
			obj:  konnectBackend,
		},
		{
			// The deprecated spec.gatewayRef only references a
			// KonnectEventGateway, whatever its name.
			name: "root entity with the deprecated gatewayRef naming an OnPremEventGateway",
			obj: &configurationv1alpha1.EventGatewayBackendCluster{
				Name: "deprecated-gateway-ref", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{GatewayRef: namespacedRef("onprem-gw")},
			},
		},
		{
			name: "virtual cluster of a KonnectEventGateway",
			obj:  konnectVirtualCluster,
		},
		{
			name: "listener policy of a KonnectEventGateway",
			obj: &configurationv1alpha1.EventGatewayListenerPolicy{
				Name: "listener-policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{EventGatewayListenerRef: namespacedRef(konnectListener.Name)},
			},
		},
		{
			name: "parent entity that does not exist (yet)",
			obj:  virtualCluster("orphan-vc", "missing-backend"),
		},
		{
			name: "policy whose virtual cluster has no backend cluster (yet)",
			obj: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
				Name: "produce-policy", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{EventGatewayVirtualClusterRef: namespacedRef("missing-vc")},
			},
		},
		{
			name: "parent referenced by Konnect ID",
			obj: &configurationv1alpha1.EventGatewayVirtualCluster{
				Name: "konnect-id-vc", Namespace: ns,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
					EventGatewayBackendClusterRef: commonv1alpha1.ObjectRef{
						Type:      commonv1alpha1.ObjectRefTypeKonnectID,
						KonnectID: new("5f7a9b1e-0c3d-4e8f-9a2b-6c1d3e5f7a9b"),
					},
				},
			},
		},
	}

	mapFn := mapEntityToOnPremEventGateway(cl)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapFn(t.Context(), tc.obj)
			if tc.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []reconcile.Request{{NamespacedName: *tc.want}}, got)
		})
	}
}

// TestEntityTypes checks that every entity kind is watched once and can name
// its parent, which the walk to the gateway relies on.
func TestEntityTypes(t *testing.T) {
	types := entityTypes()
	assert.Len(t, entityGroupKinds, len(types), "an entity kind is listed twice")
	for _, obj := range types {
		assert.Implements(t, (*entityWithParent)(nil), obj)
	}
}

// TestEntityTypesCoverEveryOnPremCapableKind checks the entities generated by
// crd-from-oas in zz_generated_entities.go against the API types of every
// group: the kinds whose parentRef can reference an OnPremEventGateway and,
// transitively, the kinds whose parent is one of them. A kind missing from
// the list would not reconcile its gateway.
func TestEntityTypesCoverEveryOnPremCapableKind(t *testing.T) {
	s := scheme.Get()
	onPremParentRef := map[string]any{"spec": map[string]any{"parentRef": map[string]any{
		"group": onPremEventGatewayGVK.Group, "kind": onPremEventGatewayGVK.Kind, "name": "gateway",
	}}}

	entities := make(map[schema.GroupKind]entityWithParent)
	want := make(map[schema.GroupKind]struct{})
	for gvk := range s.AllKnownTypes() {
		obj, err := s.New(gvk)
		require.NoError(t, err)
		entity, ok := obj.(entityWithParent)
		if !ok {
			continue
		}
		entities[gvk.GroupKind()] = entity
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(onPremParentRef, entity); err != nil {
			continue
		}
		if entity.GetParentGVK() == onPremEventGatewayGVK {
			want[gvk.GroupKind()] = struct{}{}
		}
	}
	require.NotEmpty(t, want, "no kind can reference an OnPremEventGateway")
	for added := true; added; {
		added = false
		for gk, entity := range entities {
			if _, ok := want[gk]; ok {
				continue
			}
			if _, ok := want[entity.GetParentGVK().GroupKind()]; ok {
				want[gk] = struct{}{}
				added = true
			}
		}
	}

	assert.Equal(t, want, entityGroupKinds)
}

// TestMapEntityToOnPremEventGateway_ParentReadError checks that an entity
// whose parent can't be read reconciles every gateway of the namespaces of
// the walk, instead of dropping the event.
func TestMapEntityToOnPremEventGateway_ParentReadError(t *testing.T) {
	gateway := func(namespace, name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return &eventgatewayv1alpha1.OnPremEventGateway{Name: name, Namespace: namespace}
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(gateway(ns, "gateway-1"), gateway(ns, "gateway-2"), gateway("other", "gateway-3"), gateway("unrelated", "gateway-4")).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewayBackendCluster); ok {
					return errors.New("cache not started")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	vc := virtualCluster("vc", "backend")
	vc.Spec.EventGatewayBackendClusterRef.NamespacedRef.Namespace = new("other")

	got := mapEntityToOnPremEventGateway(cl)(t.Context(), vc)
	assert.ElementsMatch(t, []reconcile.Request{
		{Namespace: ns, Name: "gateway-1"},
		{Namespace: ns, Name: "gateway-2"},
		{Namespace: "other", Name: "gateway-3"},
	}, got)
}

func TestMapKongReferenceGrantToOnPremEventGatewaysForSecrets(t *testing.T) {
	grant := func(fromKind string, toKind configurationv1alpha1.Kind) *configurationv1alpha1.KongReferenceGrant {
		return &configurationv1alpha1.KongReferenceGrant{
			Name: "grant", Namespace: "secrets",
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{{Group: "configuration.konghq.com", Kind: configurationv1alpha1.Kind(fromKind), Namespace: ns}},
				To:   []configurationv1alpha1.ReferenceGrantTo{{Group: "core", Kind: toKind}},
			},
		}
	}
	gateway := newTestGateway("gateway")
	other := newTestGateway("other")
	other.Namespace = "other"
	mapFn := mapKongReferenceGrantToOnPremEventGateways(newFakeClient(t, gateway, other))

	assert.Equal(t,
		[]reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(gateway)}},
		mapFn(t.Context(), grant("EventGatewayBackendCluster", "Secret")),
		"the gateways of the namespace whose entities may read the Secrets")
	assert.Empty(t, mapFn(t.Context(), grant("EventGatewayBackendCluster", "ConfigMap")), "not a grant to Secrets")
	assert.Empty(t, mapFn(t.Context(), grant("KongService", "Secret")), "not a grant to entities of an OnPremEventGateway")
}

func TestMapSecretToOnPremEventGateways(t *testing.T) {
	readingSecret := func(namespace, name string, gatewayRef *commonv1alpha1.GatewayRef, secretNamespace *string) *configurationv1alpha1.EventGatewayBackendCluster {
		bc := backendCluster(namespace, name, gatewayRef)
		bc.Spec.APISpec.TLS.ClientIdentity.Certificate = configurationv1alpha1.SensitiveDataSource{
			Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
			SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: "tls", Namespace: secretNamespace, Key: "tls.crt"},
		}
		return bc
	}
	cl := newFakeClient(t,
		readingSecret(ns, "a", onPremGatewayRef("gateway"), nil),
		readingSecret(ns, "b", onPremGatewayRef("gateway"), new(ns)),
		readingSecret(ns, "other-gateway", onPremGatewayRef("other"), new("secrets")),
		readingSecret(ns, "konnect", konnectGatewayRef("konnect"), nil),
		backendCluster(ns, "no-secret", onPremGatewayRef("unrelated")),
	)
	mapFn := mapSecretToOnPremEventGateways(cl)
	secret := func(namespace string) *corev1.Secret { return &corev1.Secret{Name: "tls", Namespace: namespace} }

	assert.Equal(t,
		[]reconcile.Request{{Namespace: ns, Name: "gateway"}},
		mapFn(t.Context(), secret(ns)),
		"once per gateway, not for a backend cluster of a KonnectEventGateway")
	assert.Equal(t,
		[]reconcile.Request{{Namespace: ns, Name: "other"}},
		mapFn(t.Context(), secret("secrets")),
		"a Secret of another namespace")
	assert.Empty(t, mapFn(t.Context(), secret("unread")))
}
