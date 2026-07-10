package konnect

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

type failingKonnectReferenceResolver struct {
	err error
}

func (r failingKonnectReferenceResolver) ResolveKonnectReferences(context.Context, client.Client) error {
	return r.err
}

func TestHandleKongConsumerSpecific(t *testing.T) {
	t.Run("KongConsumer", func(t *testing.T) {
		testCases := []struct {
			name            string
			consumer        *configurationv1.KongConsumer
			existingSecrets []client.Object
			wantStop        bool
			wantIsProblem   bool
			wantCondition   metav1.Condition
		}{
			{
				name: "no credentials",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: "test",
					},
					Credentials: []string{},
				},
				wantStop:      true,
				wantIsProblem: false,
			},
			{
				name: "no credentials and outdated status",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-consumer",
						Namespace:  "test",
						Generation: 1,
					},
					Credentials: []string{},
					Status: configurationv1.KongConsumerStatus{
						Conditions: []metav1.Condition{
							{
								Type:               configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
								Status:             metav1.ConditionTrue,
								Reason:             configurationv1.ReasonKongConsumerCredentialSecretRefsValid,
								ObservedGeneration: 1,
							},
						},
					},
				},
				wantStop:      false,
				wantIsProblem: false,
			},
			{
				name: "all credentials exist, status needs an update",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: "test",
					},
					Credentials: []string{"secret1", "secret2"},
				},
				existingSecrets: []client.Object{
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret1",
							Namespace: "test",
						},
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret2",
							Namespace: "test",
						},
					},
				},
				wantStop:      true,
				wantIsProblem: false,
			},
			{
				name: "all credentials exist, status is up to date",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-consumer",
						Namespace:  "test",
						Generation: 1,
					},
					Credentials: []string{"secret1", "secret2"},
					Status: configurationv1.KongConsumerStatus{
						Conditions: []metav1.Condition{
							{
								Type:               configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
								Status:             metav1.ConditionTrue,
								Reason:             configurationv1.ReasonKongConsumerCredentialSecretRefsValid,
								ObservedGeneration: 1,
							},
						},
					},
				},
				existingSecrets: []client.Object{
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret1",
							Namespace: "test",
						},
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret2",
							Namespace: "test",
						},
					},
				},
				wantStop:      false,
				wantIsProblem: false,
			},
			{
				name: "some credentials missing",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: "test",
					},
					Credentials: []string{"secret1", "secret2", "missing-secret"},
				},
				existingSecrets: []client.Object{
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret1",
							Namespace: "test",
						},
					},
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "secret2",
							Namespace: "test",
						},
					},
				},
				wantStop:      true,
				wantIsProblem: true,
				wantCondition: metav1.Condition{
					Type:    configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
					Status:  metav1.ConditionFalse,
					Reason:  configurationv1.ReasonKongConsumerCredentialSecretRefInvalid,
					Message: "secrets \"missing-secret\" not found",
				},
			},
			{
				name: "all credentials missing",
				consumer: &configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-consumer",
						Namespace: "test",
					},
					Credentials: []string{"missing-secret1", "missing-secret2"},
				},
				wantStop:      true,
				wantIsProblem: true,
				wantCondition: metav1.Condition{
					Type:    configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
					Status:  metav1.ConditionFalse,
					Reason:  configurationv1.ReasonKongConsumerCredentialSecretRefInvalid,
					Message: "secrets \"missing-secret1\" not found\nsecrets \"missing-secret2\" not found",
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cl := fake.NewClientBuilder().
					WithObjects(tc.existingSecrets...).
					WithScheme(scheme.Get()).
					Build()

				stop, isProblem := handleKongConsumerSpecific(t.Context(), cl, tc.consumer)

				assert.Equal(t, tc.wantStop, stop)
				assert.Equal(t, tc.wantIsProblem, isProblem)

				// Check condition if a problem was expected
				if tc.wantIsProblem {
					hasCondition := false
					for _, cond := range tc.consumer.Status.Conditions {
						if cond.Type == tc.wantCondition.Type {
							hasCondition = true
							assert.Equal(t, tc.wantCondition.Status, cond.Status)
							assert.Equal(t, tc.wantCondition.Reason, cond.Reason)
							assert.Equal(t, tc.wantCondition.Message, cond.Message)
						}
					}
					assert.Truef(t, hasCondition, "Expected condition not found, conditions %v", tc.consumer.Status.Conditions)
				}
			})
		}
	})
}

