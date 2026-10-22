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

// Package published names what the GWAdmin controller publishes for the
// controllers that use a GWAdmin, so that they don't depend on the GWAdmin
// controller itself: the labels of its API Service.
package published

import (
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// APIServiceRoleLabel labels the API Service of a GWAdmin with its role,
// APIServiceRole.
const APIServiceRoleLabel = consts.OperatorLabelPrefix + "gwadmin-service-role"

// APIServiceRole is the value of APIServiceRoleLabel on the API Service.
const APIServiceRole = "api"

// APIServiceLabels returns the labels of the API Service of the named
// GWAdmin. Its EndpointSlices carry them too, as the EndpointSlice controller
// copies the labels of a Service onto its EndpointSlices: they tell the
// gw-admin pods of a GWAdmin, by its name.
func APIServiceLabels(gwAdminName string) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:     consts.GWAdminManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel: gwAdminName,
		APIServiceRoleLabel:                      APIServiceRole,
	}
}
