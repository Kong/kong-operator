package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestConfigVersionTrackerKeepsWorkspace checks that both the upsert and the GET
// that verifies a periodic update target the entity's Konnect Workspace. Without
// it the GET would read the default Workspace and never find the entity.
func TestConfigVersionTrackerKeepsWorkspace(t *testing.T) {
	const workspace = "team-a"

	var stored *sdkkonnectcomp.Service
	sdk := mocks.NewMockServicesSDK(t)
	sdk.EXPECT().UpsertService(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertServiceRequest) bool {
		return req.Workspace == workspace
	})).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpsertServiceRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpsertServiceResponse, error) {
			stored = &sdkkonnectcomp.Service{
				ID:   new(req.ServiceID),
				Name: req.ServiceRequest.ServiceWithHost.Name,
				Host: req.ServiceRequest.ServiceWithHost.Host,
			}
			return &sdkkonnectops.UpsertServiceResponse{Service: new(*stored)}, nil
		},
	).Once()
	sdk.EXPECT().GetService(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.GetServiceRequest) bool {
		return req.Workspace == workspace
	})).RunAndReturn(
		func(context.Context, sdkkonnectops.GetServiceRequest, ...sdkkonnectops.Option) (*sdkkonnectops.GetServiceResponse, error) {
			return &sdkkonnectops.GetServiceResponse{Service: new(*stored)}, nil
		},
	).Once()

	version := newFakeConfigVersion(t)
	svc := newTestKongService()
	svc.SetWorkspace(workspace)

	tracker, _ := newTestConfigVersionTracker()
	g := tracker.guard(version.sdk, svc.GetUID(), testCPID, true, nil)
	require.NoError(t, updateService(t.Context(), sdk, g, svc))
	require.Equal(t, syncOutcomeSent, g.outcome)
	svc.SetSyncStatus(g.pendingStatus())

	// After a restart the persisted hashes are unverified, so the periodic update
	// is checked with a GET, which must target the same Workspace.
	restarted, _ := newTestConfigVersionTracker()
	g = restarted.guard(version.sdk, svc.GetUID(), testCPID, true, svc.GetSyncStatus())
	require.NoError(t, updateService(t.Context(), sdk, g, svc))
	assert.Equal(t, syncOutcomeVerified, g.outcome)
}
