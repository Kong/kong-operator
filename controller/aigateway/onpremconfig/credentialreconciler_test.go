package onpremconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func newCredentialReconcilerClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	// The consumer watch lists credentials by this index, as the instance's cache does.
	credIndex := index.OptionsForAIGatewayConsumerCredential()[0]
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayConsumerCredential{}).
		WithIndex(credIndex.Object, credIndex.Field, credIndex.ExtractValueFn).
		Build()
}

// credentialProgrammedCondition returns the credential's Programmed condition, or nil when
// the reconciler wrote none.
func credentialProgrammedCondition(t *testing.T, cl client.Client, nn client.ObjectKey) *metav1.Condition {
	t.Helper()
	cred := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{}
	require.NoError(t, cl.Get(t.Context(), nn, cred))
	return apimeta.FindStatusCondition(cred.Status.Conditions, string(configurationv1.ConditionProgrammed))
}

func TestCredentialReconcilerWritesProgrammedCondition(t *testing.T) {
	t.Parallel()

	cred := credentialReferencing("cred", "default", &commonv1alpha1.NamespacedRef{Name: "consumer"})
	cl := newCredentialReconcilerClient(t, cred, consumerTargeting("consumer", "default", "gw", false))
	rec := &AIGatewayConsumerCredentialReconciler{
		Client:       cl,
		Log:          logr.Discard(),
		StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: true},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
	require.NoError(t, err)

	cond := credentialProgrammedCondition(t, cl, client.ObjectKeyFromObject(cred))
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, string(configurationv1.ReasonProgrammed), cond.Reason)
}

func TestCredentialReconcilerWritesFailedConditionWithMessage(t *testing.T) {
	t.Parallel()

	cred := credentialReferencing("cred", "default", &commonv1alpha1.NamespacedRef{Name: "consumer"})
	cl := newCredentialReconcilerClient(t, cred, consumerTargeting("consumer", "default", "gw", false))
	rec := &AIGatewayConsumerCredentialReconciler{
		Client: cl,
		Log:    logr.Discard(),
		StatusClient: &stubStatusClient{
			configStatus: status.ConfigurationStatusFailed,
			message:      "converting *v1alpha1.AIGatewayConsumerCredential default/cred: Secret not found",
			enableState:  true,
		},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
	require.NoError(t, err)

	cond := credentialProgrammedCondition(t, cl, client.ObjectKeyFromObject(cred))
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(configurationv1.ReasonInvalid), cond.Reason)
	require.Contains(t, cond.Message, "Secret not found")
}

// TestCredentialReconcilerSkipsKonnectManagedCredentials pins that a credential rendered by a
// Konnect-targeted consumer never receives an on-prem Programmed condition: the Konnect
// reconciler owns its status. The credential starts with the condition this reconciler could
// have written while its consumer still existed, so that both claims are pinned at once: a
// condition on a credential the on-prem machinery does not own is left untouched, and the one
// left behind by a consumer that is gone is cleared.
func TestCredentialReconcilerSkipsKonnectManagedCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  *commonv1alpha1.NamespacedRef
		// objs are the consumers the credential may resolve to.
		objs []client.Object
		// wantCleared pins whether the seeded condition must be gone after the reconcile.
		wantCleared bool
	}{
		{
			name: "konnect-targeted consumer",
			ref:  &commonv1alpha1.NamespacedRef{Name: "consumer"},
			objs: []client.Object{consumerTargeting("consumer", "default", "gw", true)},
		},
		{
			name:        "missing consumer",
			ref:         &commonv1alpha1.NamespacedRef{Name: "consumer"},
			wantCleared: true,
		},
		{
			name: "cross-namespace consumer reference",
			ref:  &commonv1alpha1.NamespacedRef{Name: "consumer", Namespace: new("other")},
			objs: []client.Object{consumerTargeting("consumer", "other", "gw", false)},
		},
		{
			name: "reference without a namespacedRef",
			ref:  nil,
		},
	}

	// seeded marks the condition as the one the reconciler found, so that a rewritten one
	// cannot pass for it.
	const seeded = "seeded"

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cred := credentialReferencing("cred", "default", tc.ref)
			cred.Status.Conditions = []metav1.Condition{{
				Type:    string(configurationv1.ConditionProgrammed),
				Status:  metav1.ConditionTrue,
				Reason:  string(configurationv1.ReasonProgrammed),
				Message: seeded,
			}}
			objs := append([]client.Object{cred}, tc.objs...)
			cl := newCredentialReconcilerClient(t, objs...)
			rec := &AIGatewayConsumerCredentialReconciler{
				Client:       cl,
				Log:          logr.Discard(),
				StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: true},
			}

			_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
			require.NoError(t, err)

			cond := credentialProgrammedCondition(t, cl, client.ObjectKeyFromObject(cred))
			if tc.wantCleared {
				require.Nil(t, cond, "the condition of a credential whose consumer is gone must be cleared")
				return
			}
			require.NotNil(t, cond, "no condition must be written on a credential the on-prem machinery does not own")
			require.Equal(t, seeded, cond.Message, "the condition the reconciler found must be left as it was")
		})
	}
}

// consumerReadFailingClient fails every AIGatewayConsumer read with a non-NotFound error,
// simulating a transient cache miss on a consumer that does exist.
type consumerReadFailingClient struct{ client.Client }

var errTransientConsumerRead = errors.New("cache is not serving consumers")

func (c consumerReadFailingClient) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
) error {
	if _, ok := obj.(*aiconfigurationv1alpha1.AIGatewayConsumer); ok {
		return errTransientConsumerRead
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// TestCredentialReconcilerKeepsConditionOnTransientConsumerReadError pins that only a
// definitive NotFound clears the condition: a transient read failure must leave it, since
// Report republishes the credential only when its status or message changes.
func TestCredentialReconcilerKeepsConditionOnTransientConsumerReadError(t *testing.T) {
	t.Parallel()

	cred := credentialReferencing("cred", "default", &commonv1alpha1.NamespacedRef{Name: "consumer"})
	cred.Status.Conditions = []metav1.Condition{{
		Type:    string(configurationv1.ConditionProgrammed),
		Status:  metav1.ConditionTrue,
		Reason:  string(configurationv1.ReasonProgrammed),
		Message: "seeded",
	}}
	cl := consumerReadFailingClient{Client: newCredentialReconcilerClient(t, cred)}
	rec := &AIGatewayConsumerCredentialReconciler{
		Client:       cl,
		Log:          logr.Discard(),
		StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: true},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
	require.NoError(t, err)

	cond := credentialProgrammedCondition(t, cl, client.ObjectKeyFromObject(cred))
	require.NotNil(t, cond, "a transient read failure must not strip the condition")
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, "seeded", cond.Message)
}

// TestCredentialReconcilerIgnoresDeletedCredentials pins that a delete event does not error.
func TestCredentialReconcilerIgnoresDeletedCredentials(t *testing.T) {
	t.Parallel()

	cl := newCredentialReconcilerClient(t)
	rec := &AIGatewayConsumerCredentialReconciler{
		Client:       cl,
		Log:          logr.Discard(),
		StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: true},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{
		Namespace: "default",
		Name:      "gone",
	})
	require.NoError(t, err)
}
