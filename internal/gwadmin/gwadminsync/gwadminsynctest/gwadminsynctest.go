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

// Package gwadminsynctest provides a GWAdmin ready to be reached by a
// gwadminsync.Syncer, as the GWAdmin controller provisions it, for the tests
// of the controllers that push to gw-admin.
package gwadminsynctest

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// The names of the GWAdmin of the tests, and the port of its API listener.
const (
	GWAdminName     = "gw-admin"
	APIServiceName  = "gw-admin-api"
	APIBundleSecret = "gw-admin-api-ca-bundle" //nolint:gosec // the name of a Secret
	APIPort         = consts.GWAdminAPIPort
)

// ServerURL returns the cp endpoint of the GWAdmin of the tests in the
// namespace, as its controller publishes it.
func ServerURL(namespace string) string {
	return fmt.Sprintf("https://%s-cp.%s.svc:%d", GWAdminName, namespace, consts.GWAdminCPPort)
}

// GWAdmin is a GWAdmin whose API is published, with the objects reaching its
// pods needs: its API CA bundle, the operator client certificate, and the
// EndpointSlice of its API Service. The operator namespace is the one of the
// GWAdmin.
type GWAdmin struct {
	GWAdmin       *operatorv1alpha1.GWAdmin
	Bundle        *corev1.Secret
	ClientSecret  *corev1.Secret
	EndpointSlice *discoveryv1.EndpointSlice
}

// NewGWAdmin returns a GWAdmin of the namespace whose API Service publishes
// the endpoints.
func NewGWAdmin(t *testing.T, namespace string, endpoints ...discoveryv1.Endpoint) *GWAdmin {
	t.Helper()
	gw := &operatorv1alpha1.GWAdmin{Name: GWAdminName, Namespace: namespace}
	gw.SetUID(types.UID(uuid.NewString()))
	gw.Status.API = &operatorv1alpha1.GWAdminAPIStatus{ServiceName: APIServiceName, PortName: consts.GWAdminAPIPortName, Port: APIPort}
	gw.Status.CP = &operatorv1alpha1.GWAdminCPStatus{ServerURL: ServerURL(namespace)}
	gw.Status.TLS = &operatorv1alpha1.GWAdminTLSStatus{APICABundleSecretName: APIBundleSecret}
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("kong-operator"))
	return &GWAdmin{
		GWAdmin: gw,
		Bundle: &corev1.Secret{
			Name: APIBundleSecret, Namespace: namespace,
			Labels: SecretLabels(gw, false),
			Data:   map[string][]byte{consts.CACRT: certPEM},
		},
		ClientSecret: &corev1.Secret{
			Name: gwadminref.OperatorClientTLSSecretName(gw), Namespace: namespace,
			Labels: SecretLabels(gw, true),
			Data:   map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
		},
		EndpointSlice: &discoveryv1.EndpointSlice{
			Name: APIServiceName + "-abcde", Namespace: namespace,
			Labels:      EndpointSliceLabels(APIServiceName),
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports:       []discoveryv1.EndpointPort{{Name: new(consts.GWAdminAPIPortName), Port: new(int32(APIPort))}},
			Endpoints:   endpoints,
		},
	}
}

// Objects returns the objects of the GWAdmin.
func (g *GWAdmin) Objects() []client.Object {
	return []client.Object{g.GWAdmin, g.Bundle, g.ClientSecret, g.EndpointSlice}
}

// SecretLabels returns the labels the GWAdmin controller sets on a Secret of
// the GWAdmin: one it keeps in the operator namespace names the namespace of
// the GWAdmin too.
func SecretLabels(gw *operatorv1alpha1.GWAdmin, operatorNamespace bool) map[string]string {
	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:     consts.GWAdminManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel: gw.Name,
		consts.GWAdminUIDLabel:                   string(gw.UID),
	}
	if operatorNamespace {
		labels[consts.GatewayOperatorManagedByNamespaceLabel] = gw.Namespace
	}
	return labels
}

// EndpointSliceLabels returns the labels of the EndpointSlices the
// EndpointSlice controller manages for the Service serviceName of the
// GWAdmin of the tests, as it copies them from the Service.
func EndpointSliceLabels(serviceName string) map[string]string {
	labels := gwadminref.APIServiceLabels(GWAdminName)
	labels[discoveryv1.LabelManagedBy] = "endpointslice-controller.k8s.io"
	labels[discoveryv1.LabelServiceName] = serviceName
	return labels
}

// Endpoint returns the endpoint of a gw-admin pod serving, whose UID is its
// name with a "-uid" suffix, as the EndpointSlice controller publishes it for
// the API Service: Ready, as the Service publishes the pods not Ready, a pod
// terminating included. NotServing makes it one not serving.
func Endpoint(name, ip string, terminating bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses: []string{ip},
		TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: name, UID: types.UID(name + "-uid")},
		Conditions: discoveryv1.EndpointConditions{
			Ready: new(true), Serving: new(true), Terminating: new(terminating),
		},
	}
}

// NotServing returns the endpoint of a pod not serving, e.g. starting.
func NotServing(endpoint discoveryv1.Endpoint) discoveryv1.Endpoint {
	endpoint.Conditions.Serving = new(false)
	return endpoint
}

// CA is a certificate authority of a test.
type CA struct {
	// Cert is its certificate.
	Cert *x509.Certificate
	// PEM is its certificate, in PEM.
	PEM []byte

	tls tls.Certificate
}

// NewCA returns a certificate authority.
func NewCA(t *testing.T, commonName string) CA {
	t.Helper()
	ca := certificate.MustGenerateCert(certificate.WithCATrue(), certificate.WithCommonName(commonName))
	cert, err := x509.ParseCertificate(ca.Certificate[0])
	require.NoError(t, err)
	certPEM, _ := certificate.CertToPEMFormat(ca)
	return CA{Cert: cert, PEM: certPEM, tls: ca}
}

// Issue returns a certificate issued by the CA, with its key, in PEM.
func (ca CA) Issue(commonName string, dnsNames ...string) (certPEM, keyPEM []byte) {
	return certificate.MustGenerateCertPEMFormat(
		certificate.WithParent(ca.tls), certificate.WithCommonName(commonName), certificate.WithDNSNames(dnsNames...))
}
