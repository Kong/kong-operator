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
	policy := func(generation int64, programmed metav1.ConditionStatus, reason string) *configurationv1alpha1.EventGatewayListenerPolicy {
		p := &configurationv1alpha1.EventGatewayListenerPolicy{Name: "p", Namespace: "default"}
		p.SetGeneration(generation)
		if programmed != "" {
			p.SetConditions([]metav1.Condition{{Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: programmed, Reason: reason}})
		}
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
		"another status write leaves it alone": {policy(1, metav1.ConditionTrue, "Programmed"), policy(1, metav1.ConditionTrue, "Programmed"), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, p.Update(event.UpdateEvent{ObjectOld: tc.old, ObjectNew: tc.new}))
		})
	}
	assert.True(t, p.Create(event.CreateEvent{Object: policy(1, "", "")}))
	assert.True(t, p.Delete(event.DeleteEvent{Object: policy(1, "", "")}))
}
