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

// Package gwadminref follows the reference of an OnPremEventGateway to its
// GWAdmin: it finds the gateways of a GWAdmin, tells whether a gateway is
// allowed to use a GWAdmin in another namespace, and names the API Service,
// the operator Secrets the GWAdmin controller provisions, and the values the
// data planes carry to tell which certificate authorities they were rolled
// out with.
package gwadminref

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/internal/utils/pki"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// IndexFieldOnPremEventGatewayOnGWAdmin is the index field of the
// OnPremEventGateways by the GWAdmin they reference, as namespace/name.
const IndexFieldOnPremEventGatewayOnGWAdmin = "onPremEventGatewayOnGWAdmin"

// IndexOptions returns the index of the OnPremEventGateways by the
// GWAdmin they reference, in any namespace.
func IndexOptions() []index.Option {
	return []index.Option{{
		Object: &eventgatewayv1alpha1.OnPremEventGateway{},
		Field:  IndexFieldOnPremEventGatewayOnGWAdmin,
		ExtractValueFn: func(obj client.Object) []string {
			gateway, ok := obj.(*eventgatewayv1alpha1.OnPremEventGateway)
			if !ok {
				return nil
			}
			return []string{gateway.GWAdminKey().String()}
		},
	}}
}

var (
	onPremEventGatewayGroupVersionKind = metav1.GroupVersionKind{
		Group:   eventgatewayv1alpha1.SchemeGroupVersion.Group,
		Version: eventgatewayv1alpha1.SchemeGroupVersion.Version,
		Kind:    eventgatewayv1alpha1.OnPremEventGatewayKind,
	}
	gwAdminGroupVersionKind = metav1.GroupVersionKind{
		Group:   operatorv1alpha1.SchemeGroupVersion.Group,
		Version: operatorv1alpha1.SchemeGroupVersion.Version,
		Kind:    operatorv1alpha1.GWAdminKind,
	}
)

// GatewaysOf returns the OnPremEventGateways that reference the
// GWAdmin, in any namespace, whether they are allowed to or not.
func GatewaysOf(ctx context.Context, cl client.Client, gwAdmin types.NamespacedName) ([]eventgatewayv1alpha1.OnPremEventGateway, error) {
	var gateways eventgatewayv1alpha1.OnPremEventGatewayList
	if err := cl.List(ctx, &gateways, client.MatchingFields{IndexFieldOnPremEventGatewayOnGWAdmin: gwAdmin.String()}); err != nil {
		return nil, fmt.Errorf("failed to list the OnPremEventGateways of the GWAdmin %s: %w", gwAdmin, err)
	}
	return gateways.Items, nil
}

// NotAllowed returns why the gateway can't use the GWAdmin it
// references, or "": a GWAdmin in another namespace needs a
// KongReferenceGrant in its namespace allowing the OnPremEventGateways of
// the namespace of the gateway to reference it.
func NotAllowed(ctx context.Context, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) (string, error) {
	key := gateway.GWAdminKey()
	if key.Namespace == gateway.Namespace {
		return "", nil
	}
	err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl,
		gateway.Namespace, key.Namespace, key.Name, onPremEventGatewayGroupVersionKind, gwAdminGroupVersionKind)
	if crossnamespace.IsReferenceNotGranted(err) {
		return fmt.Sprintf("no KongReferenceGrant in namespace %s allows the OnPremEventGateways of namespace %s to reference the GWAdmin %s",
			key.Namespace, gateway.Namespace, key), nil
	}
	return "", err
}

