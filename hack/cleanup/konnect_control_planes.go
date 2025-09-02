package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/go-logr/logr"
	"github.com/samber/lo"

	"github.com/kong/kong-operator/test"
)

const (
	konnectControlPlanesLimit     = int64(100)
	timeUntilControlPlaneOrphaned = time.Hour

	testIDLabel = "operator-test-id"
)

// cleanupKonnectControlPlanes deletes orphaned control planes created by the tests and their roles.
func cleanupKonnectControlPlanes(ctx context.Context, log logr.Logger) error {
	serverURL, err := canonicalizedServerURL()
	if err != nil {
		return fmt.Errorf("invalid server URL %s: %w", test.KonnectServerURL(), err)
	}

	// NOTE: The domain for global endpoints is overridden in cleanup.yaml workflow.
	// See https://github.com/Kong/sdk-konnect-go/issues/20 for details
	sdk := sdkkonnectgo.New(
		sdkkonnectgo.WithSecurity(
			sdkkonnectcomp.Security{
				PersonalAccessToken: sdkkonnectgo.String(test.KonnectAccessToken()),
			},
		),
		sdkkonnectgo.WithServerURL(serverURL),
	)

	orphanedCPs, err := findOrphanedControlPlanes(ctx, log, sdk.ControlPlanes)
	if err != nil {
		return fmt.Errorf("failed to find orphaned control planes: %w", err)
	}
	if err := deleteControlPlanes(ctx, log, sdk.ControlPlanes, orphanedCPs); err != nil {
		return fmt.Errorf("failed to delete control planes: %w", err)
	}

	return nil
}

// findOrphanedControlPlanes finds control planes that were created by the tests and are older than timeUntilControlPlaneOrphaned.
func findOrphanedControlPlanes(
	ctx context.Context,
	log logr.Logger,
	c *sdkkonnectgo.ControlPlanes,
) ([]string, error) {
	response, err := c.ListControlPlanes(ctx, sdkkonnectops.ListControlPlanesRequest{
		PageSize: lo.ToPtr(konnectControlPlanesLimit),
		// Filter the control planes created in the KO integration tests by existence of label `konghq.com/test-id`
		Labels: lo.ToPtr(testIDLabel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list control planes: %w", err)
	}
	if response.ListControlPlanesResponse == nil {
		body, err := io.ReadAll(response.RawResponse.Body)
		if err != nil {
			body = []byte(err.Error())
		}
		return nil, fmt.Errorf("failed to list control planes, status: %d, body: %s", response.GetStatusCode(), body)
	}

	var orphanedControlPlanes []string
	for _, ControlPlane := range response.ListControlPlanesResponse.Data {
		if ControlPlane.CreatedAt.IsZero() {
			log.Info("Control plane has no creation timestamp, skipping", "name", ControlPlane.Name)
			continue
		}
		orphanedAfter := ControlPlane.CreatedAt.Add(timeUntilControlPlaneOrphaned)
		if !time.Now().After(orphanedAfter) {
			log.Info("Control plane is not old enough to be considered orphaned, skipping",
				"name", ControlPlane.Name, "created_at", ControlPlane.CreatedAt,
			)
			continue
		}
		orphanedControlPlanes = append(orphanedControlPlanes, ControlPlane.ID)
	}
	return orphanedControlPlanes, nil
}

// deleteControlPlanes deletes control planes by their IDs.
func deleteControlPlanes(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.ControlPlanes,
	cpsIDs []string,
) error {
	if len(cpsIDs) < 1 {
		log.Info("No control planes to clean up")
		return nil
	}

	var errs []error
	for _, cpID := range cpsIDs {
		log.Info("Deleting control plane", "name", cpID)
		if _, err := sdk.DeleteControlPlane(ctx, cpID); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete control plane %s: %w", cpID, err))
		}
	}
	return errors.Join(errs...)
}

// canonicalizedServerURL returns the canonicalized Konnect API server URL (starting with https://) from environment variable.
func canonicalizedServerURL() (string, error) {
	serverURL := test.KonnectServerURL()
	serverURL = strings.TrimPrefix(serverURL, "http://")
	serverURL = strings.TrimPrefix(serverURL, "https://")
	serverURL = "https://" + serverURL

	if _, err := url.Parse(serverURL); err != nil {
		return "", err
	}
	return serverURL, nil
}
