package dataplane

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/builder"
	"github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	"github.com/kong/kong-operator/v2/test/helpers"
)

func init() {
	if err := operatorv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1beta1 scheme")
		os.Exit(1)
	}
}

// TODO: This test requires a rewrite to get rid of the mystical .Reconcile()
// calls which tests writers each time have to guess how many of those will be
// necessary.
// There's an open issue to rewrite that into e.g. envtest based test(s) so that
// test writers will be able to rely on the reconciler running against an apiserver
// and just asserting on the actual desired effect.
//
// Ref: https://github.com/kong/kong-operator/issues/172
func TestDataPlaneBlueGreenReconciler_Reconcile(t *testing.T) {
	ca := helpers.CreateCA(t)
	mtlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mtls-secret",
			Namespace: "test-namespace",
		},
		Data: map[string][]byte{
			"tls.crt": ca.CertPEM.Bytes(),
			"tls.key": ca.KeyPEM.Bytes(),
		},
	}

	testCases := []struct {
		name                  string
		dataplaneReq          reconcile.Request
		dataplane             *operatorv1beta1.DataPlane
		dataplaneSubResources []client.Object
		testBody              func(t *testing.T, reconciler BlueGreenReconciler, dataplaneReq reconcile.Request)
	}{
		{
			name: "when live Deployment Pods become not Ready, DataPlane status should have the Ready status condition set to false",
			dataplaneReq: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dataplane",
					Namespace: "default",
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-dataplane",
					Namespace:  "default",
					UID:        types.UID(uuid.NewString()),
					Generation: 1,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							Rollout: &operatorv1beta1.Rollout{
								Strategy: operatorv1beta1.RolloutStrategy{
									BlueGreen: &operatorv1beta1.BlueGreenStrategy{
										Promotion: operatorv1beta1.Promotion{
											Strategy: operatorv1beta1.BreakBeforePromotion,
										},
									},
								},
							},
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name:  consts.DataPlaneProxyContainerName,
												Image: consts.DefaultDataPlaneImage,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			dataplaneSubResources: []client.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneDeploymentStateLabel: string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						ReadyReplicas:     1,
						Replicas:          1,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-admin-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
							consts.DataPlaneServiceStateLabel:    string(consts.DataPlaneStateLabelValueLive),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: corev1.ClusterIPNone,
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-proxy-service",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                "test-dataplane",
							consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
							consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
							consts.GatewayOperatorManagedByLabel: string(consts.DataPlaneManagedLabelValue),
						},
					},
					Spec: corev1.ServiceSpec{
						ClusterIP:  "10.0.0.1",
						ClusterIPs: []string{"10.0.0.1"},
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "6.7.8.9",
								},
							},
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-2",
						Namespace: "default",
					},
					Data: helpers.TLSSecretData(t, ca,
						helpers.CreateCert(t, "*.test-admin-service.default.svc", ca.Cert, ca.Key),
					),
				},
			},
			testBody: func(t *testing.T, reconciler BlueGreenReconciler, dataplaneReq reconcile.Request) {
				ctx := t.Context()

				_, err := reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				// The second reconcile is needed because the first one would only get to marking
				// the DataPlane as Scheduled.
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// The third reconcile is needed because the second one will only ensure
				// the service is deployed for the DataPlane.
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				// The fourth reconcile is needed to ensure the service name in the dataplane status
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				dp := &operatorv1beta1.DataPlane{}
				dpNN := types.NamespacedName{Namespace: dataplaneReq.Namespace, Name: dataplaneReq.Name}
				require.NoError(t, reconciler.Get(ctx, dpNN, dp))
				require.Equal(t, "test-proxy-service", dp.Status.Service)
				require.Equal(t, []operatorv1beta1.Address{
					// This currently assumes that we sort the addresses in a way
					// such that LoadBalancer IPs, then LoadBalancer hostnames are added
					// and then ClusterIPs follow.
					// If this ends up being the desired logic and aligns with what
					// has been agreed in https://github.com/kong/kong-operator/issues/281
					// then no action has to be taken. Otherwise this might need to be changed.
					{
						Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
						Value:      "6.7.8.9",
						SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
					},
					{
						Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
						Value:      "10.0.0.1",
						SourceType: operatorv1beta1.PrivateIPAddressSourceType,
					},
				}, dp.Status.Addresses)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// Blue Green reconciliation starts
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				require.NoError(t, reconciler.Get(ctx, dpNN, dp))
				require.NotNil(t, dp.Status.RolloutStatus)
				require.Len(t, dp.Status.RolloutStatus.Conditions, 1)
				require.EqualValues(t, kcfgdataplane.DataPlaneConditionTypeRolledOut, dp.Status.RolloutStatus.Conditions[0].Type)
				require.Equal(t, metav1.ConditionFalse, dp.Status.RolloutStatus.Conditions[0].Status)

				// Update the DataPlane deployment options to trigger rollout.
				dp.Spec.Deployment.PodTemplateSpec.Spec.Containers = append(
					dp.Spec.Deployment.PodTemplateSpec.Spec.Containers, corev1.Container{
						Name:  "proxy",
						Image: "kong:3.3.0",
					},
				)
				// We're not running these tests against an API server to let's just bump the generation ourselves.
				dp.Generation++
				require.NoError(t, reconciler.Update(ctx, dp))

				// Run reconciliation to advance the rollout.
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				// Update the DataPlane preview deployment status to pretend that its Pods are ready.
				previewDeploymentLabelReq, err := labels.NewRequirement(
					consts.DataPlaneDeploymentStateLabel,
					selection.Equals,
					[]string{consts.DataPlaneStateLabelValuePreview},
				)
				require.NoError(t, err)
				previewDeployments := &appsv1.DeploymentList{}
				require.NoError(t, reconciler.List(ctx, previewDeployments, &client.ListOptions{
					LabelSelector: labels.NewSelector().Add(*previewDeploymentLabelReq),
				}))
				require.Len(t, previewDeployments.Items, 1)
				previewDeployments.Items[0].Status.AvailableReplicas = 1
				previewDeployments.Items[0].Status.ReadyReplicas = 1
				previewDeployments.Items[0].Status.Replicas = 1
				require.NoError(t, reconciler.Client.Status().Update(ctx, &(previewDeployments.Items[0])))

				// Update the DataPlane deployment status to pretend that its Pods are not ready.
				require.NoError(t,
					reconciler.Client.Status().Update(ctx,
						&appsv1.Deployment{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-dataplane-deployment",
								Namespace: "default",
							},
							Status: appsv1.DeploymentStatus{
								Replicas:          0,
								ReadyReplicas:     0,
								AvailableReplicas: 0,
							},
						},
					),
				)

				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)
				_, err = reconciler.Reconcile(ctx, dataplaneReq)
				require.NoError(t, err)

				require.NoError(t, reconciler.Get(ctx, dpNN, dp))

				t.Logf("DataPlane status should have the Ready status condition set to false")
				require.Len(t, dp.Status.Conditions, 1)
				require.EqualValues(t, kcfgdataplane.ReadyType, dp.Status.Conditions[0].Type)
				require.Equal(t, metav1.ConditionFalse, dp.Status.Conditions[0].Status,
					"DataPlane's Ready status condition should be set to false when live Deployment has no Ready replicas",
				)
				require.EqualValues(t, kcfgdataplane.WaitingToBecomeReadyReason, dp.Status.Conditions[0].Reason)

				t.Logf("DataPlane rollout status should have the Ready status condition set to true")
				require.NotNil(t, dp.Status.RolloutStatus)
				require.Len(t, dp.Status.RolloutStatus.Conditions, 1)
				require.EqualValues(t, kcfgdataplane.DataPlaneConditionTypeRolledOut, dp.Status.RolloutStatus.Conditions[0].Type)
				require.Equal(t, metav1.ConditionFalse, dp.Status.RolloutStatus.Conditions[0].Status,
					"DataPlane's Ready rollout status condition should be set to true when preview Deployment has Ready replicas",
				)
				require.EqualValues(t, kcfgdataplane.DataPlaneConditionReasonRolloutAwaitingPromotion, dp.Status.RolloutStatus.Conditions[0].Reason)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []client.Object{
				tc.dataplane,
				mtlsSecret,
			}

			for _, dataplaneSubresource := range tc.dataplaneSubResources {
				k8sutils.SetOwnerForObject(dataplaneSubresource, tc.dataplane)
				ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.dataplane).
				Build()

			reconciler := BlueGreenReconciler{
				Client:                   fakeClient,
				ClusterCASecretName:      mtlsSecret.Name,
				ClusterCASecretNamespace: mtlsSecret.Namespace,
				DataPlaneController: &Reconciler{
					Client:                   fakeClient,
					ClusterCASecretName:      mtlsSecret.Name,
					ClusterCASecretNamespace: mtlsSecret.Namespace,
				},
			}

			tc.testBody(t, reconciler, tc.dataplaneReq)
		})
	}
}