func TestHandleKonnectReferencesResolution(t *testing.T) {
	agent := &konnectv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: konnectv1alpha1.AIGatewayAgentSpec{
			APISpec: konnectv1alpha1.AIGatewayAgentAPISpec{
				Policies: []konnectv1alpha1.AIGatewayPolicyRef{{Name: "missing-policy"}},
			},
		},
	}

	t.Run("missing referenced CR sets condition False with NotFound", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(agent.DeepCopy()).Build()
		ent := agent.DeepCopy()
		updated, isProblem, err := handleKonnectReferences(t.Context(), cl, ent, ent)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonNotFound, cond.Reason)
	})

	t.Run("referenced CR exists but is not programmed sets condition False with NotProgrammed", func(t *testing.T) {
		policy := &konnectv1alpha1.AIGatewayPolicy{ObjectMeta: metav1.ObjectMeta{Name: "missing-policy", Namespace: "ns"}}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(agent.DeepCopy(), policy).Build()
		ent := agent.DeepCopy()
		updated, isProblem, err := handleKonnectReferences(t.Context(), cl, ent, ent)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonNotProgrammed, cond.Reason)
	})

	t.Run("cross-namespace ref sets condition False with Invalid", func(t *testing.T) {
		ent := agent.DeepCopy()
		ent.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{{
			Namespace: "other-ns",
			Name:      "policy",
		}}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(ent.DeepCopy()).Build()

		updated, isProblem, err := handleKonnectReferences(t.Context(), cl, ent, ent)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonInvalid, cond.Reason)
	})

	t.Run("different GatewayID ref sets condition False with Invalid", func(t *testing.T) {
		policy := &konnectv1alpha1.AIGatewayPolicy{ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "ns"}}
		policy.SetKonnectID("kid-123")
		policy.SetGatewayID("gw-other")
		ent := agent.DeepCopy()
		ent.SetGatewayID("gw-agent")
		ent.Spec.APISpec.Policies = []konnectv1alpha1.AIGatewayPolicyRef{{Name: policy.Name}}
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(ent.DeepCopy(), policy).Build()

		updated, isProblem, err := handleKonnectReferences(t.Context(), cl, ent, ent)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonInvalid, cond.Reason)
	})

	t.Run("joined same-category reference errors keep specific reason", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		ent := agent.DeepCopy()
		resolverErr := errors.Join(
			konnectv1alpha1.ReferenceNotFoundError{Kind: "AIGatewayPolicy", Namespace: "ns", Name: "policy-1"},
			konnectv1alpha1.ReferenceNotFoundError{Kind: "AIGatewayPolicy", Namespace: "ns", Name: "policy-2"},
		)

		updated, isProblem, err := handleKonnectReferences(
			t.Context(),
			cl,
			ent,
			failingKonnectReferenceResolver{err: resolverErr},
		)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonNotFound, cond.Reason)
		assert.Contains(t, cond.Message, "policy-1")
		assert.Contains(t, cond.Message, "policy-2")
	})

	t.Run("joined mixed-category reference errors set generic failure reason", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		ent := agent.DeepCopy()
		resolverErr := errors.Join(
			konnectv1alpha1.ReferenceNotFoundError{Kind: "AIGatewayPolicy", Namespace: "ns", Name: "missing-policy"},
			konnectv1alpha1.ReferenceNotProgrammedError{Kind: "AIGatewayPolicy", Namespace: "ns", Name: "pending-policy"},
			konnectv1alpha1.ReferenceCrossNamespaceError{
				Kind:              "AIGatewayPolicy",
				Namespace:         "other-ns",
				Name:              "invalid-policy",
				ReferrerNamespace: "ns",
			},
		)

		updated, isProblem, err := handleKonnectReferences(
			t.Context(),
			cl,
			ent,
			failingKonnectReferenceResolver{err: resolverErr},
		)
		require.NoError(t, err)
		require.True(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonResolutionFailed, cond.Reason)
		assert.Contains(t, cond.Message, "missing-policy")
		assert.Contains(t, cond.Message, "pending-policy")
		assert.Contains(t, cond.Message, "invalid-policy")
	})

	t.Run("programmed referenced CR sets condition True", func(t *testing.T) {
		policy := &konnectv1alpha1.AIGatewayPolicy{ObjectMeta: metav1.ObjectMeta{Name: "missing-policy", Namespace: "ns"}}
		policy.SetKonnectID("kid-123")
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(agent.DeepCopy(), policy).Build()
		ent := agent.DeepCopy()
		updated, isProblem, err := handleKonnectReferences(t.Context(), cl, ent, ent)
		require.NoError(t, err)
		require.False(t, isProblem)
		require.True(t, updated)

		cond, ok := getKonnectReferencesResolvedCondition(ent.Status.Conditions)
		require.True(t, ok, "expected KonnectReferencesResolved condition to be set")
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectReferencesResolvedReasonResolved, cond.Reason)
	})

	t.Run("unexpected resolver error is returned for normal reconcile retry", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		ent := agent.DeepCopy()
		resolverErr := errors.New("cache unavailable")

		updated, isProblem, err := handleKonnectReferences(
			t.Context(),
			cl,
			ent,
			failingKonnectReferenceResolver{err: resolverErr},
		)
		require.ErrorIs(t, err, resolverErr)
		require.False(t, isProblem)
		require.False(t, updated)
	})
}

func getKonnectReferencesResolvedCondition(conditions []metav1.Condition) (metav1.Condition, bool) {
	for _, cond := range conditions {
		if cond.Type == konnectv1alpha1.KonnectReferencesResolvedConditionType {
			return cond, true
		}
	}
	return metav1.Condition{}, false
}
