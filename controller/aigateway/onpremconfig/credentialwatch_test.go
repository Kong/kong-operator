package onpremconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// consumerTargeting returns an AIGatewayConsumer in namespace ns whose aiGatewayRef points at
// gateway gwName, targeting a Konnect or an on-prem AI Gateway.
func consumerTargeting(name, ns, gwName string, konnect bool) *aiconfigurationv1alpha1.AIGatewayConsumer {
	ref := aiconfigurationv1alpha1.AIGatewayRef{
		Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
		Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gwName},
	}
	if konnect {
		ref.Group = aiconfigurationv1alpha1.AIGatewayRefGroupKonnect
		ref.Kind = aiconfigurationv1alpha1.AIGatewayRefKindKonnect
	}
	return &aiconfigurationv1alpha1.AIGatewayConsumer{
		Namespace: ns,
		Name:      name,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{AIGatewayRef: ref},
	}
}

// credentialReferencing returns an AIGatewayConsumerCredential in namespace ns referencing the
// consumer through ref, which is nil for the konnectID shape the CRD also admits.
func credentialReferencing(
	name, ns string,
	ref *commonv1alpha1.NamespacedRef,
) *aiconfigurationv1alpha1.AIGatewayConsumerCredential {
	return &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Namespace: ns,
		Name:      name,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: ref,
			},
		},
	}
}

func TestMapCredentialToConsumer(t *testing.T) {
	const (
		credName     = "cred"
		consumerName = "consumer"
	)
	sameNSRef := func() *commonv1alpha1.NamespacedRef {
		return &commonv1alpha1.NamespacedRef{Name: consumerName}
	}

	tests := []struct {
		name string
		obj  client.Object
		objs []client.Object
		want *types.NamespacedName
	}{
		{
			name: "on-prem consumer is enqueued",
			obj:  credentialReferencing(credName, "default", sameNSRef()),
			objs: []client.Object{consumerTargeting(consumerName, "default", "gw", false)},
			want: &types.NamespacedName{Namespace: "default", Name: consumerName},
		},
		{
			name: "explicit ref namespace pointing back at the credential's namespace is enqueued",
			obj:  credentialReferencing(credName, "default", &commonv1alpha1.NamespacedRef{Name: consumerName, Namespace: new("default")}),
			objs: []client.Object{consumerTargeting(consumerName, "default", "gw", false)},
			want: &types.NamespacedName{Namespace: "default", Name: consumerName},
		},
		{
			// The translation only renders credentials living in the consumer's namespace.
			name: "cross-namespace reference is skipped",
			obj:  credentialReferencing(credName, "default", &commonv1alpha1.NamespacedRef{Name: consumerName, Namespace: new("other")}),
			objs: []client.Object{consumerTargeting(consumerName, "other", "gw", false)},
		},
		{
			name: "konnect-targeted consumer is skipped",
			obj:  credentialReferencing(credName, "default", sameNSRef()),
			objs: []client.Object{consumerTargeting(consumerName, "default", "gw", true)},
		},
		{
			name: "missing namespacedRef does not panic",
			obj:  credentialReferencing(credName, "default", nil),
		},
		{
			name: "unknown consumer is skipped",
			obj:  credentialReferencing(credName, "default", sameNSRef()),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &AIGatewayConsumerReconciler{Client: newReconcilerClient(t, tc.objs...)}

			got := rec.mapCredentialToConsumer(t.Context(), tc.obj)

			if tc.want == nil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, []reconcile.Request{{NamespacedName: *tc.want}}, got)
		})
	}
}
