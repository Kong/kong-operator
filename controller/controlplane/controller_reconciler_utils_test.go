package controlplane

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

func Test_ensureValidatingWebhookConfiguration(t *testing.T) {
	webhookSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "webhook-svc",
		},
	}

	testCases := []struct {
		name    string
		cp      *operatorv1beta1.ControlPlane
		webhook *admregv1.ValidatingWebhookConfiguration

		testBody func(*testing.T, *Reconciler, *operatorv1beta1.ControlPlane)
	}{
		{
			name: "creating validating webhook configuration",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cp",
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							Replicas: lo.ToPtr(int32(1)),
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										func() corev1.Container {
											c := resources.GenerateControlPlaneContainer(
												resources.GenerateContainerForControlPlaneParams{
													Image:                          consts.DefaultControlPlaneImage,
													AdmissionWebhookCertSecretName: "cert-secret",
												})
											// Envs are set elsewhere so fill in the CONTROLLER_ADMISSION_WEBHOOK_LISTEN
											// here so that the webhook is enabled.
											c.Env = append(c.Env, corev1.EnvVar{
												Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
												Value: "0.0.0.0:8080",
											})
											return c
										}(),
									},
								},
							},
						},
					},
				},
			},
			testBody: func(t *testing.T, r *Reconciler, cp *operatorv1beta1.ControlPlane) {
				var (
					ctx      = context.Background()
					webhooks admregv1.ValidatingWebhookConfigurationList
				)
				require.NoError(t, r.Client.List(ctx, &webhooks))
				require.Empty(t, webhooks.Items)

				certSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cert-secret",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("ca"), // dummy
					},
				}

				res, err := r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc)
				require.NoError(t, err)
				require.Equal(t, op.Created, res)

				require.NoError(t, r.Client.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1)

				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc)
				require.NoError(t, err)
				require.Equal(t, op.Noop, res)
			},
		},
		{
			name: "updating validating webhook configuration enforces ObjectMeta",
			cp: &operatorv1beta1.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cp",
				},
				Spec: operatorv1beta1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
						Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
							Replicas: lo.ToPtr(int32(1)),
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										func() corev1.Container {
											c := resources.GenerateControlPlaneContainer(
												resources.GenerateContainerForControlPlaneParams{
													Image:                          consts.DefaultControlPlaneImage,
													AdmissionWebhookCertSecretName: "cert-secret",
												})
											// Envs are set elsewhere so fill in the CONTROLLER_ADMISSION_WEBHOOK_LISTEN
											// here so that the webhook is enabled.
											c.Env = append(c.Env, corev1.EnvVar{
												Name:  "CONTROLLER_ADMISSION_WEBHOOK_LISTEN",
												Value: "0.0.0.0:8080",
											})
											return c
										}(),
									},
								},
							},
						},
					},
				},
			},
			testBody: func(t *testing.T, r *Reconciler, cp *operatorv1beta1.ControlPlane) {
				var (
					ctx      = context.Background()
					webhooks admregv1.ValidatingWebhookConfigurationList
				)
				require.NoError(t, r.Client.List(ctx, &webhooks))
				require.Empty(t, webhooks.Items)

				certSecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cert-secret",
					},
					Data: map[string][]byte{
						"ca.crt": []byte("ca"), // dummy
					},
				}

				res, err := r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc)
				require.NoError(t, err)
				require.Equal(t, res, op.Created)

				require.NoError(t, r.Client.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1, "webhook configuration should be created")

				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc)
				require.NoError(t, err)
				require.Equal(t, res, op.Noop)

				t.Log("updating webhook configuration outside of the controller")
				{
					w := webhooks.Items[0]
					w.ObjectMeta.Labels["foo"] = "bar"
					require.NoError(t, r.Client.Update(ctx, &w))
				}

				t.Log("running ensureValidatingWebhookConfiguration to enforce ObjectMeta")
				res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, certSecret, webhookSvc)
				require.NoError(t, err)
				require.Equal(t, res, op.Updated)

				require.NoError(t, r.Client.List(ctx, &webhooks))
				require.Len(t, webhooks.Items, 1)
				require.NotContains(t, webhooks.Items[0].Labels, "foo",
					"labels should be updated by the controller so that changes applied by 3rd parties are overwritten",
				)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.cp).
				Build()

			r := &Reconciler{
				Client: fakeClient,
			}

			tc.testBody(t, r, tc.cp)
		})
	}
}
