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
// allowed to use a GWAdmin in another namespace, and names the API Service
// and the operator Secrets the GWAdmin controller provisions.
package gwadminref

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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
