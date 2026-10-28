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
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestTLSConfig(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*gwadminsynctest.GWAdmin) []client.Object
		get     func(obj client.Object) error
		wantErr string
		// notReady tells the error is a NotReadyError.
		notReady bool
	}{
		{name: "valid"},
		{
			name: "the API CA bundle doesn't exist",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				return []client.Object{f.GWAdmin, f.ClientSecret}
			},
			wantErr: `The API CA bundle Secret default/gw-admin-api-ca-bundle of the GWAdmin does not exist (it must exist and carry the "x" label`, notReady: true,
		},
		{
			name: "the API CA bundle can't be read",
			get: func(obj client.Object) error {
				if obj.GetName() == gwadminsynctest.APIBundleSecret {
					return errors.New("unavailable")
				}
				return nil
			},
			wantErr: "failed to get the API CA bundle Secret default/gw-admin-api-ca-bundle of the GWAdmin: unavailable",
		},
		{
			name: "the API CA bundle holds no certificate",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.Bundle.Data = map[string][]byte{consts.CACRT: []byte("garbage")}
				return f.Objects()
			},
			wantErr: "holds no certificate in ca.crt", notReady: true,
		},
		{
			name: "the operator client certificate doesn't exist",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				return []client.Object{f.GWAdmin, f.Bundle}
			},
			wantErr: `of the GWAdmin does not exist (it must exist and carry the "x" label`, notReady: true,
		},
		{
			name: "the operator client certificate can't be read",
			get: func(obj client.Object) error {
				if _, ok := obj.(*corev1.Secret); ok && obj.GetName() != gwadminsynctest.APIBundleSecret {
					return errors.New("unavailable")
				}
				return nil
			},
			wantErr: "failed to get the operator client certificate Secret",
		},
		{
			name: "the operator client certificate belongs to another GWAdmin",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.ClientSecret.Labels[consts.GWAdminUIDLabel] = "another"
				return f.Objects()
			},
			wantErr: "doesn't belong to the GWAdmin", notReady: true,
		},
		{
			name: "the operator client certificate is invalid",
			change: func(f *gwadminsynctest.GWAdmin) []client.Object {
				f.ClientSecret.Data[corev1.TLSPrivateKeyKey] = []byte("garbage")
				return f.Objects()
			},
			wantErr: "of the GWAdmin is invalid", notReady: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := gwadminsynctest.NewGWAdmin(t, ns)
			objects := f.Objects()
			if tc.change != nil {
				objects = tc.change(f)
			}
			funcs := interceptor.Funcs{}
			if tc.get != nil {
				funcs.Get = func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					obj.SetName(key.Name)
					if err := tc.get(obj); err != nil {
						return err
					}
					return cl.Get(ctx, key, obj, opts...)
				}
			}
			s := &Syncer{Client: newFakeClient(t, funcs, objects...), OperatorNamespace: ns, SecretLabelSelector: "x"}
			tlsConfig, fingerprint, err := s.tlsConfig(t.Context(), f.GWAdmin)
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Len(t, tlsConfig.Certificates, 1)
				assert.NotEmpty(t, fingerprint)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			notReady, ok := errors.AsType[NotReadyError](err)
			assert.Equal(t, tc.notReady, ok, "%v", err)
			if ok {
				assert.Equal(t, CauseTLSNotReady, notReady.Cause)
			}
		})
	}
}

