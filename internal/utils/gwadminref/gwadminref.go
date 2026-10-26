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
// GWAdmin: it finds the gateways of a GWAdmin, and tells whether a gateway is
// allowed to use a GWAdmin in another namespace.
package gwadminref

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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

// OfGrant returns the GWAdmins a KongReferenceGrant may allow objects of
// other namespaces to reference: those of its namespace.
func OfGrant(ctx context.Context, cl client.Client, grant client.Object) []types.NamespacedName {
	var gwAdmins operatorv1alpha1.GWAdminList
	if err := cl.List(ctx, &gwAdmins, client.InNamespace(grant.GetNamespace())); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list the GWAdmins of a KongReferenceGrant", "grant", client.ObjectKeyFromObject(grant))
		return nil
	}
	keys := make([]types.NamespacedName, 0, len(gwAdmins.Items))
	for i := range gwAdmins.Items {
		keys = append(keys, client.ObjectKeyFromObject(&gwAdmins.Items[i]))
	}
	return keys
}