func TestCanProceedWithPromotion(t *testing.T) {
	testCases := []struct {
		name               string
		dataplane          operatorv1beta1.DataPlane
		expectedCanProceed bool
		expectedErr        error
	}{
		{
			name: "AutomaticPromotion strategy",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(operatorv1beta1.AutomaticPromotion).
				Build(),
			expectedCanProceed: true,
		},
		{
			name: "BreakBeforePromotion strategy, no annotation",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(operatorv1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation false",
			dataplane: *builder.NewDataPlaneBuilder().
				WithObjectMeta(
					metav1.ObjectMeta{
						Annotations: map[string]string{
							operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey: "false",
						},
					},
				).
				WithPromotionStrategy(operatorv1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation true",
			dataplane: *builder.NewDataPlaneBuilder().
				WithObjectMeta(
					metav1.ObjectMeta{
						Annotations: map[string]string{
							operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey: operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
						},
					},
				).
				WithPromotionStrategy(operatorv1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: true,
		},
		{
			name: "unknown strategy",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(operatorv1beta1.PromotionStrategy("unknown")).
				Build(),
			expectedErr: errors.New(`unknown promotion strategy: "unknown"`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			canProceed, err := canProceedWithPromotion(tc.dataplane)
			if tc.expectedErr != nil {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedCanProceed, canProceed)
		})
	}
}

func TestEnsurePreviewIngressService(t *testing.T) {
	testCases := []struct {
		name                     string
		dataplane                *operatorv1beta1.DataPlane
		existingServiceModifier  func(*testing.T, context.Context, client.Client, *corev1.Service)
		expectedCreatedOrUpdated op.Result
		expectedService          *corev1.Service
		// expectedErrorMessage is empty if we expect no error, otherwise returned error must contain it.
		expectedErrorMessage string
	}{
		{
			name: "have existing service, should not update",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(
				metav1.ObjectMeta{Namespace: "default", Name: "dp-0"},
			).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithPromotionStrategy(operatorv1beta1.AutomaticPromotion).Build(),
			existingServiceModifier:  func(t *testing.T, ctx context.Context, cl client.Client, svc *corev1.Service) {}, // No-op
			expectedCreatedOrUpdated: op.Noop,
			expectedService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    "default",
					GenerateName: "dataplane-ingress-dp-0-",
					Labels: map[string]string{
						"app":                                "dp-0",
						consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,

						consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValuePreview,
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						"app": "dp-0",
					},
				},
			},
		},
		{
			name: "no existing service, should create",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(
				metav1.ObjectMeta{Namespace: "default", Name: "dp-1"},
			).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithPromotionStrategy(operatorv1beta1.AutomaticPromotion).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, cl client.Client, svc *corev1.Service) {
				require.NoError(t, dataplane.OwnedObjectPreDeleteHook(ctx, cl, svc))
				require.NoError(t, cl.Delete(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Created,
			expectedService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    "default",
					GenerateName: "dataplane-ingress-dp-1-",
					Labels: map[string]string{
						"app":                                "dp-1",
						consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,

						consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValuePreview,
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						"app": "dp-1",
					},
				},
			},
		},
		{
			name: "multiple services, should reduce service",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(
				metav1.ObjectMeta{Namespace: "default", Name: "dp-1"},
			).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithPromotionStrategy(operatorv1beta1.AutomaticPromotion).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, cl client.Client, svc *corev1.Service) {
				svcCopy := svc.DeepCopy()
				svcCopy.UID = ""
				svcCopy.ResourceVersion = ""
				svcCopy.Name = svc.Name + "-copy"
				require.NoError(t, cl.Create(ctx, svcCopy))
			},
			expectedErrorMessage: "number of DataPlane ingress services reduced",
		},
		{
			name: "existing service has different spec, should update",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(
				metav1.ObjectMeta{Namespace: "default", Name: "dp-1"},
			).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithPromotionStrategy(operatorv1beta1.AutomaticPromotion).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, cl client.Client, svc *corev1.Service) {
				svc.Spec.Selector["app"] = "dp-0"
				require.NoError(t, cl.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Updated,
			expectedService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    "default",
					GenerateName: "dataplane-ingress-dp-1-",
					Labels: map[string]string{
						"app":                                "dp-1",
						consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,

						consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValuePreview,
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Selector: map[string]string{
						"app": "dp-1",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.dataplane).
				WithStatusSubresource(tc.dataplane).
				Build()

			// generate an existing "preview ingress service" for the test dataplane.
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataPlane(tc.dataplane,
				func(svc *corev1.Service) {
					svc.Labels[consts.DataPlaneServiceStateLabel] = consts.DataPlaneStateLabelValuePreview
				})
			require.NoError(t, err)
			k8sutils.SetOwnerForObject(existingSvc, tc.dataplane)
			require.NoError(t, fakeClient.Create(ctx, existingSvc))
			// modify the existing service.
			tc.existingServiceModifier(t, ctx, fakeClient, existingSvc)

			reconciler := &Reconciler{
				Client: fakeClient,
			}

			bgReconciler := BlueGreenReconciler{
				Client:              fakeClient,
				DataPlaneController: reconciler,
			}

			res, svc, err := bgReconciler.ensurePreviewIngressService(ctx, logr.Discard(), tc.dataplane)
			if tc.expectedErrorMessage != "" {
				require.Error(t, err, "should return error")
				require.Contains(t, err.Error(), tc.expectedErrorMessage, "error message should contain expected content")
				return
			}

			require.NoError(t, err, "should not return error")
			require.Equal(t, tc.expectedCreatedOrUpdated, res, "should return expected result of created or updated")
			assert.Equal(t, tc.expectedService.GenerateName, svc.GenerateName, "should have expected GenerateName")
			assert.Equal(t, tc.expectedService.Labels, svc.Labels, "should have expected labels")
			assert.Equal(t, tc.expectedService.Annotations, svc.Annotations, "should have expected annotations")
			assert.Equal(t, tc.expectedService.Spec.Type, svc.Spec.Type, "should have expected service type")
			assert.Equal(t, tc.expectedService.Spec.Selector, svc.Spec.Selector, "should have expected selectors")
		})
	}
}
