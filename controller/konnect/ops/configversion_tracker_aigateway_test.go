package ops

import (
	"context"
	"slices"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

const testAIGatewayID = "aigw-1"

// fakeAIGatewayVersion serves the expected config version of an AI Gateway.
type fakeAIGatewayVersion struct {
	sdk     *mocks.MockAIGatewayNodesSDK
	version string
	calls   int
}

func newFakeAIGatewayVersion(t *testing.T) *fakeAIGatewayVersion {
	f := &fakeAIGatewayVersion{sdk: mocks.NewMockAIGatewayNodesSDK(t), version: "1"}
	f.sdk.EXPECT().GetAiGatewayExpectedConfigVersion(mock.Anything, testAIGatewayID).RunAndReturn(
		func(context.Context, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetAiGatewayExpectedConfigVersionResponse, error) {
			f.calls++
			return &sdkkonnectops.GetAiGatewayExpectedConfigVersionResponse{
				AIGatewayExpectedConfigVersion: &sdkkonnectcomp.AIGatewayExpectedConfigVersion{
					ExpectedConfigVersion: f.version,
				},
			}, nil
		},
	).Maybe()
	return f
}

// fakeAIGatewayConsumers stores the consumer and its consumer group membership
// like Konnect does, and counts the requests.
type fakeAIGatewayConsumers struct {
	sdk        *mocks.MockAIGatewayConsumersSDK
	stored     *sdkkonnectcomp.AIGatewayConsumer
	groups     []string
	updatedAt  time.Time
	gets       int
	updates    int
	groupLists int
	groupPuts  int
}

func newFakeAIGatewayConsumers(t *testing.T) *fakeAIGatewayConsumers {
	f := &fakeAIGatewayConsumers{sdk: mocks.NewMockAIGatewayConsumersSDK(t), updatedAt: time.Unix(1_700_000_000, 0)}
	f.sdk.EXPECT().UpdateAiGatewayConsumer(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpdateAiGatewayConsumerRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateAiGatewayConsumerResponse, error) {
			f.updates++
			// updated_at changes on every write and must not affect the representation hash.
			f.updatedAt = f.updatedAt.Add(time.Second)
			body := req.UpdateAIGatewayConsumerRequest
			f.stored = &sdkkonnectcomp.AIGatewayConsumer{
				ID:          req.ConsumerIDOrName,
				Name:        body.Name,
				DisplayName: body.DisplayName,
				Type:        sdkkonnectcomp.AIGatewayConsumerType(body.Type),
				CustomID:    body.CustomID,
				Labels:      body.Labels,
				UpdatedAt:   f.updatedAt,
			}
			return &sdkkonnectops.UpdateAiGatewayConsumerResponse{AIGatewayConsumer: new(*f.stored)}, nil
		},
	).Maybe()
	f.sdk.EXPECT().GetAiGatewayConsumer(mock.Anything, testAIGatewayID, mock.Anything).RunAndReturn(
		func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.GetAiGatewayConsumerResponse, error) {
			f.gets++
			return &sdkkonnectops.GetAiGatewayConsumerResponse{AIGatewayConsumer: new(*f.stored)}, nil
		},
	).Maybe()
	f.sdk.EXPECT().UpdateAiGatewayConsumerGroupsForConsumer(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, req sdkkonnectops.UpdateAiGatewayConsumerGroupsForConsumerRequest, _ ...sdkkonnectops.Option) (*sdkkonnectops.UpdateAiGatewayConsumerGroupsForConsumerResponse, error) {
			f.groupPuts++
			f.groups = slices.Clone(req.RequestBody.ConsumerGroups)
			return &sdkkonnectops.UpdateAiGatewayConsumerGroupsForConsumerResponse{}, nil
		},
	).Maybe()
	f.sdk.EXPECT().ListAiGatewayConsumerGroupsForConsumer(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, sdkkonnectops.ListAiGatewayConsumerGroupsForConsumerRequest, ...sdkkonnectops.Option) (*sdkkonnectops.ListAiGatewayConsumerGroupsForConsumerResponse, error) {
			f.groupLists++
			data := make([]sdkkonnectcomp.AIGatewayConsumerGroup, 0, len(f.groups))
			for _, name := range f.groups {
				data = append(data, sdkkonnectcomp.AIGatewayConsumerGroup{Name: name})
			}
			return &sdkkonnectops.ListAiGatewayConsumerGroupsForConsumerResponse{
				ListAIGatewayConsumerGroupsResponse: &sdkkonnectcomp.ListAIGatewayConsumerGroupsResponse{Data: data},
			}, nil
		},
	).Maybe()
	return f
}

type aiGatewayConsumerCalls struct {
	gets, updates, groupLists, groupPuts int
}

func (f *fakeAIGatewayConsumers) calls() aiGatewayConsumerCalls {
	c := aiGatewayConsumerCalls{gets: f.gets, updates: f.updates, groupLists: f.groupLists, groupPuts: f.groupPuts}
	f.gets, f.updates, f.groupLists, f.groupPuts = 0, 0, 0, 0
	return c
}

