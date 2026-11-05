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

package dataplane

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// newWatchClient returns a client indexing the OnPremEventGateways by their
// GWAdmin, and the KegDataPlanes by their control plane.
func newWatchClient(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) client.Client {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...).WithInterceptorFuncs(funcs)
	for _, o := range append(gwadminref.IndexOptions(), index.OptionsForKegDataPlane()...) {
		b = b.WithIndex(o.Object, o.Field, o.ExtractValueFn)
	}
	return b.Build()
}

// watchFixture returns the gateways of a GWAdmin in namespace gw-admins, one
// in its namespace and one in another, a gateway of another GWAdmin, and a
// data plane of each.
func watchFixture() []client.Object {
	gateway := func(namespace, name, gwAdmin string) *eventgatewayv1alpha1.OnPremEventGateway {
		return &eventgatewayv1alpha1.OnPremEventGateway{
			Namespace: namespace, Name: name,
			Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{GWAdminRef: eventgatewayv1alpha1.GWAdminRef{
				Name: gwAdmin, Namespace: new("gw-admins"),
			}},
		}
	}
	dataPlane := func(namespace, name, gateway string) *eventgatewayv1alpha1.KegDataPlane {
		egdp := onPremDataPlane()
		egdp.Namespace, egdp.Name = namespace, name
		egdp.Spec.ControlPlaneRef.OnPremNamespacedRef.Name = gateway
		return egdp
	}
	return []client.Object{
		gateway("gw-admins", "local", "gw-admin"),
		gateway("apps", "remote", "gw-admin"),
		gateway("apps", "other", "other-gw-admin"),
		dataPlane("gw-admins", "local-keg", "local"),
		dataPlane("apps", "remote-keg", "remote"),
		dataPlane("apps", "remote-keg-2", "remote"),
		dataPlane("apps", "other-keg", "other"),
		&operatorv1alpha1.GWAdmin{Namespace: "gw-admins", Name: "gw-admin"},
	}
}

// requestsOf returns the requests of the namespace/name keys.
func requestsOf(keys ...string) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(keys))
	for _, k := range keys {
		ns, name, _ := strings.Cut(k, "/")
		requests = append(requests, reconcile.Request{Namespace: ns, Name: name})
	}
	return requests
}

func TestKegDataPlanesOfGWAdmin(t *testing.T) {
	gwAdmin := types.NamespacedName{Namespace: "gw-admins", Name: "gw-admin"}
	want := requestsOf("gw-admins/local-keg", "apps/remote-keg", "apps/remote-keg-2")

	t.Run("the data planes of the gateways of the GWAdmin, in any namespace", func(t *testing.T) {
		cl := newWatchClient(t, interceptor.Funcs{}, watchFixture()...)
		require.ElementsMatch(t, want, kegDataPlanesOfGWAdmin(t.Context(), cl, gwAdmin))
		require.Empty(t, kegDataPlanesOfGWAdmin(t.Context(), cl, types.NamespacedName{Namespace: "gw-admins", Name: "unused"}))
	})

	t.Run("nothing when the gateways can't be listed", func(t *testing.T) {
		cl := newWatchClient(t, interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("list failed")
			},
		}, watchFixture()...)
		require.Empty(t, kegDataPlanesOfGWAdmin(t.Context(), cl, gwAdmin))
	})

	t.Run("nothing when the data planes can't be listed", func(t *testing.T) {
		cl := newWatchClient(t, interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*eventgatewayv1alpha1.KegDataPlaneList); ok {
					return errors.New("list failed")
				}
				return c.List(ctx, list, opts...)
			},
		}, watchFixture()...)
		require.Empty(t, kegDataPlanesOfGWAdmin(t.Context(), cl, gwAdmin))
	})

	t.Run("a Secret of the GWAdmin", func(t *testing.T) {
		cl := newWatchClient(t, interceptor.Funcs{}, watchFixture()...)
		published := &corev1.Secret{Namespace: testOperatorNamespace, Name: "cp-client-ca", Labels: map[string]string{
			consts.GWAdminUIDLabel:                        "uid",
			consts.GatewayOperatorManagedByNameLabel:      "gw-admin",
			consts.GatewayOperatorManagedByNamespaceLabel: "gw-admins",
		}}
		require.ElementsMatch(t, want, kegDataPlanesOfGWAdminSecret(t.Context(), cl, published))
		require.Empty(t, kegDataPlanesOfGWAdminSecret(t.Context(), cl, &corev1.Secret{Namespace: "gw-admins", Name: "unrelated"}))
	})

	t.Run("a KongReferenceGrant of the namespace of the GWAdmin", func(t *testing.T) {
		cl := newWatchClient(t, interceptor.Funcs{}, watchFixture()...)
		grant := func(namespace, toGroup, toKind string) *configurationv1alpha1.KongReferenceGrant {
			return &configurationv1alpha1.KongReferenceGrant{
				Namespace: namespace, Name: "grant",
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{{
						Group: eventgatewayv1alpha1.GroupName, Kind: eventgatewayv1alpha1.OnPremEventGatewayKind, Namespace: "apps",
					}},
					To: []configurationv1alpha1.ReferenceGrantTo{{
						Group: configurationv1alpha1.Group(toGroup), Kind: configurationv1alpha1.Kind(toKind),
					}},
				},
			}
		}
		require.ElementsMatch(t, want, kegDataPlanesOfGrant(t.Context(), cl, grant("gw-admins", operatorv1alpha1.GroupVersion.Group, "GWAdmin")))
		require.Empty(t, kegDataPlanesOfGrant(t.Context(), cl, grant("apps", operatorv1alpha1.GroupVersion.Group, "GWAdmin")), "no GWAdmin in that namespace")
		require.Empty(t, kegDataPlanesOfGrant(t.Context(), cl, grant("gw-admins", "core", "Secret")), "a grant that doesn't concern GWAdmins")
		require.Empty(t, kegDataPlanesOfGrant(t.Context(), cl, &configurationv1alpha1.KongReferenceGrant{Namespace: "gw-admins", Name: "grant"}), "a grant for nothing")
		require.Empty(t, kegDataPlanesOfGrant(t.Context(), cl, &corev1.Secret{Namespace: "gw-admins", Name: "not-a-grant"}))
	})
}

func TestExtraWatches(t *testing.T) {
	mgr, err := ctrl.NewManager(&rest.Config{Host: "https://127.0.0.1:1"}, ctrl.Options{
		Scheme:  managerscheme.Get(),
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	require.NoError(t, err)
	for _, onPrem := range []bool{false, true} {
		require.NotNil(t, extraWatches(ctrl.NewControllerManagedBy(mgr), mgr, onPrem))
	}
}
