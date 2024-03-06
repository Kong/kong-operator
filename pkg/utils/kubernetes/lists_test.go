package kubernetes_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

func TestListValidatingWebhookConfigurationsForOwner(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name          string
		objects       []runtime.Object
		ownerUID      types.UID
		expectedCount int
	}{
		{
			name:          "no objects",
			expectedCount: 0,
		},
		{
			name:     "multiple objects, one owned by uid, one not",
			ownerUID: types.UID("owner"),
			objects: []runtime.Object{
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "owned",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: "owner",
							},
						},
					},
				},
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-owned",
					},
				},
			},
			expectedCount: 1,
		},
		{
			name:     "multiple objects, one owned by uid, one by another",
			ownerUID: types.UID("owner"),
			objects: []runtime.Object{
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "owned",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: "owner",
							},
						},
					},
				},
				&admregv1.ValidatingWebhookConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-owned",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: "another-owner",
							},
						},
					},
				},
			},
			expectedCount: 1,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewFakeClient(tc.objects...)
			ownedCfgs, err := k8sutils.ListValidatingWebhookConfigurationsForOwner(ctx, client, tc.ownerUID)
			require.NoError(t, err)
			require.Len(t, ownedCfgs, tc.expectedCount)
		})
	}
}
