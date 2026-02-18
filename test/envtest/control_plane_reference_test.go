//go:build envtest

package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/ingress-controller/test/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/test/helpers/conditions"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
)

// TestControlPlaneReferenceHandling tests ControlPlaneReference handling in controllers supporting it.
// It expects that if an object has a ControlPlaneReference set, it should only be programmed if the reference
// is set to 'kic'.
func TestControlPlaneReferenceHandling(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	const ingressClassName = "kongenvtest"
	scheme := Scheme(t, WithKong)
	envcfg, _ := Setup(t, ctx, scheme)
	ctrlClient := NewControllerClient(t, scheme, envcfg)
	deployIngressClass(ctx, t, ingressClassName, ctrlClient)
	ns := CreateNamespace(ctx, t, ctrlClient)

	var (
		kongContainer = runKongEnterprise(ctx, t)
	)

	logs := RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithUpdateStatus(),
		WithIngressClass(ingressClassName),
		WithPublishService(ns.Name),
		WithProxySyncInterval(100*time.Millisecond),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)
	WaitForManagerStart(t, logs)

	var (
		kicCPRef = &commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKIC,
		}
		konnectCPRef = &commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "test-cp",
			},
		}

		validConsumer = func() *configurationv1.KongConsumer {
			return &configurationv1.KongConsumer{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "consumer-",
					Namespace:    ns.Name,
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClassName,
					},
				},
				Username: uuid.New().String(),
			}
		}
		validConsumerGroup = func() *configurationv1beta1.KongConsumerGroup {
			return &configurationv1beta1.KongConsumerGroup{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "consumer-group-",
					Namespace:    ns.Name,
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClassName,
					},
				},
				Spec: configurationv1beta1.KongConsumerGroupSpec{
					Name: "consumer-group-" + lo.RandomString(8, lo.LowerCaseLettersCharset),
				},
			}
		}
		validVault = func() *configurationv1alpha1.KongVault {
			return &configurationv1alpha1.KongVault{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "vault-",
					Namespace:    ns.Name,
					Annotations: map[string]string{
						annotations.IngressClassKey: ingressClassName,
					},
				},
				Spec: configurationv1alpha1.KongVaultSpec{
					Backend: "env",
					// Prefix has to be unique for each Vault object as it's validated by KIC in translation.
					Prefix: "prefix-" + lo.RandomString(8, lo.LowerCaseLettersCharset),
				},
			}
		}
	)

	testCases := []struct {
		name   string
		object interface {
			client.Object
			GetConditions() []metav1.Condition
			SetControlPlaneRef(*commonv1alpha1.ControlPlaneRef)
		}
		controlPlaneRef     *commonv1alpha1.ControlPlaneRef
		shouldSetProgrammed bool
	}{
		{
			name:                "KongConsumer - without ControlPlaneRef",
			object:              validConsumer(),
			shouldSetProgrammed: true,
		},
		{
			name:                "KongConsumer - with ControlPlaneRef == kic",
			object:              validConsumer(),
			controlPlaneRef:     kicCPRef,
			shouldSetProgrammed: true,
		},
		{
			name:                "KongConsumer - with ControlPlaneRef != kic",
			object:              validConsumer(),
			controlPlaneRef:     konnectCPRef,
			shouldSetProgrammed: false,
		},
		{
			name:                "KongConsumerGroup - without ControlPlaneRef",
			object:              validConsumerGroup(),
			shouldSetProgrammed: true,
		},
		{
			name:                "KongConsumerGroup - with ControlPlaneRef == kic",
			object:              validConsumerGroup(),
			controlPlaneRef:     kicCPRef,
			shouldSetProgrammed: true,
		},
		{
			name:                "KongConsumerGroup - with ControlPlaneRef != kic",
			object:              validConsumerGroup(),
			controlPlaneRef:     konnectCPRef,
			shouldSetProgrammed: false,
		},
		{
			name:                "KongVault - without ControlPlaneRef",
			object:              validVault(),
			shouldSetProgrammed: true,
		},
		{
			name:                "KongVault - with ControlPlaneRef == kic",
			object:              validVault(),
			controlPlaneRef:     kicCPRef,
			shouldSetProgrammed: true,
		},
		{
			name:                "KongVault - with ControlPlaneRef != kic",
			object:              validVault(),
			controlPlaneRef:     konnectCPRef,
			shouldSetProgrammed: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.controlPlaneRef != nil {
				tc.object.SetControlPlaneRef(tc.controlPlaneRef)
			}
			err := ctrlClient.Create(ctx, tc.object)
			require.NoError(t, err)

			if tc.shouldSetProgrammed {
				require.EventuallyWithT(t, func(t *assert.CollectT) {
					if !assert.NoError(t, ctrlClient.Get(ctx, client.ObjectKeyFromObject(tc.object), tc.object)) {
						return
					}
					assert.True(t, conditions.Contain(
						tc.object.GetConditions(),
						conditions.WithType(string(configurationv1.ConditionProgrammed)),
						conditions.WithStatus(metav1.ConditionTrue),
					))
				}, waitTime, tickDuration, "expected object to be programmed")
			} else {
				asserts.Never(t, func(ctx context.Context) bool {
					require.NoError(t, ctrlClient.Get(ctx, client.ObjectKeyFromObject(tc.object), tc.object))

					return conditions.Contain(
						tc.object.GetConditions(),
						conditions.WithType(string(configurationv1.ConditionProgrammed)),
						conditions.WithStatus(metav1.ConditionTrue),
					)
				}, waitTime, tickDuration, "expected object not to be programmed")
			}
		})
	}
}
