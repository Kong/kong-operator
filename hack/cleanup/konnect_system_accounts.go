package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/test"
)

const (
	systemAccountsPageSize         = int64(100)
	timeUntilSystemAccountOrphaned = time.Hour
)

// cleanupKonnectSystemAccounts deletes orphaned system accounts created by the tests.
func cleanupKonnectSystemAccounts(ctx context.Context, log logr.Logger) error {
	serverURL, err := canonicalizedServerURL()
	if err != nil {
		return fmt.Errorf("invalid server URL %s: %w", test.KonnectServerURL(), err)
	}

	sdk := sdkkonnectgo.New(
		sdkkonnectgo.WithSecurity(
			sdkkonnectcomp.Security{
				PersonalAccessToken: new(test.KonnectAccessToken()),
			},
		),
		sdkkonnectgo.WithServerURL(serverURL),
	)

	orphanedSAs, err := findOrphanedSystemAccounts(ctx, log, sdk.SystemAccounts)
	if err != nil {
		return fmt.Errorf("failed to find orphaned system accounts: %w", err)
	}
	if err := deleteSystemAccounts(ctx, log, sdk.SystemAccounts, orphanedSAs); err != nil {
		return fmt.Errorf("failed to delete system accounts: %w", err)
	}

	return nil
}

// findOrphanedSystemAccounts finds system accounts that were created more than timeUntilSystemAccountOrphaned ago.
func findOrphanedSystemAccounts(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.SystemAccounts,
) ([]string, error) {
	var orphanedSystemAccounts []string
	pageNumber := int64(1)

	for {
		response, err := sdk.GetSystemAccounts(ctx, sdkkonnectops.GetSystemAccountsRequest{
			PageSize:   new(systemAccountsPageSize),
			PageNumber: &pageNumber,
			Filter: &sdkkonnectops.GetSystemAccountsQueryParamFilter{
				// Only consider non-Konnect-managed system accounts.
				KonnectManaged: new(false),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list system accounts (page %d): %w", pageNumber, err)
		}
		if response.SystemAccountCollection == nil {
			return nil, fmt.Errorf("failed to list system accounts, response is nil (page %d)", pageNumber)
		}

		accounts := response.SystemAccountCollection.GetData()
		if len(accounts) == 0 {
			break
		}

		for _, sa := range accounts {
			if sa.ID == nil {
				log.Info("System account has no ID, skipping", "name", sa.GetName())
				continue
			}
			if sa.CreatedAt == nil {
				log.Info("System account has no creation timestamp, skipping", "id", *sa.ID, "name", sa.GetName())
				continue
			}
			orphanedAfter := sa.CreatedAt.Add(timeUntilSystemAccountOrphaned)
			if !time.Now().After(orphanedAfter) {
				log.Info("System account is not old enough to be considered orphaned, skipping",
					"id", *sa.ID, "name", sa.GetName(), "created_at", *sa.CreatedAt,
				)
				continue
			}
			orphanedSystemAccounts = append(orphanedSystemAccounts, *sa.ID)
		}

		if int64(len(accounts)) < systemAccountsPageSize {
			break
		}
		pageNumber++
	}

	return orphanedSystemAccounts, nil
}

// deleteSystemAccounts deletes system accounts by their IDs.
func deleteSystemAccounts(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.SystemAccounts,
	saIDs []string,
) error {
	if len(saIDs) == 0 {
		log.Info("No system accounts to clean up")
		return nil
	}

	var errs []error
	for _, saID := range saIDs {
		log.Info("Deleting system account", "id", saID)
		if _, err := sdk.DeleteSystemAccountsID(ctx, saID); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete system account %s: %w", saID, err))
		}
	}
	return errors.Join(errs...)
}
