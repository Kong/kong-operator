// This script cleans up orphaned GKE clusters and Konnect runtime
// groups that were created by the e2e tests (caued by e.g. unexpected
// crash that didn't allow a test's teardown to be completed correctly).
// It's meant to be installed as a cronjob and run repeatedly throughout
// the day to catch any orphaned resources: however tests should be trying to
// delete the resources they create themselves.
//
// A cluster is considered orphaned when all conditions are satisfied:
// 1. Its name begins with a predefined prefix (`gke-e2e-`).
// 2. It was created more than 1h ago.
//
// A control plane is considered orphaned when all conditions are satisfied:
// 1. It has a label `created_in_tests` with value `true`.
// 2. It was created more than 1h ago.
//
// A system account is considered orphaned when all conditions are satisfied:
// 1. It is not managed by Konnect.
// 2. It was created more than 1h ago.
//
// Usage: `go run ./hack/cleanup [mode]`
// Where `mode` is one of:
// - `all` (default): clean up both GKE clusters and Konnect control planes
// - `gke`: clean up only GKE clusters
// - `konnect`: clean up only Konnect control planes and system accounts
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters/types/gke"
	"go.uber.org/zap"

	"github.com/kong/kong-operator/v2/test"
)

const (
	cleanupModeAll     = "all"
	cleanupModeGKE     = "gke"
	cleanupModeKonnect = "konnect"

	konnectAccessTokenVar = "KONG_TEST_KONNECT_ACCESS_TOKEN" //nolint:gosec
	konnectServerURLVar   = "KONG_TEST_KONNECT_SERVER_URL"
)

var (
	// GKE environment variables.
	gkeCreds    = os.Getenv(gke.GKECredsVar)
	gkeProject  = os.Getenv(gke.GKEProjectVar)
	gkeLocation = os.Getenv(gke.GKELocationVar)
)

func main() {
	zaplog, err := zap.NewDevelopment()
	if err != nil {
		os.Exit(1)
	}
	log := zapr.NewLogger(zaplog)

	mode, err := getCleanupMode()
	if err != nil {
		log.Error(err, "error getting cleanup mode")
		os.Exit(1)
	}

	if err := validateVars(mode); err != nil {
		log.Error(err, "error validating vars")
		os.Exit(1)
	}

	cleanupFuncs, err := resolveCleanupFuncs(mode)
	if err != nil {
		log.Error(err, "error resolving cleanup functions")
		os.Exit(1)
	}
	ctx := context.Background()
	for _, f := range cleanupFuncs {
		if err := f(ctx, log); err != nil {
			log.Error(err, "error running cleanup function")
			os.Exit(1)
		}
	}
}

func getCleanupMode() (string, error) {
	if len(os.Args) < 2 {
		return cleanupModeAll, nil
	}

	switch os.Args[1] {
	case cleanupModeAll:
	case cleanupModeGKE:
	case cleanupModeKonnect:
	default:
		return "", fmt.Errorf("invalid cleanup mode: %s", os.Args[1])
	}

	return os.Args[1], nil
}

func generateSDK() (*sdkkonnectgo.SDK, error) {
	serverURL, err := canonicalizedServerURL()
	if err != nil {
		return nil, fmt.Errorf("invalid server URL %s: %w", test.KonnectServerURL(), err)
	}

	return sdkkonnectgo.New(
		sdkkonnectgo.WithSecurity(
			sdkkonnectcomp.Security{
				PersonalAccessToken: new(test.KonnectAccessToken()),
			},
		),
		sdkkonnectgo.WithServerURL(serverURL),
	), nil
}

func resolveCleanupFuncs(mode string) ([]func(context.Context, logr.Logger) error, error) {

	switch mode {
	case cleanupModeGKE:
		return []func(context.Context, logr.Logger) error{
			cleanupGKEClusters,
		}, nil
	case cleanupModeKonnect:
		sdk, err := generateSDK()
		if err != nil {
			return nil, fmt.Errorf("error generating SDK: %w", err)
		}

		return []func(context.Context, logr.Logger) error{
			cleanupKonnectEventGateways(sdk),
			cleanupKonnectControlPlanes(sdk),
			cleanupKonnectSystemAccounts(sdk),
		}, nil
	default:
		sdk, err := generateSDK()
		if err != nil {
			return nil, fmt.Errorf("error generating SDK: %w", err)
		}

		return []func(context.Context, logr.Logger) error{
			cleanupGKEClusters,
			cleanupKonnectEventGateways(sdk),
			cleanupKonnectControlPlanes(sdk),
			cleanupKonnectSystemAccounts(sdk),
		}, nil
	}
}

func validateVars(mode string) error {
	switch mode {
	case cleanupModeGKE:
		return validateGKEVars()
	case cleanupModeKonnect:
		return validateKonnectVars()
	default:
		if err := validateGKEVars(); err != nil {
			return err
		}
		if err := validateKonnectVars(); err != nil {
			return err
		}
		return nil
	}
}

func validateKonnectVars() error {
	return errors.Join(
		notEmpty(konnectAccessTokenVar, test.KonnectAccessToken()),
		notEmpty(konnectServerURLVar, test.KonnectServerURL()),
	)
}

func validateGKEVars() error {
	if err := notEmpty(gke.GKECredsVar, gkeCreds); err != nil {
		return err
	}
	if err := notEmpty(gke.GKEProjectVar, gkeProject); err != nil {
		return err
	}
	return notEmpty(gke.GKELocationVar, gkeLocation)
}

func notEmpty(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s was empty", name)
	}
	return nil
}
