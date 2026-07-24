package eventually

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// AssertMatchingKongPluginBindings lists KongPluginBindings matching the
// provided fields until the supplied assertions pass.
func AssertMatchingKongPluginBindings(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	matchingFields client.MatchingFields,
	waitTime time.Duration,
	tickTime time.Duration,
	assertions func(*assert.CollectT, []configurationv1alpha1.KongPluginBinding),
	msgAndArgs ...any,
) bool {
	t.Helper()

	return assert.EventuallyWithT(t,
		func(c *assert.CollectT) {
			var list configurationv1alpha1.KongPluginBindingList
			if !assert.NoError(c, cl.List(ctx, &list, matchingFields)) {
				return
			}
			assertions(c, list.Items)
		},
		waitTime, tickTime,
		msgAndArgs...,
	)
}
