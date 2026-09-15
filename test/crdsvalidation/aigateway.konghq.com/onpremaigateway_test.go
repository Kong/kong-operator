package crdsvalidation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validOnPremAIGateway(ns string) *aigatewayv1alpha1.OnPremAIGateway {
	return &aigatewayv1alpha1.OnPremAIGateway{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec:       aigatewayv1alpha1.OnPremAIGatewaySpec{},
	}
}

func TestOnPremAIGateway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("empty spec", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.OnPremAIGateway]{
			{
				Name:       "empty spec - valid",
				TestObject: validOnPremAIGateway(ns.Name),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status defaults", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.OnPremAIGateway]{
			{
				Name: "ready condition defaulted on first status update",
				TestObject: &aigatewayv1alpha1.OnPremAIGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
					// Non-zero status triggers the framework's status update, which causes
					// the API server to apply the +kubebuilder:default on status.conditions.
					Status: aigatewayv1alpha1.OnPremAIGatewayStatus{
						ConfigHash: "deadbeef",
					},
				},
				Assert: func(t *testing.T, obj *aigatewayv1alpha1.OnPremAIGateway) {
					t.Helper()

					readyCond := apimeta.FindStatusCondition(
						obj.Status.Conditions, string(aigatewayv1alpha1.ReadyType),
					)
					require.NotNil(t, readyCond, "Ready condition not found in status")
					require.Equal(t, metav1.ConditionUnknown, readyCond.Status)
					require.Equal(t, "Pending", readyCond.Reason)
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status.configHash validation", func(t *testing.T) {
		common.TestCasesGroup[*aigatewayv1alpha1.OnPremAIGateway]{
			{
				Name:       "configHash at max length - valid",
				TestObject: validOnPremAIGateway(ns.Name),
				StatusUpdate: func(obj *aigatewayv1alpha1.OnPremAIGateway) {
					obj.Status.ConfigHash = strings.Repeat("a", 64)
				},
			},
			{
				Name:       "configHash over max length - invalid",
				TestObject: validOnPremAIGateway(ns.Name),
				StatusUpdate: func(obj *aigatewayv1alpha1.OnPremAIGateway) {
					obj.Status.ConfigHash = strings.Repeat("a", 65)
				},
				// NOTE: Kubernetes 1.32 changed the validation error for values exceeding the maximum length.
				// It used to be:
				// "Too long: may not be longer than 63"
				// In 1.32+ it is:
				// "Too long: may not be more than 63 bytes"
				// We're using here the common part of the error message to avoid breaking the test when upgrading Kubernetes.
				ExpectedStatusUpdateErrorMessage: new("Too long: may not be"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
