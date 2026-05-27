package manager

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

type fakeCRDChecker struct {
	missing map[schema.GroupVersionResource]struct{}
}

func (f *fakeCRDChecker) CRDExists(gvr schema.GroupVersionResource) (bool, error) {
	_, missing := f.missing[gvr]
	return !missing, nil
}

func defaultConfigWithDisabledControllers() Config {
	c := DefaultConfig()
	c.GatewayControllerEnabled = false
	c.ControlPlaneControllerEnabled = false
	c.DataPlaneControllerEnabled = false
	c.DataPlaneBlueGreenControllerEnabled = false
	c.AIGatewayControllerEnabled = false
	c.KongPluginInstallationControllerEnabled = false
	c.ControlPlaneExtensionsControllerEnabled = false
	c.KonnectControllersEnabled = false
	c.KEGDataPlaneControllerEnabled = false
	return c
}

func TestEnsureRequiredCRDsChecksGatewayConfigurationForGatewayController(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()
	cfg.GatewayControllerEnabled = true

	missingGVR := gwtypes.GatewayConfigurationGVR()
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			missingGVR: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.Error(t, err)
	require.ErrorContains(t, err, missingGVR.String())
}

func TestEnsureRequiredCRDsChecksWatchNamespaceGrantForControlPlaneController(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()
	cfg.ControlPlaneControllerEnabled = true

	missingGVR := schema.GroupVersionResource{
		Group:    operatorv1alpha1.SchemeGroupVersion.Group,
		Version:  operatorv1alpha1.SchemeGroupVersion.Version,
		Resource: "watchnamespacegrants",
	}
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			missingGVR: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.Error(t, err)
	require.ErrorContains(t, err, missingGVR.String())
}

func TestEnsureRequiredCRDsChecksDataPlaneMetricsExtensionForControlPlaneExtensionsController(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()
	cfg.ControlPlaneExtensionsControllerEnabled = true

	missingGVR := schema.GroupVersionResource{
		Group:    operatorv1alpha1.SchemeGroupVersion.Group,
		Version:  operatorv1alpha1.SchemeGroupVersion.Version,
		Resource: "dataplanemetricsextensions",
	}
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			missingGVR: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.Error(t, err)
	require.ErrorContains(t, err, missingGVR.String())
}

func TestEnsureRequiredCRDsChecksKonnectCloudGatewayTransitGatewayForKonnectControllers(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()
	cfg.KonnectControllersEnabled = true

	missingGVR := schema.GroupVersionResource{
		Group:    konnectv1alpha1.SchemeGroupVersion.Group,
		Version:  konnectv1alpha1.SchemeGroupVersion.Version,
		Resource: "konnectcloudgatewaytransitgateways",
	}
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			missingGVR: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.Error(t, err)
	require.ErrorContains(t, err, missingGVR.String())
}

func TestEnsureRequiredCRDsChecksKegDataPlaneForKEGDataPlaneController(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()
	cfg.KEGDataPlaneControllerEnabled = true

	missingGVR := schema.GroupVersionResource{
		Group:    eventgatewayv1alpha1.SchemeGroupVersion.Group,
		Version:  eventgatewayv1alpha1.SchemeGroupVersion.Version,
		Resource: "kegdataplanes",
	}
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			missingGVR: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.Error(t, err)
	require.ErrorContains(t, err, missingGVR.String())
}

func TestEnsureRequiredCRDsSkipsDisabledControllerChecks(t *testing.T) {
	t.Parallel()

	cfg := defaultConfigWithDisabledControllers()

	skippedGVR := gwtypes.GatewayConfigurationGVR()
	checker := &fakeCRDChecker{
		missing: map[schema.GroupVersionResource]struct{}{
			skippedGVR: {},
			{
				Group:    configurationv1alpha1.SchemeGroupVersion.Group,
				Version:  configurationv1alpha1.SchemeGroupVersion.Version,
				Resource: "kongreferencegrants",
			}: {},
		},
	}

	err := ensureRequiredCRDs(&cfg, checker)
	require.NoError(t, err)
}
