/*
Copyright 2025 Kong, Inc.

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

package dataplane

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	ctrl "sigs.k8s.io/controller-runtime"

	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
)

// requiredSchemas lists the GroupVersions whose OpenAPI schemas are needed by
// this controller for structured-merge-diff merging and diff-before-apply.
var requiredSchemas = []schema.GroupVersion{
	// core: Service, Secret, ConfigMap
	{Group: "", Version: "v1"},
	// Deployment
	{Group: "apps", Version: "v1"},
	// KegDataPlane (status apply)
	{Group: "eventgateway.konghq.com", Version: "v1alpha1"},
	// EventGatewayDataPlaneCertificate
	{Group: "configuration.konghq.com", Version: "v1alpha1"},
	// KonnectEventGateway
	{Group: "konnect.konghq.com", Version: "v1alpha1"},
}

// initTypeConverter creates a TypeConverter from the API server's OpenAPI v3 schemas.
// Only the schemas needed by this controller are fetched.
// Called once during SetupWithManager.
func initTypeConverter(mgr ctrl.Manager) (managedfields.TypeConverter, error) {
	return controllerpkgssa.NewTypeConverter(mgr, requiredSchemas)
}
