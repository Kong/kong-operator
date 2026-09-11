package dataplane

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// errListClient returns an error on every List call.
type errListClient struct{ client.Client }

func (c *errListClient) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return assert.AnError
}

func Test_enqueueForAIGatewayDataPlaneCertificateSecretRef(t *testing.T) {
	const (
		ns         = "test-ns"
		secretName = "user-cert"
	)

	secret := &corev1.Secret{
		Namespace: ns, Name: secretName,
	}

	aigwdpMatching := &aigatewayv1alpha1.AIGatewayDataPlane{
		Namespace: ns, Name: "dp-match",
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			CertificateSecret: &aigatewayv1alpha1.CertificateSecret{
				Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
				SecretRef:    &aigatewayv1alpha1.SecretRef{Name: secretName},
			},
		},
	}

	aigwdpOther := &aigatewayv1alpha1.AIGatewayDataPlane{
		Namespace: ns, Name: "dp-other",
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			CertificateSecret: &aigatewayv1alpha1.CertificateSecret{
				Provisioning: new(aigatewayv1alpha1.ManualCertificateProvisioning),
				SecretRef:    &aigatewayv1alpha1.SecretRef{Name: "other-secret"},
			},
		},
	}

	scheme := managerscheme.Get()

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, aigwdpMatching, aigwdpOther)
	for _, o := range index.OptionsForAIGatewayDataPlane() {
		if o.Field == index.IndexFieldAIGatewayDataPlaneOnCertificateSecret {
			builder = builder.WithIndex(o.Object, o.Field, o.ExtractValueFn)
		}
	}
	cl := builder.Build()

	tests := []struct {
		name    string
		cl      client.Client
		obj     client.Object
		wantNil bool
		want    []types.NamespacedName
	}{
		{
			name: "returns requests for matching DataPlanes",
			cl:   cl,
			obj:  secret,
			want: []types.NamespacedName{
				{Namespace: ns, Name: "dp-match"},
			},
		},
		{
			name:    "returns nil when obj is not a Secret",
			cl:      cl,
			obj:     &konnectv1alpha1.KonnectAIGateway{},
			wantNil: true,
		},
		{
			name:    "returns nil when List fails",
			cl:      &errListClient{},
			obj:     secret,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapFunc := enqueueForAIGatewayDataPlaneCertificateSecretRef(tc.cl)
			requests := mapFunc(t.Context(), tc.obj)
			if tc.wantNil {
				require.Nil(t, requests)
				return
			}
			require.Len(t, requests, len(tc.want))
			var got []types.NamespacedName
			for _, r := range requests {
				got = append(got, r.NamespacedName)
			}
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}
