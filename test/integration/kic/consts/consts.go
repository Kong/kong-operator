package consts

import (
	"time"

	testconsts "github.com/kong/kong-operator/ingress-controller/test/consts"
)

const (
	// WaitTick is the default timeout tick interval for checking on resources.
	WaitTick = 250 * time.Millisecond

	// IngressWait is the default amount of time to wait for any particular ingress resource to be provisioned.
	IngressWait = 3 * time.Minute

	// StatusWait is the default amount of time to wait for object statuses to fulfill a provided predicate.
	StatusWait = 3 * time.Minute

	// IngressClass indicates the ingress class name which the tests will use for supported object reconciliation.
	IngressClass = testconsts.IngressClass

	// KongTestPassword is used for integration tests with Kong Gateway.
	KongTestPassword = testconsts.KongTestPassword

	// ControllerNamespace is the Kubernetes namespace where the controller is deployed.
	ControllerNamespace = testconsts.ControllerNamespace

	// JPEGMagicNumber is the magic number that identifies a JPEG file.
	JPEGMagicNumber = testconsts.JPEGMagicNumber

	// PNGMagicNumber is the magic number that identifies a PNG file.
	PNGMagicNumber = testconsts.PNGMagicNumber

	// ExitCodeIncompatibleOptions indicates incompatible options.
	ExitCodeIncompatibleOptions = testconsts.ExitCodeIncompatibleOptions
	// ExitCodeCantUseExistingCluster indicates existing cluster unusable.
	ExitCodeCantUseExistingCluster = testconsts.ExitCodeCantUseExistingCluster
	// ExitCodeEnvSetupFailed indicates env setup failure.
	ExitCodeEnvSetupFailed = testconsts.ExitCodeEnvSetupFailed
	// ExitCodeCantCreateLogger indicates logger creation failure.
	ExitCodeCantCreateLogger = testconsts.ExitCodeCantCreateLogger

	// KongTestWorkspace is used for integration tests with Kong Gateway.
	KongTestWorkspace = testconsts.KongTestWorkspace

	// DefaultControllerFeatureGates are the default feature gates for tests.
	DefaultControllerFeatureGates = testconsts.DefaultControllerFeatureGates
)
