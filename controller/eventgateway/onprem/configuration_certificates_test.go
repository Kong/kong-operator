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
	"encoding/pem"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

func TestBuildConfigurationCertificates(t *testing.T) {
	gateway := newTestGateway("gateway")
	created := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	certPEM, keyPEM := gwadminsynctest.NewCA(t, "dp-ca").Issue("dp")
	secret := &corev1.Secret{
		Name: "dp-tls", Namespace: ns,
		Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM, "garbage": []byte("not a certificate")},
	}
	// newCertificate returns a certificate of the gateway reading the key of
	// secret.
	newCertificate := func(name, key string) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		return fromYAML[configurationv1alpha1.EventGatewayDataPlaneCertificate](t, created, `
metadata: {name: `+name+`}
spec:
  parentRef: {group: eventgateway.konghq.com, kind: OnPremEventGateway, name: gateway}
  apiSpec:
    certificate: {type: secretRef, secretRef: {name: dp-tls, key: `+key+`}}
`)
	}

	valid := newCertificate("valid", "tls.crt")
	invalid := newCertificate("invalid", "garbage")
	withKey := newCertificate("with-key", "tls.key")
	// Its Secret is gone: left out, rather than kept as it was, as other
	// entities are, so that gw-admin stops trusting it.
	gone := newCertificate("gone", "missing")
	// Being deleted: neither pushed nor reported, gw-admin stops trusting it.
	deleting := newCertificate("deleting", "tls.crt")
	deleting.Finalizers = []string{"example.com/hold"}
	deleting.DeletionTimestamp = new(metav1.NewTime(created))
	// Attached to another gateway.
	elsewhere := newCertificate("elsewhere", "tls.crt")
	elsewhere.Spec.ParentRef.Name = "other"

	cl := newFakeClient(t, gateway, secret, valid, invalid, withKey, gone, deleting, elsewhere)
	previous := map[types.UID]any{gone.UID: certificateEntity{}}
	cfg, err := buildConfiguration(t.Context(), cl, gateway, "", previous)
	require.NoError(t, err)

	require.Len(t, cfg.certificates, 1)
	assert.Equal(t, client.ObjectKeyFromObject(valid), cfg.certificates[0].key)
	assert.Equal(t, string(certPEM), cfg.certificates[0].body.Certificate)
	assert.Equal(t, string(valid.UID), cfg.certificates[0].id.String())
	assert.Equal(t, created, cfg.certificates[0].body.CreatedAt)
	assert.NotContains(t, cfg.built, valid.UID, "a certificate is never kept as it was")

	excluded := map[client.ObjectKey]excludedEntity{}
	for _, e := range cfg.excluded {
		excluded[client.ObjectKeyFromObject(e.obj)] = e
	}
	assert.ElementsMatch(t, []client.ObjectKey{client.ObjectKeyFromObject(invalid), client.ObjectKeyFromObject(withKey), client.ObjectKeyFromObject(gone)}, slices.Collect(maps.Keys(excluded)))
	for _, e := range excluded {
		assert.Equal(t, eventgatewayv1alpha1.OnPremEntityInvalidReason, e.reason, e.obj.GetName())
	}
	assert.Contains(t, excluded[client.ObjectKeyFromObject(withKey)].message, `PRIVATE KEY"`, "a private key isn't sent")
}

func TestValidateCertificates(t *testing.T) {
	ca := gwadminsynctest.NewCA(t, "dp-ca")
	first, key := ca.Issue("first")
	second, _ := ca.Issue("second")
	for name, tc := range map[string]struct {
		data string
		err  string
	}{
		"a certificate":                      {data: string(first)},
		"a chain":                            {data: string(first) + string(second)},
		"empty":                              {err: "not a PEM certificate"},
		"not PEM":                            {data: "certificate", err: "not a PEM certificate"},
		"with a private key":                 {data: string(first) + string(key), err: `PRIVATE KEY"`},
		"a certificate block that isn't one": {data: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("junk")})), err: "the certificate is invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateCertificates(tc.data)
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}
