package secretcert

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func Test_secretMatchesFilter(t *testing.T) {
	tests := []struct {
		name   string
		secret *corev1.Secret
		want   bool
	}{
		{
			name: "DataPlane managed TLS secret matches",
			secret: newTLSSecret(map[string]string{
				config.DefaultSecretLabelSelector:    config.LabelValueForSelectorTrue,
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			}),
			want: true,
		},
		{
			name: "ControlPlane managed TLS secret matches",
			secret: newTLSSecret(map[string]string{
				config.DefaultSecretLabelSelector:    config.LabelValueForSelectorTrue,
				consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
			}),
			want: true,
		},
		{
			name: "AIGatewayDataPlane managed TLS secret matches",
			secret: newTLSSecret(map[string]string{
				config.DefaultSecretLabelSelector:    config.LabelValueForSelectorTrue,
				consts.GatewayOperatorManagedByLabel: consts.AIGatewayDataPlaneManagedByLabelValue,
			}),
			want: true,
		},
		{
			name: "unrecognized managed-by value does not match",
			secret: newTLSSecret(map[string]string{
				config.DefaultSecretLabelSelector:    config.LabelValueForSelectorTrue,
				consts.GatewayOperatorManagedByLabel: "something-else",
			}),
			want: false,
		},
		{
			name: "missing managed-by label does not match",
			secret: newTLSSecret(map[string]string{
				config.DefaultSecretLabelSelector: config.LabelValueForSelectorTrue,
			}),
			want: false,
		},
		{
			name: "missing selector label does not match",
			secret: newTLSSecret(map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			}),
			want: false,
		},
		{
			name: "non-TLS secret type does not match",
			secret: func() *corev1.Secret {
				s := newTLSSecret(map[string]string{
					config.DefaultSecretLabelSelector:    config.LabelValueForSelectorTrue,
					consts.GatewayOperatorManagedByLabel: consts.AIGatewayDataPlaneManagedByLabelValue,
				})
				s.Type = corev1.SecretTypeOpaque
				return s
			}(),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, secretMatchesFilter(tc.secret))
		})
	}
}

func newTLSSecret(labels map[string]string) *corev1.Secret {
	return &corev1.Secret{
		Name:      "cert",
		Namespace: "default",
		Labels:    labels,
		Type:      corev1.SecretTypeTLS,
	}
}
