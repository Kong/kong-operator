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

	"github.com/kong/kong-operator/controller/konnect/ops"
	"github.com/kong/kong-operator/test"
)

const (
	konnectControlPlanesLimit     = int64(100)
	timeUntilControlPlaneOrphaned = time.Hour

	testIDLabel                       = "operator-test-id"
	k8sKindKonnectGatewayControlPlane = "KonnectGatewayControlPlane"
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

	// We have to manually delete roles created for the control plane because Konnect doesn't do it automatically.
	// If we don't do it, we will eventually hit a problem with Konnect APIs answering our requests with 504s
	// because of a performance issue when there's too many roles for the account
	// (see https://konghq.atlassian.net/browse/TPS-1319).
	//
	// We can drop this once the automated cleanup is implemented on Konnect side:
	// https://konghq.atlassian.net/browse/TPS-1453.
	rolesToDelete, err := findOrphanedRolesToDelete(ctx, log, sdk.Roles, orphanedCPs, userID)
	if err != nil {
		return fmt.Errorf("failed to list control plane roles to delete: %w", err)
	}
	if err := deleteRoles(ctx, log, sdk.Roles, *me.User.ID, rolesToDelete); err != nil {
		return fmt.Errorf("failed to delete control plane roles: %w", err)
	}

	return nil
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

// findOrphanedControlPlanes finds control planes that were created by the tests and are older than timeUntilControlPlaneOrphaned.
func findOrphanedControlPlanes(
	ctx context.Context,
	log logr.Logger,
	c *sdkkonnectgo.ControlPlanes,
) ([]string, error) {
	// We need to query control planes with two different label filters:
	// 1. Control planes created by integration tests (with `operator-test-id` label)
	// 2. Control planes managed by KO (with `k8s-kind:KonnectGatewayControlPlane` label)
	labelFilters := []string{
		testIDLabel,
		fmt.Sprintf("%s:%s", ops.KubernetesKindLabelKey, k8sKindKonnectGatewayControlPlane),
	}

	seenCPIDs := make(map[string]struct{})
	var orphanedControlPlanes []string

	for _, labelFilter := range labelFilters {
		response, err := c.ListControlPlanes(ctx, sdkkonnectops.ListControlPlanesRequest{
			PageSize:     lo.ToPtr(konnectControlPlanesLimit),
			FilterLabels: lo.ToPtr(labelFilter),
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

// findOrphanedRolesToDelete gets a list of roles that belong to the orphaned control planes.
func findOrphanedRolesToDelete(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.Roles,
	orphanedCPsIDs []string,
	userID string,
) ([]string, error) {
	if len(orphanedCPsIDs) < 1 {
		log.Info("No control planes to clean up, skipping listing roles")
		return nil, nil
	}

	resp, err := sdk.ListUserRoles(ctx, userID,
		// NOTE: Sadly we can't do filtering here (yet?) because ListUserRolesQueryParamFilter
		// can only match by exact name and we match against a list of orphaned control plane IDs.
		&sdkkonnectops.ListUserRolesQueryParamFilter{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list user roles: %w", err)
	}

	if resp == nil || resp.AssignedRoleCollection == nil {
		return nil, errors.New("failed to list user roles, response is nil")
	}

	var rolesIDsToDelete []string
	for _, role := range resp.AssignedRoleCollection.GetData() {
		log.Info("User role", "id", role.ID, "entity_id", role.EntityID)
		belongsToOrphanedControlPlane := lo.ContainsBy(orphanedCPsIDs, func(cpID string) bool {
			if role.EntityID == nil {
				return false
			}
			return cpID == *role.EntityID
		})
		if !belongsToOrphanedControlPlane {
			continue
		}
		rolesIDsToDelete = append(rolesIDsToDelete, *role.ID)
	}

	return rolesIDsToDelete, nil
}

// deleteRoles deletes roles by their IDs.
func deleteRoles(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.Roles,
	userID string,
	rolesIDsToDelete []string,
) error {
	if len(rolesIDsToDelete) == 0 {
		log.Info("No roles to delete")
		return nil
	}

	var errs []error
	for _, roleID := range rolesIDsToDelete {
		log.Info("Deleting role", "id", roleID)
		_, err := sdk.UsersRemoveRole(ctx, userID, roleID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to delete role %s: %w", roleID, err))
		}
	}

	return errors.Join(errs...)
}
