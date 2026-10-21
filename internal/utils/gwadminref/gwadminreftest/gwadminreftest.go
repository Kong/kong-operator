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

// Package gwadminreftest builds the objects the tests of the references to a
// GWAdmin need.
package gwadminreftest

import (
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
)

// Grant returns the KongReferenceGrant namespace/name allowing the
// OnPremEventGateways of namespace from to reference the GWAdmin gwAdminName
// of its namespace, or any GWAdmin of it when gwAdminName is empty.
func Grant(namespace, name, from, gwAdminName string) *configurationv1alpha1.KongReferenceGrant {
	to := configurationv1alpha1.ReferenceGrantTo{
		Group: configurationv1alpha1.Group(operatorv1alpha1.SchemeGroupVersion.Group),
		Kind:  configurationv1alpha1.Kind(operatorv1alpha1.GWAdminKind),
	}
	if gwAdminName != "" {
		to.Name = new(configurationv1alpha1.ObjectName(gwAdminName))
	}
	return &configurationv1alpha1.KongReferenceGrant{
		Namespace: namespace, Name: name,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group:     configurationv1alpha1.Group(eventgatewayv1alpha1.SchemeGroupVersion.Group),
				Kind:      configurationv1alpha1.Kind(eventgatewayv1alpha1.OnPremEventGatewayKind),
				Namespace: configurationv1alpha1.Namespace(from),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{to},
		},
	}
}