// GatewaysAllowedToUse returns the gateways of the list allowed to use the
// GWAdmin: those of its namespace, and those of the namespaces a
// KongReferenceGrant in its namespace allows.
func GatewaysAllowedToUse(ctx context.Context, cl client.Client, gwAdmin types.NamespacedName, gateways []eventgatewayv1alpha1.OnPremEventGateway) ([]eventgatewayv1alpha1.OnPremEventGateway, error) {
	var grants configurationv1alpha1.KongReferenceGrantList
	if err := cl.List(ctx, &grants, client.InNamespace(gwAdmin.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list the KongReferenceGrants of namespace %s: %w", gwAdmin.Namespace, err)
	}
	allowed := make([]eventgatewayv1alpha1.OnPremEventGateway, 0, len(gateways))
	for _, g := range gateways {
		if g.Namespace == gwAdmin.Namespace ||
			crossnamespace.ReferenceGrantsAllow(grants.Items, g.Namespace, gwAdmin.Name, onPremEventGatewayGroupVersionKind, gwAdminGroupVersionKind) {
			allowed = append(allowed, g)
		}
	}
	return allowed, nil
}

// NamespacesGrantedBy returns the namespaces whose OnPremEventGateways the
// KongReferenceGrant allows to reference GWAdmins of its namespace, some or
// all of them, other than its own namespace.
func NamespacesGrantedBy(grant *configurationv1alpha1.KongReferenceGrant) []string {
	if !slices.ContainsFunc(grant.Spec.To, func(to configurationv1alpha1.ReferenceGrantTo) bool {
		return string(to.Group) == gwAdminGroupVersionKind.Group && string(to.Kind) == gwAdminGroupVersionKind.Kind
	}) {
		return nil
	}
	var namespaces []string
	for _, from := range grant.Spec.From {
		ns := string(from.Namespace)
		if string(from.Group) == onPremEventGatewayGroupVersionKind.Group &&
			string(from.Kind) == onPremEventGatewayGroupVersionKind.Kind &&
			ns != grant.Namespace && !slices.Contains(namespaces, ns) {
			namespaces = append(namespaces, ns)
		}
	}
	return namespaces
}

// OfSecret returns the GWAdmin a Secret belongs to: its labels name the
// GWAdmin, and, for the Secrets kept in the operator namespace, its
// namespace.
func OfSecret(obj client.Object) (types.NamespacedName, bool) {
	labels := obj.GetLabels()
	name := labels[consts.GatewayOperatorManagedByNameLabel]
	if labels[consts.GWAdminUIDLabel] == "" || name == "" {
		return types.NamespacedName{}, false
	}
	return types.NamespacedName{Namespace: cmp.Or(labels[consts.GatewayOperatorManagedByNamespaceLabel], obj.GetNamespace()), Name: name}, true
}

// OfGrant returns the GWAdmins a KongReferenceGrant may allow objects of
// other namespaces to reference: those of its namespace.
func OfGrant(ctx context.Context, cl client.Client, grant client.Object) ([]types.NamespacedName, error) {
	var gwAdmins operatorv1alpha1.GWAdminList
	if err := cl.List(ctx, &gwAdmins, client.InNamespace(grant.GetNamespace())); err != nil {
		return nil, fmt.Errorf("failed to list the GWAdmins of namespace %s: %w", grant.GetNamespace(), err)
	}
	keys := make([]types.NamespacedName, 0, len(gwAdmins.Items))
	for i := range gwAdmins.Items {
		keys = append(keys, client.ObjectKeyFromObject(&gwAdmins.Items[i]))
	}
	return keys, nil
}

// APIServiceLabels returns the labels of the API Service of the named
// GWAdmin. Its EndpointSlices carry them too, as the EndpointSlice controller
// copies the labels of a Service onto its EndpointSlices: they tell the
// gw-admin pods of a GWAdmin, by its name.
func APIServiceLabels(gwAdminName string) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:     consts.GWAdminManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel: gwAdminName,
		consts.GWAdminServiceRoleLabel:           consts.GWAdminServiceRoleAPI,
	}
}

// The Secrets only Kong Operator uses live in the operator namespace, named
// after the UID of the GWAdmin.

// OperatorSecretPrefix returns the prefix of the names of the Secrets the
// GWAdmin keeps in the operator namespace.
func OperatorSecretPrefix(gw *operatorv1alpha1.GWAdmin) string { return "gwadmin-" + string(gw.UID) }

// OperatorClientTLSSecretName returns the name of the Secret, in the operator
// namespace, holding the client certificate Kong Operator authenticates to
// the API listener of the GWAdmin with.
func OperatorClientTLSSecretName(gw *operatorv1alpha1.GWAdmin) string {
	return OperatorSecretPrefix(gw) + "-operator-client-tls"
}

