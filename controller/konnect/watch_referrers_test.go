package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestReferrerChanged(t *testing.T) {
	policyObserving := func(generation, observedGeneration int64, programmed metav1.ConditionStatus, reason string) *configurationv1alpha1.EventGatewayListenerPolicy {
		p := &configurationv1alpha1.EventGatewayListenerPolicy{Name: "p", Namespace: "default"}
		p.SetGeneration(generation)
		if programmed != "" {
			p.SetConditions([]metav1.Condition{{
				Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: programmed, Reason: reason,
				ObservedGeneration: observedGeneration,
			}})
		}
		return p
	}
	policy := func(generation int64, programmed metav1.ConditionStatus, reason string) *configurationv1alpha1.EventGatewayListenerPolicy {
		return policyObserving(generation, generation, programmed, reason)
	}
	// otherStatusWrite changes all but the generation and the status, reason
	// and observed generation of Programmed: its message and transition time,
	// another condition and the Konnect ID.
	otherStatusWrite := func() *configurationv1alpha1.EventGatewayListenerPolicy {
		p := policy(1, metav1.ConditionTrue, "Programmed")
		conditions := p.GetConditions()
		conditions[0].Message = "another message"
		conditions[0].LastTransitionTime = metav1.Now()
		p.SetConditions(append(conditions, metav1.Condition{Type: "Other", Status: metav1.ConditionTrue, Reason: "Other"}))
		p.SetKonnectID("konnect-id")
		return p
	}
	p := referrerChanged()
	for name, tc := range map[string]struct {
		old, new *configurationv1alpha1.EventGatewayListenerPolicy
		want     bool
	}{
		"its spec changes":                     {policy(1, metav1.ConditionTrue, "Programmed"), policy(2, metav1.ConditionTrue, "Programmed"), true},
		"it is programmed":                     {policy(1, metav1.ConditionFalse, "Pending"), policy(1, metav1.ConditionTrue, "Programmed"), true},
		"the reason of Programmed changes":     {policy(1, metav1.ConditionFalse, "Pending"), policy(1, metav1.ConditionFalse, "KonnectAPIOpFailed"), true},
		"another status write leaves it alone": {policy(1, metav1.ConditionTrue, "Programmed"), otherStatusWrite(), false},
		// E.g. it dropped a reference: its Konnect update is done once
		// Programmed observes the generation of the spec change.
		"Programmed observes its new generation": {policyObserving(2, 1, metav1.ConditionTrue, "Programmed"), policy(2, metav1.ConditionTrue, "Programmed"), true},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.Update(event.UpdateEvent{ObjectOld: tc.old, ObjectNew: tc.new}))
		})
	}
	assert.True(t, p.Create(event.CreateEvent{Object: policy(1, "", "")}))
	assert.True(t, p.Delete(event.DeleteEvent{Object: policy(1, "", "")}))
}
