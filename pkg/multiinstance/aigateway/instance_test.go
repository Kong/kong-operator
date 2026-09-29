package aigateway

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

func adminAPI(address string) adminapi.DiscoveredAdminAPI {
	return adminapi.DiscoveredAdminAPI{
		Address:       address,
		TLSServerName: "pod.dp-admin.default.svc",
		PodRef:        types.NamespacedName{Namespace: "default", Name: "dp"},
	}
}

// waitForNotification returns the next Change from the notifier channel, or nil when
// no notification arrives.
func waitForNotification(ch <-chan changenotifier.Change) *changenotifier.Change {
	select {
	case change := <-ch:
		return &change
	default:
		return nil
	}
}

func TestOnAdminAPIsDiscovered(t *testing.T) {
	ctx := context.Background()
	gatewayNN := types.NamespacedName{Namespace: "default", Name: "gw"}

	instance := NewInstance(
		manager.NewRandomID(),
		logr.Discard(),
		Config{},
		Env{GatewayNN: gatewayNN},
	)
	ch := instance.cn.NotifyChannel()

	oneEndpoint := sets.New(adminAPI("https://10.0.0.1:8444"))
	otherEndpoint := sets.New(adminAPI("https://10.0.0.2:8444"))

	// Discovery from nothing to one endpoint notifies the sync loop.
	instance.onAdminAPIsDiscovered(ctx, oneEndpoint)
	change := waitForNotification(ch)
	require.NotNil(t, change, "expected a change notification for the first discovery")
	require.Equal(t, gatewayNN, *change.ParentNN)

	// Unchanged set stays silent but is stored.
	instance.onAdminAPIsDiscovered(ctx, oneEndpoint)
	require.Nil(t, waitForNotification(ch), "unchanged set must not notify")
	require.True(t, instance.AdminAPIs().Equal(oneEndpoint))

	// Set emptied stays silent but the empty set is stored.
	instance.onAdminAPIsDiscovered(ctx, sets.New[adminapi.DiscoveredAdminAPI]())
	require.Nil(t, waitForNotification(ch), "emptied set must not notify")
	require.Empty(t, instance.AdminAPIs())

	// Refilled set notifies again.
	instance.onAdminAPIsDiscovered(ctx, otherEndpoint)
	change = waitForNotification(ch)
	require.NotNil(t, change, "expected a change notification for the refilled set")
	require.Equal(t, gatewayNN, *change.ParentNN)
	require.True(t, instance.AdminAPIs().Equal(otherEndpoint))
}
