package manager_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kong/kong-operator/v2/modules/manager"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
)

type allKindsRESTMapper struct {
	meta.RESTMapper
}

func (allKindsRESTMapper) KindFor(gvr schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return gvr.GroupVersion().WithKind(gvr.Resource), nil
}

func TestSetupControllers(t *testing.T) {
	t.Parallel()

	// Actual values of parameters are not important for this test.
	// A RESTMapper that resolves everything is supplied because there is no
	// apiserver here: without it, SetupControllers' CRD checks fail with a
	// discovery error rather than finding the CRDs installed.
	mgr, err := ctrl.NewManager(&rest.Config{}, ctrlmgr.Options{
		MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) {
			return allKindsRESTMapper{RESTMapper: meta.NewDefaultRESTMapper(nil)}, nil
		},
	})
	require.NoError(t, err)
	cfg := testutils.DefaultControllerConfigForTests()
	controllerDefs, err := manager.SetupControllers(mgr, &cfg, nil, nil)
	require.NoError(t, err)

	const expectedControllerCount = 85
	require.Len(t, controllerDefs, expectedControllerCount)

	seenControllerTypes := make(map[string]int, expectedControllerCount)
	for _, def := range controllerDefs {
		// Use PkgPath + Name instead of reflect.Type.String() so that two
		// controllers with the same struct name in different packages
		// (e.g. controller/dataplane.Reconciler and
		// controller/eventgateway/dataplane.Reconciler) are treated as
		// distinct types rather than false duplicates.
		elem := reflect.TypeOf(def.Controller).Elem()
		key := elem.PkgPath() + "." + elem.Name()
		seenControllerTypes[key]++
	}
	duplicates := make(map[string]int)
	for typeName, count := range seenControllerTypes {
		if count > 1 {
			duplicates[typeName] = count
		}
	}
	require.Empty(t, duplicates, "found duplicate controller types: %v", duplicates)
}
