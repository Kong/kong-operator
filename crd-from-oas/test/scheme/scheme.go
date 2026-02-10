package scheme

import (
	konnectv1alpha1 "github.com/kong/kong-operator/crd-from-oas/api/konnect/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// Get returns a scheme aware of all types the manager can interact with.
func Get() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(konnectv1alpha1.AddToScheme(scheme))
	return scheme
}
