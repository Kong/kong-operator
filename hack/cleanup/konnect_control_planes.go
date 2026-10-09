package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
)

const (
	konnectControlPlanesLimit     = int64(100)
	konnectEventGatewaysLimit     = int64(100)
	timeUntilControlPlaneOrphaned = time.Hour

	// konnectUserRolesPageSize is the page size used when listing user roles.
	// The SDK's ListUserRoles doesn't support pagination parameters, so the
	// listing is done with raw HTTP requests paging through the results.
	konnectUserRolesPageSize = 100

	// konnectCleanupConcurrencyDefault is the default number of parallel goroutines
	// used for Konnect API calls (listing role pages, deleting roles).
	konnectCleanupConcurrencyDefault = 8

	// konnectCleanupConcurrencyVar is the environment variable that can be used
	// to override the number of parallel goroutines used for Konnect API calls.
	konnectCleanupConcurrencyVar = "KONNECT_CLEANUP_CONCURRENCY"

	k8sKindKonnectGatewayControlPlane = "KonnectGatewayControlPlane"
)

// cleanupConcurrency returns the number of parallel goroutines used for Konnect
// API calls (listing role pages, deleting roles).
func cleanupConcurrency() int {
	if v := os.Getenv(konnectCleanupConcurrencyVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return konnectCleanupConcurrencyDefault
}

func cleanupKonnectEventGateways(sdk *sdkkonnectgo.SDK) func(ctx context.Context, log logr.Logger) error {
	return func(ctx context.Context, log logr.Logger) error {
		orphanedEventGateways, err := findOrphanedEventGateways(ctx, log, sdk.EventGateways)
		if err != nil {
			return fmt.Errorf("failed to find orphaned event gateways: %w", err)
		}
		if err := deleteEventGateways(ctx, log, sdk.EventGateways, orphanedEventGateways); err != nil {
			return fmt.Errorf("failed to delete event gateways: %w", err)
		}
		return nil
	}
}

// cleanupKonnectControlPlanes deletes orphaned control planes created by the tests and their roles.
func cleanupKonnectControlPlanes(sdk *sdkkonnectgo.SDK) func(ctx context.Context, log logr.Logger) error {
	return func(ctx context.Context, log logr.Logger) error {
		me, err := sdk.Me.GetUsersMe(ctx)
		if err != nil {
			return fmt.Errorf("failed to get user info: %w", err)
		}
		if me.User == nil || me.User.ID == nil {
			return errors.New("failed to get user info, user is nil")
		}

		orphanedCPs, err := findOrphanedControlPlanes(ctx, log, sdk.ControlPlanes)
		if err != nil {
			return fmt.Errorf("failed to find orphaned control planes: %w", err)
		}
		if err := deleteControlPlanes(ctx, log, sdk.ControlPlanes, orphanedCPs); err != nil {
			return fmt.Errorf("failed to delete control planes: %w", err)
		}

		userID := *me.User.ID
		userID = "63da16fc-94e7-4cbf-bf12-1b2623e0c0d6"

		log.Info("User", "user_id", userID)

		// We have to manually delete roles created for the control plane because Konnect doesn't do it automatically.
		// If we don't do it, we will eventually hit a problem with Konnect APIs answering our requests with 504s
		// because of a performance issue when there's too many roles for the account
		// (see https://konghq.atlassian.net/browse/TPS-1319).
		//
		// We can drop this once the automated cleanup is implemented on Konnect side:
		// https://konghq.atlassian.net/browse/TPS-1453.
		log.Info("Listing existing Control Planes", "user_id", userID)
		existingCPIDs, err := listControlPlaneIDsPaged(ctx, log, sdk.ControlPlanes)
		if err != nil {
			return fmt.Errorf("failed to list existing control planes: %w", err)
		}
		log.Info("Listed existing control planes", "count", len(existingCPIDs))

		if err := removeOrphanedControlPlaneRoles(ctx, log, sdk.Roles, userID, existingCPIDs, orphanedCPs); err != nil {
			return fmt.Errorf("failed to remove control plane roles: %w", err)
		}

		return nil
	}
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

func findOrphanedEventGateways(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.EventGateways,
) ([]string, error) {

	seenEventGatewayIDs := make(map[string]struct{})
	var orphanedEventGateways []string

	response, err := sdk.ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
		PageSize: new(konnectEventGatewaysLimit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list event gateways: %w", err)
	}
	if response.ListEventGatewaysResponse == nil {
		body, err := io.ReadAll(response.RawResponse.Body)
		if err != nil {
			body = []byte(err.Error())
		}
		return nil, fmt.Errorf("failed to list event gateways, status: %d, body: %s", response.GetStatusCode(), body)
	}

	for _, eventGateway := range response.ListEventGatewaysResponse.Data {
		// Skip if we've already processed this event gateway
		if _, seen := seenEventGatewayIDs[eventGateway.ID]; seen {
			continue
		}
		seenEventGatewayIDs[eventGateway.ID] = struct{}{}

		if eventGateway.Labels["test"] == "" {
			log.Info("EventGateway has no test label, skipping", "name", eventGateway.Name)
			continue
		}

		if eventGateway.CreatedAt.IsZero() {
			log.Info("EventGateway has no creation timestamp, skipping", "name", eventGateway.Name)
			continue
		}
		orphanedAfter := eventGateway.CreatedAt.Add(timeUntilControlPlaneOrphaned)
		if !time.Now().After(orphanedAfter) {
			log.Info("EventGateway is not old enough to be considered orphaned, skipping",
				"name", eventGateway.Name, "created_at", eventGateway.CreatedAt,
			)
			continue
		}
		orphanedEventGateways = append(orphanedEventGateways, eventGateway.ID)
	}

	return orphanedEventGateways, nil
}

// findOrphanedControlPlanes finds control planes that were created by the tests and are older than timeUntilControlPlaneOrphaned.
func findOrphanedControlPlanes(
	ctx context.Context,
	log logr.Logger,
	c *sdkkonnectgo.ControlPlanes,
) ([]string, error) {
	// We need to query control planes with two different label filters:
	// 1. Control planes created by integration tests (with `operator-test-id` label)
	// 2. Control planes managed by KO (with `k8s-kind:KonnectGatewayControlPlane` label)
	// 3. Control planes created by tests (with `created_in_tests` label)
	labelFilters := []string{
		deploy.KonnectTestIDLabel,
		fmt.Sprintf("%s:%s", ops.KubernetesKindLabelKey, k8sKindKonnectGatewayControlPlane),
		deploy.KonnectCreatedInTestsLabel,
	}

	seenCPIDs := make(map[string]struct{})
	var orphanedControlPlanes []string

	for _, labelFilter := range labelFilters {
		response, err := c.ListControlPlanes(ctx, sdkkonnectops.ListControlPlanesRequest{
			PageSize:     new(konnectControlPlanesLimit),
			FilterLabels: new(labelFilter),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list control planes with label %s: %w", labelFilter, err)
		}
		if response.ListControlPlanesResponse == nil {
			body, err := io.ReadAll(response.RawResponse.Body)
			if err != nil {
				body = []byte(err.Error())
			}
			return nil, fmt.Errorf("failed to list control planes, status: %d, body: %s", response.GetStatusCode(), body)
		}

		for _, ControlPlane := range response.ListControlPlanesResponse.Data {
			// Skip if we've already processed this control plane
			if _, seen := seenCPIDs[ControlPlane.ID]; seen {
				continue
			}
			seenCPIDs[ControlPlane.ID] = struct{}{}

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
	}
	return orphanedControlPlanes, nil
}

func deleteEventGateways(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.EventGateways,
	egIDs []string,
) error {
	if len(egIDs) < 1 {
		log.Info("No event gateways to clean up")
		return nil
	}

	var errs []error
	for _, egID := range egIDs {
		log.Info("Deleting event gateway", "ID", egID)
		if _, err := sdk.DeleteEventGateway(ctx, egID); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete event gateway %s: %w", egID, err))
		}
	}
	return errors.Join(errs...)
}

// listControlPlaneIDsPaged lists the IDs of all control planes, paging through
// the results. No label filter is applied: the full set of existing IDs is
// needed so that only roles of control planes that no longer exist get removed.
func listControlPlaneIDsPaged(
	ctx context.Context,
	log logr.Logger,
	c *sdkkonnectgo.ControlPlanes,
) ([]string, error) {
	var ids []string
	for pageNumber := int64(1); ; pageNumber++ {
		response, err := c.ListControlPlanes(ctx, sdkkonnectops.ListControlPlanesRequest{
			PageSize:   new(konnectControlPlanesLimit),
			PageNumber: new(pageNumber),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list control planes (page %d): %w", pageNumber, err)
		}
		if response.ListControlPlanesResponse == nil {
			body, err := io.ReadAll(response.RawResponse.Body)
			if err != nil {
				body = []byte(err.Error())
			}
			return nil, fmt.Errorf("failed to list control planes, status: %d, body: %s", response.GetStatusCode(), body)
		}

		controlPlanes := response.ListControlPlanesResponse.Data
		if len(controlPlanes) == 0 {
			break
		}
		for _, cp := range controlPlanes {
			ids = append(ids, cp.ID)
		}
		log.Info("Fetched control planes page", "page", pageNumber, "count", len(controlPlanes), "total", len(ids))
		if int64(len(controlPlanes)) < konnectControlPlanesLimit {
			break
		}
	}
	return ids, nil
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
		log.Info("Deleting control plane", "ID", cpID)
		if _, err := sdk.DeleteControlPlane(ctx, cpID); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete control plane %s: %w", cpID, err))
		}
	}
	return errors.Join(errs...)
}
