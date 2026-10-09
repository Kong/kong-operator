package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/go-logr/logr"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
)

const (
	konnectAIGatewaysLimit = int64(100)

	k8sKindKonnectAIGateway = "KonnectAIGateway"
)

// cleanupKonnectAIGateways deletes orphaned AI Gateways created by the tests.
func cleanupKonnectAIGateways(sdk *sdkkonnectgo.SDK) func(ctx context.Context, log logr.Logger) error {
	return func(ctx context.Context, log logr.Logger) error {
		me, err := sdk.Me.GetUsersMe(ctx)
		if err != nil {
			return fmt.Errorf("failed to get user info: %w", err)
		}
		if me.User == nil || me.User.ID == nil {
			return errors.New("failed to get user info, user is nil")
		}

		orphanedAIGateways, err := findOrphanedAIGateways(ctx, log, sdk.AIGateways)
		if err != nil {
			return fmt.Errorf("failed to find orphaned AI Gateways: %w", err)
		}
		if err := deleteAIGateways(ctx, log, sdk.AIGateways, orphanedAIGateways); err != nil {
			return fmt.Errorf("failed to delete AI Gateways: %w", err)
		}

		userID := *me.User.ID

		// We do this but in the future this should be done automatically.
		// Ref thread: https://kongstrong.slack.com/archives/CQK8J4VN3/p1791535821979929
		if err := removeOrphanedAIGatewayRoles(ctx, log, sdk, userID); err != nil {
			return fmt.Errorf("failed to remove AI gateway roles: %w", err)
		}

		log.Info("AI gateway cleanup completed")
		return nil
	}
}

// findOrphanedAIGateways finds AI Gateways that were created by Kong Operator (e.g. from e2e tests)
// and are older than timeUntilControlPlaneOrphaned.
func findOrphanedAIGateways(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.AIGateways,
) ([]string, error) {
	seenAIGatewayIDs := make(map[string]struct{})
	var orphanedAIGateways []string

	response, err := sdk.ListAiGateways(ctx, new(konnectAIGatewaysLimit), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list AI Gateways: %w", err)
	}
	if response.ListAIGatewaysResponse == nil {
		body, err := io.ReadAll(response.RawResponse.Body)
		response.RawResponse.Body.Close()
		if err != nil {
			body = []byte(err.Error())
		}
		return nil, fmt.Errorf("failed to list AI Gateways, status: %d, body: %s", response.GetStatusCode(), body)
	}

	for _, aiGateway := range response.ListAIGatewaysResponse.Data {
		// Skip if we've already processed this AI Gateway.
		if _, seen := seenAIGatewayIDs[aiGateway.ID]; seen {
			continue
		}
		seenAIGatewayIDs[aiGateway.ID] = struct{}{}

		if aiGateway.Labels[ops.KubernetesKindLabelKey] != k8sKindKonnectAIGateway {
			log.Info("AIGateway not managed by Kong Operator, skipping", "name", aiGateway.Name)
			continue
		}
		if aiGateway.Labels["test"] == "" {
			log.Info("AIGateway has no test label, skipping", "name", aiGateway.Name)
			continue
		}

		if aiGateway.CreatedAt.IsZero() {
			log.Info("AIGateway has no creation timestamp, skipping", "name", aiGateway.Name)
			continue
		}
		orphanedTime := aiGateway.CreatedAt.Add(timeUntilControlPlaneOrphaned)
		if orphanedTime.After(time.Now()) {
			log.Info("AIGateway is not old enough to be considered orphaned, skipping",
				"name", aiGateway.Name, "id", aiGateway.ID, "created_at", aiGateway.CreatedAt,
			)
			continue
		}
		orphanedAIGateways = append(orphanedAIGateways, aiGateway.ID)
	}

	log.Info("Checked AI Gateways for orphans", "count", len(seenAIGatewayIDs), "orphaned", len(orphanedAIGateways))

	return orphanedAIGateways, nil
}

// listAIGatewayIDsPaged lists the IDs of all AI Gateways, paging through the results.
func listAIGatewayIDsPaged(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.AIGateways,
) ([]string, error) {
	var ids []string
	for pageNumber := int64(1); ; pageNumber++ {
		if pageNumber > konnectUserRolesMaxPages {
			return nil, fmt.Errorf("failed to list AI Gateways: exceeded %d pages", konnectUserRolesMaxPages)
		}
		response, err := sdk.ListAiGateways(ctx, new(konnectAIGatewaysLimit), new(pageNumber))
		if err != nil {
			return nil, fmt.Errorf("failed to list AI Gateways (page %d): %w", pageNumber, err)
		}
		if response.ListAIGatewaysResponse == nil {
			body, err := io.ReadAll(response.RawResponse.Body)
			response.RawResponse.Body.Close()
			if err != nil {
				body = []byte(err.Error())
			}
			return nil, fmt.Errorf("failed to list AI Gateways, status: %d, body: %s", response.GetStatusCode(), body)
		}

		gateways := response.ListAIGatewaysResponse.Data
		if len(gateways) == 0 {
			break
		}
		for _, aiGateway := range gateways {
			ids = append(ids, aiGateway.ID)
		}
		log.Info("Fetched AI Gateways page", "page", pageNumber, "count", len(gateways), "total", len(ids))
		if int64(len(gateways)) < konnectAIGatewaysLimit {
			break
		}
	}
	return ids, nil
}

// deleteAIGateways deletes AI Gateways by their IDs.
func deleteAIGateways(
	ctx context.Context,
	log logr.Logger,
	sdk *sdkkonnectgo.AIGateways,
	aiGatewayIDs []string,
) error {
	if len(aiGatewayIDs) < 1 {
		log.Info("No AI Gateways to clean up")
		return nil
	}

	var errs []error
	for _, id := range aiGatewayIDs {
		log.Info("Deleting AI Gateway", "ID", id)
		if _, err := sdk.DeleteAiGateway(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete AI Gateway %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
