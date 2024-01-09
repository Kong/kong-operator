package dataplane

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/builder"
	"github.com/kong/gateway-operator/controllers/pkg/dataplane"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

func TestCanProceedWithPromotion(t *testing.T) {
	testCases := []struct {
		name               string
		dataplane          v1beta1.DataPlane
		expectedCanProceed bool
		expectedErr        error
	}{
		{
			name: "AutomaticPromotion strategy",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(v1beta1.AutomaticPromotion).
				Build(),
			expectedCanProceed: true,
		},
		{
			name: "BreakBeforePromotion strategy, no annotation",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(v1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation false",
			dataplane: *builder.NewDataPlaneBuilder().
				WithObjectMeta(
					metav1.ObjectMeta{
						Annotations: map[string]string{
							v1beta1.DataPlanePromoteWhenReadyAnnotationKey: "false",
						},
					},
				).
				WithPromotionStrategy(v1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: false,
		},
		{
			name: "BreakBeforePromotion strategy, annotation true",
			dataplane: *builder.NewDataPlaneBuilder().
				WithObjectMeta(
					metav1.ObjectMeta{
						Annotations: map[string]string{
							v1beta1.DataPlanePromoteWhenReadyAnnotationKey: v1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
						},
					},
				).
				WithPromotionStrategy(v1beta1.BreakBeforePromotion).
				Build(),
			expectedCanProceed: true,
		},
		{
			name: "unknown strategy",
			dataplane: *builder.NewDataPlaneBuilder().
				WithPromotionStrategy(v1beta1.PromotionStrategy("unknown")).
				Build(),
			expectedErr: errors.New(`unknown promotion strategy: "unknown"`),
		},
	}

	for _, tc := range testCases {
		tc := tc
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
		dataplane                *v1beta1.DataPlane
		existingServiceModifier  func(*testing.T, context.Context, client.Client, *corev1.Service)
		expectedCreatedOrUpdated op.CreatedUpdatedOrNoop
		expectedService          *corev1.Service
		// expectedErrorMessage is empty if we expect no error, otherwise returned error must contain it.
		expectedErrorMessage string
	}{
		{
			name: "have existing service, should not update",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(
				metav1.ObjectMeta{Namespace: "default", Name: "dp-0"},
			).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithPromotionStrategy(v1beta1.AutomaticPromotion).Build(),
			existingServiceModifier:  func(t *testing.T, ctx context.Context, cl client.Client, svc *corev1.Service) {}, // No-op
			expectedCreatedOrUpdated: op.Noop,
			expectedService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:    "default",
					GenerateName: "dataplane-ingress-dp-0-",
					Labels: map[string]string{
						"app":                                "dp-0",
						consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
						consts.GatewayOperatorManagedByLabelLegacy: consts.DataPlaneManagedLabelValue,
						consts.DataPlaneServiceTypeLabel:           string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel:          consts.DataPlaneStateLabelValuePreview,
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
				WithPromotionStrategy(v1beta1.AutomaticPromotion).Build(),
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
						consts.GatewayOperatorManagedByLabelLegacy: consts.DataPlaneManagedLabelValue,
						consts.DataPlaneServiceTypeLabel:           string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel:          consts.DataPlaneStateLabelValuePreview,
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
				WithPromotionStrategy(v1beta1.AutomaticPromotion).Build(),
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
				WithPromotionStrategy(v1beta1.AutomaticPromotion).Build(),
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
						consts.GatewayOperatorManagedByLabelLegacy: consts.DataPlaneManagedLabelValue,
						consts.DataPlaneServiceTypeLabel:           string(consts.DataPlaneIngressServiceLabelValue),
						consts.DataPlaneServiceStateLabel:          consts.DataPlaneStateLabelValuePreview,
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.dataplane).
				WithStatusSubresource(tc.dataplane).
				Build()

			// generate an existing "preview ingress service" for the test dataplane.
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataplane(tc.dataplane,
				func(svc *corev1.Service) {
					svc.ObjectMeta.Labels[consts.DataPlaneServiceStateLabel] = consts.DataPlaneStateLabelValuePreview
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