func TestTLSConfigFingerprint(t *testing.T) {
	f := gwadminsynctest.NewGWAdmin(t, ns)
	cl := newFakeClient(t, interceptor.Funcs{}, f.Objects()...)
	s := &Syncer{Client: cl, OperatorNamespace: ns}
	_, before, err := s.tlsConfig(t.Context(), f.GWAdmin)
	require.NoError(t, err)
	_, same, err := s.tlsConfig(t.Context(), f.GWAdmin)
	require.NoError(t, err)
	assert.Equal(t, before, same, "the same TLS material")

	for _, secret := range []*corev1.Secret{f.Bundle, f.ClientSecret} {
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(secret), secret))
		secret.Labels["unrelated"] = "x"
		secret.Annotations = map[string]string{"unrelated": "x"}
		secret.Data["unrelated"] = []byte("x")
		require.NoError(t, cl.Update(t.Context(), secret))
		_, after, err := s.tlsConfig(t.Context(), f.GWAdmin)
		require.NoError(t, err)
		assert.Equal(t, before, after, "only what isn't read of the Secret %s changed", secret.Name)
	}

	ca := gwadminsynctest.NewCA(t, "other")
	renewedCert, renewedKey := ca.Issue("kong-operator")
	for _, change := range []struct {
		secret *corev1.Secret
		data   map[string][]byte
	}{
		{f.Bundle, map[string][]byte{consts.CACRT: ca.PEM}},
		// A key pair is renewed as a whole.
		{f.ClientSecret, map[string][]byte{corev1.TLSCertKey: renewedCert, corev1.TLSPrivateKeyKey: renewedKey}},
	} {
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(change.secret), change.secret))
		maps.Copy(change.secret.Data, change.data)
		require.NoError(t, cl.Update(t.Context(), change.secret))
		_, after, err := s.tlsConfig(t.Context(), f.GWAdmin)
		require.NoError(t, err)
		assert.NotEqual(t, before, after, "the TLS material of the Secret %s changed", change.secret.Name)
		before = after
	}

	assert.NotEqual(t, tlsFingerprint(f.GWAdmin, "ab", []byte("c")), tlsFingerprint(f.GWAdmin, "a", []byte("bc")),
		"the parts are told apart")
}

func TestGetSecret(t *testing.T) {
	gw := &operatorv1alpha1.GWAdmin{Namespace: "admins", Name: "admin"}
	gw.SetUID("uid")
	owned := &corev1.Secret{Namespace: "kong-system", Name: "owned", Labels: map[string]string{consts.GWAdminUIDLabel: "uid"}}
	other := &corev1.Secret{Namespace: "kong-system", Name: "other", Labels: map[string]string{consts.GWAdminUIDLabel: "another"}}
	s := &Syncer{Client: newFakeClient(t, interceptor.Funcs{}, owned, other)}

	got, err := s.getSecret(t.Context(), client.ObjectKeyFromObject(owned), "operator client certificate", gw)
	require.NoError(t, err)
	assert.Equal(t, "owned", got.Name)

	_, err = s.getSecret(t.Context(), client.ObjectKeyFromObject(other), "API CA bundle", nil)
	require.NoError(t, err, "without ownedBy, the label isn't checked")

	_, err = s.getSecret(t.Context(), client.ObjectKeyFromObject(other), "operator client certificate", gw)
	notReady, ok := errors.AsType[NotReadyError](err)
	require.True(t, ok)
	assert.Equal(t, NotReadyError{
		Message: "The operator client certificate Secret kong-system/other doesn't belong to the GWAdmin admins/admin", Cause: CauseTLSNotReady,
	}, notReady)

	_, err = s.getSecret(t.Context(), client.ObjectKey{Namespace: "kong-system", Name: "missing"}, "API CA bundle", nil)
	require.EqualError(t, err, "The API CA bundle Secret kong-system/missing of the GWAdmin does not exist")
	s.SecretLabelSelector = "x"
	_, err = s.getSecret(t.Context(), client.ObjectKey{Namespace: "kong-system", Name: "missing"}, "API CA bundle", nil)
	require.EqualError(t, err, `The API CA bundle Secret kong-system/missing of the GWAdmin does not exist (it must exist and carry the "x" label to be visible to the operator)`)

	s.Client = newFakeClient(t, interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
		return errors.New("unavailable")
	}})
	_, err = s.getSecret(t.Context(), client.ObjectKeyFromObject(owned), "API CA bundle", nil)
	require.EqualError(t, err, "failed to get the API CA bundle Secret kong-system/owned of the GWAdmin: unavailable")
	_, ok = errors.AsType[NotReadyError](err)
	assert.False(t, ok, "an error reading the cluster is retried")
}
