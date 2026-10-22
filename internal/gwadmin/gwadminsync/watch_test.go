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

package gwadminsync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// requestOf returns a RequestsFunc enqueuing the GWAdmin itself.
func requestOf(_ context.Context, gwAdmin types.NamespacedName) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: gwAdmin}}
}

var gwAdminRequest = []reconcile.Request{{Namespace: ns, Name: gwadminsynctest.GWAdminName}}

func TestServingChanged(t *testing.T) {
	gw := gwadminsynctest.NewGWAdmin(t, ns).GWAdmin
	update := func(mutate func(*operatorv1alpha1.GWAdmin)) bool {
		updated := gw.DeepCopy()
		mutate(updated)
		return servingChanged().Update(event.UpdateEvent{ObjectOld: gw, ObjectNew: updated})
	}

	assert.False(t, update(func(g *operatorv1alpha1.GWAdmin) {
		g.Generation++
		g.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
	}), "a change a Syncer doesn't read")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.API.Port = 9443 }), "the API it publishes")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.API = nil }), "the API it no longer publishes")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.TLS.APICABundleSecretName = "other" }), "its TLS material")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.CP.ServerURL = "https://other:8083" }), "its cp endpoint")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.SetDeletionTimestamp(new(metav1.Now())) }), "its deletion")
	assert.True(t, servingChanged().Update(event.UpdateEvent{ObjectOld: &corev1.Secret{}, ObjectNew: &corev1.Secret{}}), "not a GWAdmin")
	assert.True(t, servingChanged().Create(event.CreateEvent{Object: gw}))
	assert.True(t, servingChanged().Delete(event.DeleteEvent{Object: gw}))
}

func TestMapGWAdmin(t *testing.T) {
	assert.Equal(t, gwAdminRequest, mapGWAdmin(requestOf)(t.Context(), gwadminsynctest.NewGWAdmin(t, ns).GWAdmin))
}

func TestMapSecret(t *testing.T) {
	gw := gwadminsynctest.NewGWAdmin(t, ns).GWAdmin
	operatorSecret := &corev1.Secret{Name: "operator-client", Namespace: "kong-system", Labels: map[string]string{
		consts.GWAdminUIDLabel:                        string(gw.UID),
		consts.GatewayOperatorManagedByNameLabel:      gwadminsynctest.GWAdminName,
		consts.GatewayOperatorManagedByNamespaceLabel: ns,
	}}
	assert.Equal(t, gwAdminRequest, mapSecret(requestOf)(t.Context(), operatorSecret), "a Secret of the GWAdmin in the operator namespace")
	assert.Equal(t, gwAdminRequest, mapSecret(requestOf)(t.Context(), gwadminsynctest.NewGWAdmin(t, ns).Bundle),
		"a Secret of the GWAdmin in its namespace, e.g. its API CA bundle")
	assert.Empty(t, mapSecret(requestOf)(t.Context(), &corev1.Secret{Name: "tls", Namespace: ns}), "not a Secret of a GWAdmin")
}

func TestMapEndpointSlice(t *testing.T) {
	slice := gwadminsynctest.NewGWAdmin(t, ns).EndpointSlice
	mapFn := mapEndpointSlice(requestOf)

	assert.Equal(t, gwAdminRequest, mapFn(t.Context(), slice), "whether the GWAdmin exists or not")

	cp := slice.DeepCopy()
	cp.Labels[consts.GWAdminServiceRoleLabel] = "cp"
	assert.Empty(t, mapFn(t.Context(), cp), "not the API Service")

	notManaged := slice.DeepCopy()
	delete(notManaged.Labels, discoveryv1.LabelManagedBy)
	assert.Empty(t, mapFn(t.Context(), notManaged), "not managed by the EndpointSlice controller")

	unlabelled := slice.DeepCopy()
	unlabelled.Labels = map[string]string{discoveryv1.LabelServiceName: gwadminsynctest.APIServiceName}
	assert.Empty(t, mapFn(t.Context(), unlabelled), "not a Service of a GWAdmin")

	key, ok := ofEndpointSlice(slice)
	assert.True(t, ok)
	assert.Equal(t, types.NamespacedName{Namespace: ns, Name: gwadminsynctest.GWAdminName}, key)
}
