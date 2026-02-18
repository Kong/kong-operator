package kongintegration

import (
	"testing"
	"time"

	"github.com/blang/semver/v4"
	"github.com/go-logr/zapr"
	"github.com/kong/go-database-reconciler/pkg/dump"
	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/kong/go-kong/kong"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/network"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/kongintegration/containers"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testenv"
)

func TestUpdateStrategyDBMode(t *testing.T) {
	t.Parallel()

	const (
		timeout = time.Second * 5
		period  = time.Millisecond * 200
	)
	ctx := t.Context()

	// Create a network for Postgres and Kong containers to communicate over.
	net, err := network.New(ctx)
	require.NoError(t, err)

	_ = containers.NewPostgres(ctx, t, net)
	kongC := containers.NewKong(ctx, t, containers.KongWithDBMode(net.Name))

	kongClient, err := adminapi.NewKongAPIClient(kongC.AdminURL(ctx, t), managercfg.AdminAPIClientConfig{}, "")
	require.NoError(t, err)

	gatewayTag, err := testenv.GetDependencyVersion("kongintegration.kong-ee")
	require.NoError(t, err)
	gatewayTag = trimEnterpriseTagToSemver(gatewayTag)

	logbase, err := zap.NewDevelopment()
	require.NoError(t, err)
	logger := zapr.NewLogger(logbase)
	sut := sendconfig.NewUpdateStrategyDBMode(
		kongClient,
		dump.Config{},
		semver.MustParse(gatewayTag),
		concurrency,
		logger,
	)

	faultyConfig := sendconfig.ContentWithHash{
		Content: &file.Content{
			FormatVersion: "3.0",
			Services: []file.FService{
				{
					Service: kong.Service{
						Name:     new("test-service"),
						Host:     new("konghq.com"),
						Port:     new(80),
						Protocol: new("grpc"),
						// Paths are not supported for gRPC services. This will trigger an error.
						Path: new("/test"),
						Tags: []*string{
							// Tags are used to identify the resource in the flattened errors response.
							new("k8s-name:test-service"),
							new("k8s-namespace:default"),
							new("k8s-kind:Service"),
							new("k8s-uid:a3b8afcc-9f19-42e4-aa8f-5866168c2ad3"),
							new("k8s-group:"),
							new("k8s-version:v1"),
						},
					},
				},
			},
		},
	}

	const expectedMessage = `invalid service:test-service: HTTP status 400 (message: "2 schema violations (failed conditional validation given value of field 'protocol'; path: value must be null)")`
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		configSize, err := sut.Update(ctx, faultyConfig)
		if !assert.Error(t, err) {
			return
		}
		// Default value 0 to discard, since error has been returned.
		if !assert.Zero(t, configSize) {
			return
		}
		var updateError sendconfig.UpdateError
		if !assert.ErrorAs(t, err, &updateError) {
			return
		}
		if !assert.NotEmpty(t, updateError.ResourceFailures()) {
			return
		}
		resourceErr, found := lo.Find(updateError.ResourceFailures(), func(r failures.ResourceFailure) bool {
			return lo.ContainsBy(r.CausingObjects(), func(obj client.Object) bool {
				return obj.GetName() == "test-service"
			})
		})
		if !assert.Truef(t, found, "expected resource error for test-service, got: %+v", updateError.ResourceFailures()) {
			return
		}
		if !assert.Equal(t, expectedMessage, resourceErr.Message()) {
			return
		}
	}, timeout, period)
	require.NoError(t, err)
}
