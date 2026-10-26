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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// tlsConfig returns the TLS configuration of the connections to the API
// listener of the GWAdmin: Kong Operator verifies it with the API CA bundle,
// under the name of the API Service, and authenticates with the operator
// client certificate. Both are read on every resolve, so that renewals and
// rotations are picked up: the fingerprint it returns changes with them.
func (s *Syncer) tlsConfig(ctx context.Context, gw *operatorv1alpha1.GWAdmin) (*tls.Config, string, error) {
	bundleKey := client.ObjectKey{Namespace: gw.Namespace, Name: gw.Status.TLS.APICABundleSecretName}
	bundle, err := s.getSecret(ctx, bundleKey, "API CA bundle", nil)
	if err != nil {
		return nil, "", err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle.Data[consts.CACRT]) {
		return nil, "", NotReadyError{
			Message: fmt.Sprintf("The API CA bundle Secret %s of the GWAdmin holds no certificate in %s", bundleKey, consts.CACRT),
			Cause:   CauseTLSNotReady,
		}
	}

	clientKey := client.ObjectKey{Namespace: s.OperatorNamespace, Name: gwadminref.OperatorClientTLSSecretName(gw)}
	clientSecret, err := s.getSecret(ctx, clientKey, "operator client certificate", gw)
	if err != nil {
		return nil, "", err
	}
	cert, err := tls.X509KeyPair(clientSecret.Data[corev1.TLSCertKey], clientSecret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return nil, "", NotReadyError{
			Message: fmt.Sprintf("The operator client certificate Secret %s of the GWAdmin is invalid: %v", clientKey, err),
			Cause:   CauseTLSNotReady,
		}
	}

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
		// A name the server certificate of the API listener is issued for.
		ServerName: gw.Status.API.ServiceName + "." + gw.Namespace + ".svc",
	}
	return cfg, tlsFingerprint(gw, cfg.ServerName, bundle.Data[consts.CACRT],
		clientSecret.Data[corev1.TLSCertKey], clientSecret.Data[corev1.TLSPrivateKeyKey]), nil
}

// tlsFingerprint returns the fingerprint of the TLS material the pods of the
// GWAdmin are reached with: a hash of the UID of the GWAdmin, the server name
// and what is read of its Secrets, the CA bundle, the client certificate and
// its key, so that a change of their metadata alone, e.g. a label, doesn't
// change it.
func tlsFingerprint(gw *operatorv1alpha1.GWAdmin, serverName string, material ...[]byte) string {
	h := sha256.New()
	for _, part := range slices.Concat([][]byte{[]byte(gw.UID), []byte(serverName)}, material) {
		// Each part is length-prefixed, so that no two lists of parts hash
		// the same.
		_ = binary.Write(h, binary.BigEndian, uint64(len(part)))
		h.Write(part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// getSecret returns a Secret of the GWAdmin, or a NotReadyError when it
// isn't there. what names it in messages, e.g. "API CA bundle". A Secret the
// GWAdmin keeps in the operator namespace is named after the UID of the
// GWAdmin, and labelled with it: when ownedBy is set, the label must match.
func (s *Syncer) getSecret(ctx context.Context, key client.ObjectKey, what string, ownedBy *operatorv1alpha1.GWAdmin) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := s.Client.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NotReadyError{
				Message: fmt.Sprintf("The %s Secret %s of the GWAdmin does not exist%s", what, key, SecretHint(s.SecretLabelSelector)),
				Cause:   CauseTLSNotReady,
			}
		}
		return nil, fmt.Errorf("failed to get the %s Secret %s of the GWAdmin: %w", what, key, err)
	}
	if ownedBy != nil && secret.Labels[consts.GWAdminUIDLabel] != string(ownedBy.UID) {
		return nil, NotReadyError{
			Message: fmt.Sprintf("The %s Secret %s doesn't belong to the GWAdmin %s", what, key, client.ObjectKeyFromObject(ownedBy)),
			Cause:   CauseTLSNotReady,
		}
	}
	return &secret, nil
}

// SecretHint tells how to make a Secret visible to the operator when its
// Secret cache is filtered by the label selector, or "" when it isn't.
func SecretHint(selector string) string {
	if selector == "" {
		return ""
	}
	return fmt.Sprintf(" (it must exist and carry the %q label to be visible to the operator)", selector)
}
