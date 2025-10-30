package helpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestWatch(t *testing.T) {
	var (
		ctx = t.Context()
		cl  = fakectrlruntimeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			Build()
		consumer = &configurationv1.KongConsumer{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-consumer",
			},
		}
	)

	wConsumer, err := cl.Watch(ctx, &configurationv1.KongConsumerList{})
	require.NoError(t, err)
	require.NoError(t, cl.Create(ctx, consumer))
	WatchFor(t, ctx, wConsumer, apiwatch.Added,
		time.Second,
		func(c *configurationv1.KongConsumer) bool {
			return c.Name == consumer.Name
		},
		"error",
	)
}
