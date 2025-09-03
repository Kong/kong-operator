package envtest

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/apis/configuration/v1"
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

	wConsumer := setupWatch[configurationv1.KongConsumerList](t, ctx, cl)
	require.NoError(t, cl.Create(ctx, consumer))
	watchFor(t, ctx, wConsumer, apiwatch.Added,
		objectMatchesName(consumer),
		"error",
	)
}