func newTestAIGatewayConsumerGroup(name, konnectName string) *aiconfigurationv1alpha1.AIGatewayConsumerGroup {
	group := &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
		Name:      name,
		Namespace: "default",
	}
	group.Spec.APISpec.Name = aiconfigurationv1alpha1.AIGatewayEntityIdentifier(konnectName)
	group.SetKonnectID(name + "-konnect-id")
	return group
}

// aiGatewayConsumerSync runs guarded updates of an AIGatewayConsumer and, like
// the reconciler, commits the pending record after each update.
type aiGatewayConsumerSync struct {
	t         *testing.T
	tracker   *ConfigVersionTracker
	clock     *testClock
	version   *fakeAIGatewayVersion
	consumers *fakeAIGatewayConsumers
	cl        client.Client
	consumer  *aiconfigurationv1alpha1.AIGatewayConsumer
}

func newAIGatewayConsumerSync(t *testing.T) *aiGatewayConsumerSync {
	tracker, clock := newTestConfigVersionTracker()
	consumer := testGeneratedAIGatewayConsumerForSDKOps()
	consumer.SetGatewayID(testAIGatewayID)
	consumer.SetKonnectID("consumer-konnect-id")
	consumer.Spec.ConsumerGroups = []aiconfigurationv1alpha1.AIGatewayConsumerGroupRef{{Name: "group-b"}, {Name: "group-a"}}
	cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(
		newTestAIGatewayConsumerGroup("group-a", "konnect-group-a"),
		newTestAIGatewayConsumerGroup("group-b", "konnect-group-b"),
	).Build()
	return &aiGatewayConsumerSync{
		t:         t,
		tracker:   tracker,
		clock:     clock,
		version:   newFakeAIGatewayVersion(t),
		consumers: newFakeAIGatewayConsumers(t),
		cl:        cl,
		consumer:  consumer,
	}
}

func (s *aiGatewayConsumerSync) update(periodic bool) (*syncGuard, aiGatewayConsumerCalls) {
	s.t.Helper()
	g := s.tracker.aiGatewayGuard(s.version.sdk, s.consumer.GetUID(), testAIGatewayID, periodic)
	require.NoError(s.t, updateAIGatewayConsumer(s.t.Context(), s.cl, s.consumers.sdk, g, s.consumer))
	if rec, ok := g.pendingRecord(); ok {
		s.tracker.store(s.consumer.GetUID(), rec)
	}
	return g, s.consumers.calls()
}

func TestAIGatewayConsumerGuardedUpdate(t *testing.T) {
	t.Run("first update sends the consumer and its membership", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{updates: 1, groupPuts: 1}, calls)
		assert.Equal(t, []string{"konnect-group-b", "konnect-group-a"}, s.consumers.groups)
	})

	t.Run("unchanged periodic update is skipped without any request but the version read", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		versionReads := s.version.calls

		s.clock.Advance(testVersionTTL)
		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSkipped, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{}, calls)
		assert.Equal(t, versionReads+1, s.version.calls, "the version is read once per TTL")

		g, calls = s.update(true)
		assert.Equal(t, syncOutcomeSkipped, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{}, calls)
		assert.Equal(t, versionReads+1, s.version.calls, "the version is not read again within the TTL")
	})

	t.Run("non periodic update always sends", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		g, calls := s.update(false)
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{updates: 1, groupPuts: 1}, calls)
	})

	t.Run("external change of nothing is verified without writes", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		s.version.version = "2"
		s.clock.Advance(testVersionTTL)

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{gets: 1, groupLists: 1}, calls)
	})

	t.Run("external change of the consumer sends it again", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		s.consumers.stored.DisplayName = "changed in Konnect"
		s.version.version = "2"
		s.clock.Advance(testVersionTTL)

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{gets: 1, updates: 1, groupPuts: 1}, calls)
		assert.Equal(t, "test-value", s.consumers.stored.DisplayName)
	})

	t.Run("external change of the membership is reverted without sending the consumer", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		s.consumers.groups = []string{"konnect-group-a"}
		s.version.version = "2"
		s.clock.Advance(testVersionTTL)

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{gets: 1, groupLists: 1, groupPuts: 1}, calls)
		assert.ElementsMatch(t, []string{"konnect-group-a", "konnect-group-b"}, s.consumers.groups)
	})

	t.Run("drift check verifies the membership without a version change", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)
		s.consumers.groups = nil
		s.clock.Advance(testDriftCheckPeriod + testDriftCheckPeriod/10)

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeVerified, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{gets: 1, groupLists: 1, groupPuts: 1}, calls)
	})

	t.Run("a referenced group becoming available sends the consumer again", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.consumer.Spec.ConsumerGroups = append(s.consumer.Spec.ConsumerGroups, aiconfigurationv1alpha1.AIGatewayConsumerGroupRef{Name: "group-c"})
		g := s.tracker.aiGatewayGuard(s.version.sdk, s.consumer.GetUID(), testAIGatewayID, true)
		err := updateAIGatewayConsumer(t.Context(), s.cl, s.consumers.sdk, g, s.consumer)
		require.Error(t, err, "an invalid reference is reported")
		_, ok := s.tracker.record(s.consumer.GetUID())
		require.False(t, ok, "a failed membership must not be recorded")
		s.consumers.calls()

		require.NoError(t, s.cl.Create(t.Context(), newTestAIGatewayConsumerGroup("group-c", "konnect-group-c")))
		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSent, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{updates: 1, groupPuts: 1}, calls)
		assert.Contains(t, s.consumers.groups, "konnect-group-c")
	})
}

