package sendconfig_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/zapr"
	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/diagnostics"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/metrics"
	"github.com/kong/kong-operator/v2/ingress-controller/test/mocks"
)

type updateStrategyResolver struct {
	strategy sendconfig.UpdateStrategy
}

func (r updateStrategyResolver) ResolveUpdateStrategy(
	sendconfig.UpdateClient,
	*diagnostics.Client,
) sendconfig.UpdateStrategy {
	return r.strategy
}

type successfulUpdateStrategy struct{}

func (successfulUpdateStrategy) Update(context.Context, sendconfig.ContentWithHash) (mo.Option[int], error) {
	return mo.Some(1), nil
}

func (successfulUpdateStrategy) MetricsProtocol() metrics.Protocol {
	return metrics.ProtocolDBLess
}

func (successfulUpdateStrategy) Type() string {
	return "successful"
}

func TestPerformUpdateRecordsReportedDBLessConfigurationHash(t *testing.T) {
	ctx := t.Context()
	logger := zapr.NewLogger(zap.NewNop())

	const reportedHash = "canonical-configuration-hash"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/status", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"configuration_hash":%q}`, reportedHash)
	}))
	t.Cleanup(server.Close)

	kongClient, err := kong.NewClient(new(server.URL), server.Client())
	require.NoError(t, err)
	client := adminapi.NewClient(kongClient)

	var (
		lastDBLessConfigHash    string
		compareDBLessConfigHash bool
	)
	detector := mocks.ConfigurationChangeDetector{
		ConfigurationChanged:    true,
		LastDBLessConfigHash:    &lastDBLessConfigHash,
		CompareDBLessConfigHash: &compareDBLessConfigHash,
	}

	_, err = sendconfig.PerformUpdate(
		ctx,
		logger,
		client,
		sendconfig.Config{InMemory: true},
		&file.Content{FormatVersion: "3.0"},
		nil,
		mocks.MetricsRecorder{},
		updateStrategyResolver{strategy: successfulUpdateStrategy{}},
		detector,
		nil,
		false,
	)
	require.NoError(t, err)

	require.True(t, compareDBLessConfigHash)
	require.Empty(t, lastDBLessConfigHash)
	require.Equal(t, reportedHash, client.LastDBLessConfigHash())
}
