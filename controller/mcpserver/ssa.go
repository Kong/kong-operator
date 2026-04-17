package mcpserver

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/managedfields"
	ctrl "sigs.k8s.io/controller-runtime"

	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
)

// requiredSchemas lists the GroupVersions whose OpenAPI schemas are needed by
// this controller for structured-merge-diff merging and diff-before-apply.
var requiredSchemas = []schema.GroupVersion{
	// core: Service
	{Group: "", Version: "v1"},
	// Deployment
	{Group: "apps", Version: "v1"},
}

// initTypeConverter creates a TypeConverter from the API server's OpenAPI v3 schemas.
// Only the schemas needed by this controller are fetched.
// Called once during SetupWithManager.
func initTypeConverter(mgr ctrl.Manager) (managedfields.TypeConverter, error) {
	return controllerpkgssa.NewTypeConverter(mgr, requiredSchemas)
}