func TestConfigVersionTrackerAIGatewayScope(t *testing.T) {
	t.Run("operator writes to the AI Gateway do not trigger a verification", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)

		// An operator write elsewhere in the same AI Gateway changes its version.
		scope := sdkops.AIGatewayWriteScope(testAIGatewayID)
		s.tracker.BeforeWrite(t.Context(), scope)
		s.version.version = "2"
		s.tracker.AfterWrite(t.Context(), scope)
		s.tracker.waitForRebaseline()
		s.clock.Advance(testVersionTTL)

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSkipped, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{}, calls)
	})

	t.Run("a control plane with the same ID does not share the AI Gateway state", func(t *testing.T) {
		s := newAIGatewayConsumerSync(t)
		s.update(true)

		// A Kong Gateway control plane scope with the same ID as the AI Gateway
		// sees an external change.
		cp := newFakeConfigVersion(t)
		source := newDPNodesVersionSource(cp.sdk, testCPID)
		_, ok := s.tracker.lastExternalChange(t.Context(), source, testAIGatewayID)
		require.True(t, ok)
		cp.version = "2"
		s.clock.Advance(testVersionTTL)
		changedAt, ok := s.tracker.lastExternalChange(t.Context(), source, testAIGatewayID)
		require.True(t, ok)
		require.False(t, changedAt.IsZero(), "the control plane scope must record the external change")

		g, calls := s.update(true)
		assert.Equal(t, syncOutcomeSkipped, g.outcome)
		assert.Equal(t, aiGatewayConsumerCalls{}, calls)
	})

	t.Run("nil guard always sends", func(t *testing.T) {
		tracker, _ := newTestConfigVersionTracker()
		assert.Nil(t, tracker.aiGatewayGuard(nil, "uid", testAIGatewayID, true))
		assert.Nil(t, tracker.aiGatewayGuard(newFakeAIGatewayVersion(t).sdk, "uid", "", true))
		var nilTracker *ConfigVersionTracker
		assert.Nil(t, nilTracker.aiGatewayGuard(newFakeAIGatewayVersion(t).sdk, "uid", testAIGatewayID, true))
	})
}

func TestUpdateSkipsPeriodicUpdateOfUnchangedAIGatewayEntity(t *testing.T) {
	tracker, _ := newTestConfigVersionTracker()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	consumers := newFakeAIGatewayConsumers(t)
	sdk.AIGatewayConsumersSDK = consumers.sdk
	version := newFakeAIGatewayVersion(t)
	sdk.AIGatewayNodesSDK = version.sdk
	s := newAIGatewayConsumerSync(t)
	consumer := s.consumer
	syncPeriod := time.Minute

	_, syncResult, err := Update(t.Context(), sdk, syncPeriod, s.cl, &metricsmocks.MockRecorder{}, consumer, tracker)
	require.NoError(t, err)
	require.NotNil(t, syncResult)
	syncResult.Commit()
	assert.Equal(t, aiGatewayConsumerCalls{updates: 1, groupPuts: 1}, consumers.calls())

	programmed, ok := findCondition(consumer.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType)
	require.True(t, ok)
	lastUpdate := metav1.NewTime(time.Now().Add(-2 * syncPeriod).Truncate(time.Second))
	setConditionTime(consumer, konnectv1alpha1.KonnectEntityProgrammedConditionType, lastUpdate)
	programmed.LastTransitionTime = lastUpdate

	res, syncResult, err := Update(t.Context(), sdk, syncPeriod, s.cl, &metricsmocks.MockRecorder{}, consumer, tracker)
	require.NoError(t, err)
	assert.Nil(t, syncResult)
	assert.Equal(t, syncPeriod, res.RequeueAfter)
	assert.Equal(t, aiGatewayConsumerCalls{}, consumers.calls())
	after, ok := findCondition(consumer.Status.Conditions, konnectv1alpha1.KonnectEntityProgrammedConditionType)
	require.True(t, ok)
	assert.Equal(t, programmed, after, "a skipped update must not touch the Programmed condition")

	consumer.Generation++
	_, _, err = Update(t.Context(), sdk, syncPeriod, s.cl, &metricsmocks.MockRecorder{}, consumer, tracker)
	require.NoError(t, err)
	assert.Equal(t, aiGatewayConsumerCalls{updates: 1, groupPuts: 1}, consumers.calls(), "a generation change is sent")
}

func findCondition(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c, true
		}
	}
	return metav1.Condition{}, false
}

func setConditionTime(obj *aiconfigurationv1alpha1.AIGatewayConsumer, conditionType string, at metav1.Time) {
	for i := range obj.Status.Conditions {
		if obj.Status.Conditions[i].Type == conditionType {
			obj.Status.Conditions[i].LastTransitionTime = at
		}
	}
}
