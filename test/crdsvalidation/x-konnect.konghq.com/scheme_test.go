package configuration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func Scheme(t *testing.T) *runtime.Scheme {
	scheme := scheme.Get()
	require.NoError(t, xkonnectv1alpha1.AddToScheme(scheme))
	return scheme
}
