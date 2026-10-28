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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

func TestResolve(t *testing.T) {
	key := client.ObjectKey{Namespace: ns, Name: gwadminsynctest.GWAdminName}
	being := func(f *gwadminsynctest.GWAdmin) {
		f.GWAdmin.SetDeletionTimestamp(new(metav1.Now()))
		f.GWAdmin.SetFinalizers([]string{"example.com/hold"})
	}
	tests := []struct {
		name string
		// change changes the GWAdmin, and returns the objects of the cluster.
		change func(*gwadminsynctest.GWAdmin) []client.Object
		mode   Mode
		get    func(client.Object) error
		list   func(client.ObjectList) error
		// wantNotReady, when set, is the NotReadyError expected, but its
		// message when wantMsgContains is set.
		wantNotReady    *NotReadyError
		wantMsgContains string
		wantErr         string
		wantPods        []Pod
		wantTerm        []Pod
	}{
		{
			name:     "ready",
			wantPods: []Pod{{Name: "gw-admin-0", UID: "gw-admin-0-uid", Address: "10.0.0.1:8083", Serving: true}},
			wantTerm: []Pod{{Name: "gw-admin-1", UID: "gw-admin-1-uid", Address: "10.0.0.2:8083", Serving: true}},
		},
		{
			name: "the GWAdmin can't be read",
			get: func(obj client.Object) error {
				if _, ok := obj.(*operatorv1alpha1.GWAdmin); ok {
					return errors.New("unavailable")
				}
				return nil
			},
			wantErr: "failed to get the GWAdmin default/gw-admin: unavailable",
		},
		{
			name:   "the GWAdmin doesn't exist, and has no pod left",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object { return []client.Object{f.Bundle, f.ClientSecret} },
			wantNotReady: &NotReadyError{
				Message: "The GWAdmin default/gw-admin does not exist", Cause: CauseNotFound, NothingToRemove: true,
			},
		},
		{
			// Its operator client certificate went with it: they can't be
			// told anything anymore.
			name: "the GWAdmin doesn't exist, but its pods still run", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object { return []client.Object{f.EndpointSlice} },
			wantNotReady: &NotReadyError{
				Message: "The GWAdmin default/gw-admin does not exist, but its gw-admin pods are still running",
				Cause:   CauseNotFound, NothingToRemove: true,
			},
		},
		{
			name: "the GWAdmin is being deleted: not pushed to",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				being(f)
				return f.Objects()
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin is being deleted", Cause: CauseBeingDeleted},
		},
		{
			name: "a GWAdmin being deleted is still told to stop serving", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				being(f)
				return f.Objects()
			},
			wantPods: []Pod{{Name: "gw-admin-0", UID: "gw-admin-0-uid", Address: "10.0.0.1:8083", Serving: true}},
			wantTerm: []Pod{{Name: "gw-admin-1", UID: "gw-admin-1-uid", Address: "10.0.0.2:8083", Serving: true}},
		},
		{
			name: "a GWAdmin being deleted without its TLS material: its pods go with it", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				being(f)
				return []client.Object{f.GWAdmin, f.Bundle, f.EndpointSlice}
			},
			wantNotReady:    &NotReadyError{Cause: CauseTLSNotReady, NothingToRemove: true},
			wantMsgContains: "The operator client certificate Secret default/gwadmin-",
		},
		{
			name: "a GWAdmin not being deleted without its TLS material is waited for", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				return []client.Object{f.GWAdmin, f.Bundle, f.EndpointSlice}
			},
			wantNotReady:    &NotReadyError{Cause: CauseTLSNotReady},
			wantMsgContains: "The operator client certificate Secret default/gwadmin-",
		},
		{
			name: "the GWAdmin hasn't published its API",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.GWAdmin.Status.API = nil
				f.EndpointSlice.Endpoints = nil
				return f.Objects()
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin hasn't published its API yet", Cause: CauseAPINotPublished, NothingToRemove: true},
		},
		{
			// Its status was lost: the GWAdmin controller publishes it again.
			name: "the GWAdmin hasn't published its API, but its pods run",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.GWAdmin.Status.API = nil
				return f.Objects()
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin hasn't published its API yet", Cause: CauseAPINotPublished},
		},
		{
			name: "the GWAdmin has no pod",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.EndpointSlice.Endpoints = nil
				return f.Objects()
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin has no gw-admin pod", Cause: CauseNoPod, NothingToRemove: true},
		},
		{
			name: "a GWAdmin with no pod has nothing to remove, even without its TLS material", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.EndpointSlice.Endpoints = nil
				return []client.Object{f.GWAdmin, f.EndpointSlice}
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin has no gw-admin pod", Cause: CauseNoPod, NothingToRemove: true},
		},
		{
			// e.g. a rollout of a GWAdmin of one replica, whose new pod isn't
			// published yet.
			name: "only terminating pods are still told to stop serving", mode: ForRemoval,
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.EndpointSlice.Endpoints = []discoveryv1.Endpoint{gwadminsynctest.Endpoint("gw-admin-1", "10.0.0.2", true)}
				return f.Objects()
			},
			wantTerm: []Pod{{Name: "gw-admin-1", UID: "gw-admin-1-uid", Address: "10.0.0.2:8083", Serving: true}},
		},
		{
			name: "only terminating pods can't be pushed to",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.EndpointSlice.Endpoints = []discoveryv1.Endpoint{gwadminsynctest.Endpoint("gw-admin-1", "10.0.0.2", true)}
				return f.Objects()
			},
			wantNotReady: &NotReadyError{Message: "The GWAdmin default/gw-admin has no gw-admin pod", Cause: CauseNoPod, NothingToRemove: true},
		},
		{
			name: "the EndpointSlices can't be listed",
			list: func(list client.ObjectList) error {
				if _, ok := list.(*discoveryv1.EndpointSliceList); ok {
					return errors.New("unavailable")
				}
				return nil
			},
			wantErr: "failed to list the EndpointSlices of the API Service default/gw-admin-api of the GWAdmin",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := gwadminsynctest.NewGWAdmin(t, ns, gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", false), gwadminsynctest.Endpoint("gw-admin-1", "10.0.0.2", true))
			objects := f.Objects()
			if tc.change != nil {
				objects = tc.change(f)
			}
			funcs := interceptor.Funcs{}
			if tc.get != nil {
				funcs.Get = func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if err := tc.get(obj); err != nil {
						return err
					}
					return cl.Get(ctx, key, obj, opts...)
				}
			}
			if tc.list != nil {
				funcs.List = func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if err := tc.list(list); err != nil {
						return err
					}
					return cl.List(ctx, list, opts...)
				}
			}
			s := &Syncer{Client: newFakeClient(t, funcs, objects...), OperatorNamespace: ns}
			target, err := s.Resolve(t.Context(), key, tc.mode)
			switch {
			case tc.wantNotReady != nil:
				notReady, ok := errors.AsType[NotReadyError](err)
				require.True(t, ok, "not ready: %v", err)
				if tc.wantMsgContains != "" {
					assert.Contains(t, notReady.Message, tc.wantMsgContains)
					notReady.Message = ""
				}
				if notReady.Cause == CauseNotFound {
					assert.Nil(t, notReady.GWAdmin, "a GWAdmin that doesn't exist")
				} else if assert.NotNil(t, notReady.GWAdmin, "the GWAdmin, which exists") {
					assert.Equal(t, gwadminsynctest.GWAdminName, notReady.GWAdmin.Name)
				}
				notReady.GWAdmin = nil
				assert.Equal(t, *tc.wantNotReady, notReady)
			case tc.wantErr != "":
				require.ErrorContains(t, err, tc.wantErr)
				_, notReady := errors.AsType[NotReadyError](err)
				assert.False(t, notReady, "an error reading the cluster is retried")
			default:
				require.NoError(t, err)
				assert.Equal(t, key, target.Key())
				assert.Equal(t, tc.wantPods, target.Pods)
				assert.Equal(t, tc.wantTerm, target.Terminating)
				assert.Equal(t, gwadminsynctest.APIServiceName+"."+ns+".svc", target.tlsConfig.ServerName)
				assert.NotEmpty(t, target.TLSFingerprint)
			}
		})
	}
}

