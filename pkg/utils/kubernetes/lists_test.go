package kubernetes_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestListValidatingWebhookConfigurationsForOwner(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name          string
		objects       []runtime.Object
		expectedCount int
	}{
		{
			name:          "no objects",
			expectedCount: 0,
		},
		{
			name: "multiple objects, one owned by uid, one not",
			objects: []runtime.Object{
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "owned",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByNameLabel: "owner",
						},
					},
				},
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							consts.GatewayOperatorManagedByNameLabel: "not-owned",
						},
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "multiple objects, one owned by uid, one by another",
			objects: []runtime.Object{
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "owned",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByNameLabel: "owner",
						},
					},
				},
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-owned",
						Labels: map[string]string{
							consts.GatewayOperatorManagedByNameLabel: "another-owner",
						},
					},
				},
			},
			expectedCount: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewFakeClient(tc.objects...)
			ownedCfgs, err := k8sutils.ListValidatingWebhookConfigurations(ctx,
				c,
				client.MatchingLabels{
					consts.GatewayOperatorManagedByNameLabel: "owner",
				},
			)
			require.NoError(t, err)
			require.Len(t, ownedCfgs, tc.expectedCount)
		})
	}
}
