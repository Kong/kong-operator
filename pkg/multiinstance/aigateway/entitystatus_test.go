package aigateway

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
)

func modelFixture(name string) *aiconfigurationv1alpha1.AIGatewayModel {
	return &aiconfigurationv1alpha1.AIGatewayModel{
		Namespace: "default", Name: name,
	}
}

func newSchemeWithAIGateway(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	return scheme
}

func TestEntityStatusReporter_ReportAndStatus(t *testing.T) {
	t.Parallel()

	reporter := NewEntityStatusReporter(newSchemeWithAIGateway(t))

	okModel := modelFixture("model-ok")
	brokenModel := modelFixture("model-broken")
	untouchedModel := modelFixture("model-untouched")

	// Before anything is reported, every entity is Unknown (Pending).
	require.Equal(t, status.ConfigurationStatusUnknown, reporter.KubernetesObjectConfigurationStatus(okModel))
	require.True(t, reporter.AreKubernetesObjectReportsEnabled())
	require.Empty(t, reporter.KubernetesObjectConfigurationStatusMessage(brokenModel))

	reporter.Report(
		[]client.Object{okModel},
		[]EntityFailure{{Obj: brokenModel, Err: errors.New("boom: unresolved reference")}},
	)

	require.Equal(t, status.ConfigurationStatusSucceeded, reporter.KubernetesObjectConfigurationStatus(okModel))
	require.True(t, reporter.KubernetesObjectIsConfigured(okModel))
	require.Equal(t, status.ConfigurationStatusFailed, reporter.KubernetesObjectConfigurationStatus(brokenModel))
	require.False(t, reporter.KubernetesObjectIsConfigured(brokenModel))
	require.Equal(t, "boom: unresolved reference", reporter.KubernetesObjectConfigurationStatusMessage(brokenModel))
	require.Empty(t, reporter.KubernetesObjectConfigurationStatusMessage(okModel))
	// An entity not part of the last sync stays Unknown.
	require.Equal(t, status.ConfigurationStatusUnknown, reporter.KubernetesObjectConfigurationStatus(untouchedModel))

	// A new wholesale report replaces the previous one.
	reporter.Report([]client.Object{untouchedModel}, nil)
	require.Equal(t, status.ConfigurationStatusUnknown, reporter.KubernetesObjectConfigurationStatus(okModel))
	require.Equal(t, status.ConfigurationStatusSucceeded, reporter.KubernetesObjectConfigurationStatus(untouchedModel))
	// The failure message of an entity no longer reported as failed is dropped.
	require.Empty(t, reporter.KubernetesObjectConfigurationStatusMessage(brokenModel))
}

func TestEntityStatusReporter_GenerationStaleness(t *testing.T) {
	t.Parallel()

	reporter := NewEntityStatusReporter(newSchemeWithAIGateway(t))

	model := modelFixture("model")
	model.Generation = 1
	reporter.Report([]client.Object{model}, nil)
	require.Equal(t, status.ConfigurationStatusSucceeded, reporter.KubernetesObjectConfigurationStatus(model))

	// A newer generation that has not been reported yet reads as Unknown.
	model.Generation = 2
	require.Equal(t, status.ConfigurationStatusUnknown, reporter.KubernetesObjectConfigurationStatus(model))
}

func TestEntityStatusReporter_PublishesToQueue(t *testing.T) {
	t.Parallel()

	reporter := NewEntityStatusReporter(newSchemeWithAIGateway(t))
	events := reporter.Queue().Subscribe(
		aiconfigurationv1alpha1.GroupVersion.WithKind("AIGatewayModel"),
	)

	okModel := modelFixture("model-ok")
	brokenModel := modelFixture("model-broken")
	reporter.Report(
		[]client.Object{okModel},
		[]EntityFailure{{Obj: brokenModel, Err: errors.New("boom")}},
	)

	// Both the included and the failed entity are published, deduplicated.
	var names []string
	for range 2 {
		select {
		case ev := <-events:
			names = append(names, ev.Object.GetName())
		default:
			require.FailNow(t, "expected a queued status event")
		}
	}
	require.ElementsMatch(t, []string{"model-ok", "model-broken"}, names)

	// An unchanged re-report publishes nothing: the reconcilers already hold
	// up-to-date conditions, and republishing would make the entity
	// reconciles notify the sync loop forever.
	reporter.Report(
		[]client.Object{okModel},
		[]EntityFailure{{Obj: brokenModel, Err: errors.New("boom")}},
	)
	select {
	case ev := <-events:
		t.Fatalf("expected no event for an unchanged report, got %s", ev.Object.GetName())
	default:
	}

	// A changed failure message republishes only the affected entity:
	// brokenModel keeps its Failed status, so okModel must not be published.
	reporter.Report(
		[]client.Object{okModel},
		[]EntityFailure{{Obj: brokenModel, Err: errors.New("boom: unresolved reference")}},
	)
	select {
	case ev := <-events:
		require.Equal(t, "model-broken", ev.Object.GetName())
	default:
		require.FailNow(t, "expected an event for the entity with the changed message")
	}
	select {
	case ev := <-events:
		t.Fatalf("expected no additional event, got %s", ev.Object.GetName())
	default:
	}
}