func TestResolveInAnotherOperatorNamespace(t *testing.T) {
	f := gwadminsynctest.NewGWAdmin(t, ns, gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", false))
	f.ClientSecret.Namespace = "kong-system"
	s := &Syncer{Client: newFakeClient(t, interceptor.Funcs{}, f.Objects()...), OperatorNamespace: "kong-system"}
	_, err := s.Resolve(t.Context(), client.ObjectKeyFromObject(f.GWAdmin), ForPush)
	require.NoError(t, err, "the operator client certificate is read in the operator namespace")

	s.OperatorNamespace = ns
	_, err = s.Resolve(t.Context(), client.ObjectKeyFromObject(f.GWAdmin), ForPush)
	require.ErrorContains(t, err, "The operator client certificate Secret default/gwadmin-", "not in the namespace of the GWAdmin")
}

func TestRecords(t *testing.T) {
	f := gwadminsynctest.NewGWAdmin(t, ns, gwadminsynctest.Endpoint("gw-admin-0", "10.0.0.1", false), gwadminsynctest.Endpoint("gw-admin-1", "10.0.0.2", false))
	cl := newFakeClient(t, interceptor.Funcs{}, f.Objects()...)
	s := &Syncer{Client: cl, OperatorNamespace: ns}
	key := client.ObjectKeyFromObject(f.GWAdmin)
	target, err := s.Resolve(t.Context(), key, ForPush)
	require.NoError(t, err)
	pod0, pod1 := target.Pods[0], target.Pods[1]

	assert.Empty(t, s.Recorded(target, pod0, "item"), "nothing recorded yet")
	s.Record(target, pod0, "item", "value")
	s.Record(target, pod0, "other", "value")
	s.Record(target, pod1, "item", "value")
	assert.Equal(t, "value", s.Recorded(target, pod0, "item"))

	s.Unrecord(target, pod0, "item")
	assert.Empty(t, s.Recorded(target, pod0, "item"), "an item deleted from a pod")
	assert.Equal(t, "value", s.Recorded(target, pod0, "other"), "only that one")

	// A pod gone, e.g. in a rollout.
	f.EndpointSlice.Endpoints = f.EndpointSlice.Endpoints[:1]
	require.NoError(t, cl.Update(t.Context(), f.EndpointSlice))
	_, err = s.Resolve(t.Context(), key, ForPush)
	require.NoError(t, err)
	assert.Empty(t, s.Recorded(target, pod1, "item"), "what was recorded of a pod gone is forgotten")

	s.Record(target, pod0, "item", "value")
	require.NoError(t, cl.Delete(t.Context(), f.GWAdmin))
	require.NoError(t, cl.Delete(t.Context(), f.EndpointSlice))
	_, err = s.Resolve(t.Context(), key, ForPush)
	require.Error(t, err)
	assert.Empty(t, s.Recorded(target, pod0, "item"), "what was recorded of a GWAdmin gone is forgotten")

	_, err = s.Resolve(t.Context(), key, ForRemoval)
	require.Error(t, err, "gone")
	require.NoError(t, cl.Create(t.Context(), func() client.Object { g := f.GWAdmin.DeepCopy(); g.ResourceVersion = ""; return g }()))
	require.NoError(t, cl.Create(t.Context(), func() client.Object { e := f.EndpointSlice.DeepCopy(); e.ResourceVersion = ""; return e }()))
	target, err = s.Resolve(t.Context(), key, ForPush)
	require.NoError(t, err)
	s.Record(target, pod0, "item", "value")
	assert.True(t, s.forgetDeleted().Delete(event.DeleteEvent{Object: f.GWAdmin}))
	assert.Empty(t, s.Recorded(target, pod0, "item"), "and so is what was recorded of a GWAdmin deleted")
	s.Record(target, pod0, "item", "value")
	assert.Empty(t, s.Recorded(target, pod0, "item"), "a push resolved before is still running: what it records isn't kept")
}

func TestPodKey(t *testing.T) {
	assert.Equal(t, "uid", Pod{Name: "gw-admin-0", UID: "uid", Address: "10.0.0.1:8083"}.Key())
	assert.Equal(t, "gw-admin-0@10.0.0.1:8083", Pod{Name: "gw-admin-0", Address: "10.0.0.1:8083"}.Key(), "without its UID")
}