// CPClientCASecretName returns the name of the Secret, in the operator
// namespace, the cp client CA of the GWAdmin is published in: tls.crt and
// tls.key hold the certificate authority the controller managing the data
// planes issues their client certificates from. It only ever holds one the
// gw-admin pods trust.
func CPClientCASecretName(gw *operatorv1alpha1.GWAdmin) string {
	return OperatorSecretPrefix(gw) + "-cp-client-ca"
}

// PreviousCAKey is the key, on the Secret of a certificate authority the
// GWAdmin controller owns, holding the certificate authorities its domain
// moves off. On the published cp client CA, they are those published before
// it, which the gw-admin pods keep trusting until every data plane runs a
// certificate from the published one.
const PreviousCAKey = "previous-ca.crt"

// DataPlaneServerBundleChecksum returns the value of the
// GWAdminDataPlaneServerBundleAnnotation of a data plane mounting the cp
// server bundle Secret.
func DataPlaneServerBundleChecksum(bundle *corev1.Secret) string {
	var data []byte
	if bundle != nil {
		data = bundle.Data[consts.CACRT]
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DataPlaneClientCAFingerprint returns the value of the
// GWAdminDataPlaneClientCAAnnotation of a data plane whose client
// certificate is issued from the cp client CA in caPEM: the published
// Secret's tls.crt. It returns "" when caPEM holds no certificate.
func DataPlaneClientCAFingerprint(caPEM []byte) string {
	certs := pki.ParseCertificates(caPEM)
	if len(certs) == 0 {
		return ""
	}
	return pki.Fingerprint(certs[0])
}

// GroupVersionResources returns the resources a controller following the
// GWAdmin of an OnPremEventGateway needs: the gateways, the GWAdmins serving
// them, and the KongReferenceGrants allowing a gateway to use a GWAdmin in
// another namespace.
func GroupVersionResources() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
		eventgatewayv1alpha1.SchemeGroupVersion.WithResource("onpremeventgateways"),
		operatorv1alpha1.GWAdminGVR(),
		configurationv1alpha1.SchemeGroupVersion.WithResource("kongreferencegrants"),
	}
}

// NotReadyError tells what the GWAdmin of a gateway doesn't provide yet. It
// is not retried: a change to the GWAdmin or to its Secrets triggers the
// controllers following it again.
type NotReadyError struct {
	Message string
}

func (e NotReadyError) Error() string { return e.Message }

// SecretHint tells how to make a Secret visible to the operator when its
// Secret cache is filtered by the label selector, or "" when it isn't.
func SecretHint(selector string) string {
	if selector == "" {
		return ""
	}
	return fmt.Sprintf(" (it must exist and carry the %q label to be visible to the operator)", selector)
}

// GetSecret returns a Secret of the GWAdmin. what names it in messages,
// e.g. "API CA bundle". A Secret the GWAdmin publishes in the operator
// namespace is named after the UID of the GWAdmin, and labelled with it:
// when ownedBy is set, the label must match. The message of a Secret that
// isn't found tells how to make it visible under the label selector of the
// operator's Secret cache (see SecretHint).
func GetSecret(
	ctx context.Context, cl client.Client, key client.ObjectKey, what string, ownedBy *operatorv1alpha1.GWAdmin, selector string,
) (*corev1.Secret, error) {
	var secret corev1.Secret
	if err := cl.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, NotReadyError{Message: fmt.Sprintf("The %s Secret %s of the GWAdmin does not exist%s", what, key, SecretHint(selector))}
		}
		return nil, fmt.Errorf("failed to get the %s Secret %s of the GWAdmin: %w", what, key, err)
	}
	if ownedBy != nil && secret.Labels[consts.GWAdminUIDLabel] != string(ownedBy.UID) {
		return nil, NotReadyError{Message: fmt.Sprintf("The %s Secret %s doesn't belong to the GWAdmin %s", what, key, client.ObjectKeyFromObject(ownedBy))}
	}
	return &secret, nil
}
