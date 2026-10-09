package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/test"
)

// konnectAIGatewayEntityTypeName is the entity_type_name of AI Gateway roles
// as returned by the list user roles endpoint.
const konnectAIGatewayEntityTypeName = "AI Gateways"

// konnectControlPlaneEntityTypeName is the entity_type_name of control plane roles
// as returned by the list user roles endpoint.
const konnectControlPlaneEntityTypeName = "Control Planes"

// userRolesRequestURL builds the URL for listing the roles assigned to userID,
// paged with page[size]/page[number].
func userRolesRequestURL(baseURL, userID string, pageNumber int) (*url.URL, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base URL %s: %w", baseURL, err)
	}
	u = u.JoinPath("v3", "users", userID, "assigned-roles")
	q := url.Values{}
	q.Set("page[size]", strconv.Itoa(konnectUserRolesPageSize))
	q.Set("page[number]", strconv.Itoa(pageNumber))
	u.RawQuery = q.Encode()
	return u, nil
}

// listUserRolesPaged lists all the roles assigned to userID. The first page is
// fetched to learn the total number of roles from the response metadata, then
// the remaining pages are fetched in parallel (at most cleanupConcurrency()
// pages in flight).
//
// NOTE: No role must be removed while the listing is in progress. The endpoint
// paginates by position over the live collection, so removing roles shifts the
// remaining ones into already fetched pages and they get skipped.
// If some pages fail to be fetched (after retries), the successfully fetched
// roles are returned along with the joined errors - the callers are expected to
// process the returned roles anyway and surface the error.
func listUserRolesPaged(
	ctx context.Context,
	log logr.Logger,
	baseURL, token, userID string,
) ([]sdkkonnectcomp.AssignedRole, error) {
	client := &http.Client{}

	log.Info("Listing user roles", "user_id", userID)
	firstPage, total, err := fetchUserRolesPage(ctx, log, client, baseURL, token, userID, 1)
	if err != nil {
		return nil, err
	}
	// Defensive fallback in case the response metadata doesn't provide the total.
	if total == 0 {
		total = float64(len(firstPage))
	}
	totalPages := (int(total) + konnectUserRolesPageSize - 1) / konnectUserRolesPageSize
	totalPages = max(totalPages, 1)

	// fetchedPages counts how many pages have been fetched so far (page 1 is
	// already fetched), so the logs show the progress as n/MAX.
	var (
		fetchedPages atomic.Int64
		errsMu       sync.Mutex
		listErrs     []error
	)
	fetchedPages.Store(1)
	log.Info("Fetched user roles page",
		"progress", fmt.Sprintf("1/%d", totalPages),
		"count", len(firstPage),
		"total_roles", int(total),
	)

	pageNumbers := make([]int, 0, totalPages-1)
	for pageNumber := 2; pageNumber <= totalPages; pageNumber++ {
		pageNumbers = append(pageNumbers, pageNumber)
	}
	otherPages := make([][]sdkkonnectcomp.AssignedRole, len(pageNumbers))
	if err := forEachLimited(pageNumbers, cleanupConcurrency(), func(pageNumber int) error {
		pageRoles, _, err := fetchUserRolesPage(ctx, log, client, baseURL, token, userID, pageNumber)
		// A failed page must not abort the listing: record the error and keep
		// going so that the successfully fetched roles can still be processed.
		if err != nil {
			errsMu.Lock()
			listErrs = append(listErrs, err)
			errsMu.Unlock()
			return nil
		}
		// Each goroutine writes to a distinct index, no lock needed.
		otherPages[pageNumber-2] = pageRoles
		log.Info("Fetched user roles page",
			"progress", fmt.Sprintf("%d/%d", fetchedPages.Add(1), totalPages),
			"count", len(pageRoles),
		)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list user roles: %w", err)
	}

	roles := firstPage
	for _, pageRoles := range otherPages {
		roles = append(roles, pageRoles...)
	}
	log.Info("Listed user roles", "user_id", userID, "count", len(roles), "pages", totalPages)

	return roles, errors.Join(listErrs...)
}

// filterRoleIDs returns the IDs of the roles for which match returns true.
// Roles with no ID are skipped.
func filterRoleIDs(
	log logr.Logger,
	roles []sdkkonnectcomp.AssignedRole,
	match func(role sdkkonnectcomp.AssignedRole) bool,
) []string {
	var roleIDs []string
	for _, role := range roles {
		if role.ID == nil || !match(role) {
			continue
		}
		log.Info("Found orphaned role", "id", role.ID, "entity_id", role.EntityID, "entity_type_name", role.EntityTypeName)
		roleIDs = append(roleIDs, *role.ID)
	}
	return roleIDs
}

// removeOrphanedControlPlaneRoles lists all the roles assigned to userID, then
// removes those that reference control planes which no longer exist, or that
// were deleted during this run (the Konnect listing may lag behind the deletions).
func removeOrphanedControlPlaneRoles(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.Roles,
	userID string,
	existingCPsIDs []string,
	deletedCPsIDs []string,
) error {
	existingCPs := make(map[string]struct{}, len(existingCPsIDs))
	for _, cpID := range existingCPsIDs {
		existingCPs[cpID] = struct{}{}
	}
	deletedCPs := make(map[string]struct{}, len(deletedCPsIDs))
	for _, cpID := range deletedCPsIDs {
		deletedCPs[cpID] = struct{}{}
	}

	baseURL, err := canonicalizedServerURL()
	if err != nil {
		return fmt.Errorf("failed to get Konnect server URL: %w", err)
	}
	token := test.KonnectAccessToken()

	roles, listErr := listUserRolesPaged(ctx, log, baseURL, token, userID)
	roleIDsToRemove := filterRoleIDs(log, roles, func(role sdkkonnectcomp.AssignedRole) bool {
		if role.EntityTypeName == nil || *role.EntityTypeName != konnectControlPlaneEntityTypeName {
			return false
		}
		if role.EntityID == nil {
			return true
		}
		if _, deleted := deletedCPs[*role.EntityID]; deleted {
			return true
		}
		_, exists := existingCPs[*role.EntityID]
		return !exists
	})

	removed, removeErr := removeRoles(ctx, log, sdk, userID, roleIDsToRemove)
	log.Info("Removed orphaned control plane roles", "count", removed, "to_remove", len(roleIDsToRemove))

	return errors.Join(listErr, removeErr)
}

// removeOrphanedAIGatewayRoles lists all the roles assigned to userID, then
// removes those that reference AI Gateways which no longer exist.
func removeOrphanedAIGatewayRoles(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.Roles,
	userID string,
	existingAIGatewayIDs []string,
) error {
	existingAIGateways := make(map[string]struct{}, len(existingAIGatewayIDs))
	for _, id := range existingAIGatewayIDs {
		existingAIGateways[id] = struct{}{}
	}

	baseURL, err := canonicalizedServerURL()
	if err != nil {
		return fmt.Errorf("failed to get Konnect server URL: %w", err)
	}
	token := test.KonnectAccessToken()

	roles, listErr := listUserRolesPaged(ctx, log, baseURL, token, userID)
	roleIDsToRemove := filterRoleIDs(log, roles, func(role sdkkonnectcomp.AssignedRole) bool {
		if role.EntityTypeName == nil || *role.EntityTypeName != konnectAIGatewayEntityTypeName {
			return false
		}
		if role.EntityID == nil {
			return true
		}
		_, exists := existingAIGateways[*role.EntityID]
		return !exists
	})

	removed, removeErr := removeRoles(ctx, log, sdk, userID, roleIDsToRemove)
	log.Info("Removed orphaned AI Gateway roles", "count", removed, "to_remove", len(roleIDsToRemove))

	return errors.Join(listErr, removeErr)
}

// removeRoles removes the given roles of userID, with at most cleanupConcurrency()
// removals running in parallel. Roles that no longer exist (404) are skipped.
// It returns the number of removed roles and all errors joined.
func removeRoles(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.Roles,
	userID string,
	rolesIDsToRemove []string,
) (int, error) {
	if len(rolesIDsToRemove) == 0 {
		return 0, nil
	}

	var (
		removed atomic.Int64
		errsMu  sync.Mutex
		errs    []error
	)
	if err := forEachLimited(rolesIDsToRemove, cleanupConcurrency(), func(roleID string) error {
		log.Info("Removing role", "id", roleID)
		_, err := sdk.UsersRemoveRole(ctx, userID, roleID)
		if err != nil {
			if _, ok := errors.AsType[*sdkkonnecterrs.NotFoundError](err); ok {
				return nil
			}
			errsMu.Lock()
			errs = append(errs, fmt.Errorf("failed to delete role %s: %w", roleID, err))
			errsMu.Unlock()
			return nil
		}
		removed.Add(1)
		return nil
	}); err != nil {
		return int(removed.Load()), err
	}

	return int(removed.Load()), errors.Join(errs...)
}

const (
	// userRolesRetryMaxAttempts is the maximum number of attempts for a single
	// user roles page fetch. Konnect sometimes answers 5xx for individual pages
	// when there's many roles, so the fetches are retried.
	userRolesRetryMaxAttempts = 5
)

// userRolesRetryBaseDelay is the base delay for the exponential backoff
// between user roles page fetch retries (1s, 2s, 4s, 8s).
// It's a variable so that tests can shorten the retry delays.
var userRolesRetryBaseDelay = time.Second

// fetchUserRolesPage fetches a single page of the roles assigned to userID.
// It returns the roles from that page and the total number of roles as reported
// by the response metadata (0 if not provided).
// Requests failing with 4xx (except 429) errors are not retried, 429 and 5xx
// responses and transport errors are retried with exponential backoff.
func fetchUserRolesPage(
	ctx context.Context,
	log logr.Logger,
	client *http.Client,
	baseURL, token, userID string,
	pageNumber int,
) ([]sdkkonnectcomp.AssignedRole, float64, error) {
	u, err := userRolesRequestURL(baseURL, userID, pageNumber)
	if err != nil {
		return nil, 0, err
	}

	var lastErr error
	for attempt := 1; attempt <= userRolesRetryMaxAttempts; attempt++ {
		if attempt > 1 {
			delay := userRolesRetryBaseDelay << (attempt - 2)
			log.Info("Retrying user roles page fetch",
				"page", pageNumber, "attempt", attempt, "delay", delay.String(), "error", lastErr)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to list user roles (page %d): %w", pageNumber, err)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read user roles response body (page %d): %w", pageNumber, err)
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("failed to list user roles (page %d), status: %d, body: %s",
				pageNumber, resp.StatusCode, body)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("failed to list user roles (page %d), status: %d, body: %s",
				pageNumber, resp.StatusCode, body)
		}

		var collection sdkkonnectcomp.AssignedRoleCollection
		if err := json.Unmarshal(body, &collection); err != nil {
			return nil, 0, fmt.Errorf("failed to unmarshal user roles response (page %d): %w", pageNumber, err)
		}

		return collection.GetData(), collection.GetMeta().GetPage().Total, nil
	}

	return nil, 0, fmt.Errorf("%w (after %d attempts)", lastErr, userRolesRetryMaxAttempts)
}
