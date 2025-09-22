package manager_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmgr "sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kong/kong-operator/modules/manager"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
)

func TestSetupControllers(t *testing.T) {
	t.Parallel()

	// Actual values of parameters are not important for this test.
	mgr, err := ctrl.NewManager(&rest.Config{}, ctrlmgr.Options{})
	require.NoError(t, err)
	cfg := testutils.DefaultControllerConfigForTests()
	controllerDefs, err := manager.SetupControllers(mgr, &cfg, nil)
	require.NoError(t, err)

	const expectedControllerCount = 42
	require.Len(t, controllerDefs, expectedControllerCount)

	seenControllerTypes := make(map[string]int, expectedControllerCount)
	for _, def := range controllerDefs {
		seenControllerTypes[reflect.TypeOf(def.Controller).String()]++
	}
	duplicates := make(map[string]int)
	for typeName, count := range seenControllerTypes {
		if count > 1 {
			duplicates[typeName] = count
		}
	}
	require.Empty(t, duplicates, "found duplicate controller types: %v", duplicates)
}
