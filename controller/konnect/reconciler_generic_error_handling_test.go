package konnect

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestHandleOpsErr(t *testing.T) {
	tests := []struct {
		name                   string
		inputErr               *url.Error
		expectedResult         ctrl.Result
		expectedErr            bool
		expectedErrMsg         string
		expectConditionPatched bool
		interceptorFunc        interceptor.Funcs
	}{
		{
			name:           "nil error returns empty result",
			inputErr:       nil,
			expectedResult: ctrl.Result{},
			expectedErr:    false,
		},
		{
			name: "url.Error patches status condition and returns nil error",
			inputErr: &url.Error{
				Op:  "Get",
				URL: "https://api.konghq.com",
				Err: errors.New("connection refused"),
			},
			expectedResult:         ctrl.Result{RequeueAfter: consts.RequeueWithoutBackoff},
			expectedErr:            false,
			expectConditionPatched: true,
		},
		{
			name: "url.Error with patch conflict returns requeue",
			inputErr: &url.Error{
				Op:  "Post",
				URL: "https://api.konghq.com",
				Err: errors.New("timeout"),
			},
			expectedResult: ctrl.Result{Requeue: true},
			expectedErr:    false,
			interceptorFunc: interceptor.Funcs{
				SubResourcePatch: func(
					ctx context.Context,
					client client.Client,
					subResourceName string,
					obj client.Object,
					patch client.Patch,
					opts ...client.SubResourcePatchOption,
				) error {
					return &apierrors.StatusError{
						ErrStatus: metav1.Status{
							Status: metav1.StatusFailure,
							Reason: metav1.StatusReasonConflict,
						},
					}
				},
			},
		},
		{
			name: "wrapped url.Error is handled correctly",
			inputErr: &url.Error{
				Op:  "Get",
				URL: "https://api.konghq.com",
				Err: errors.New("no such host"),
			},
			expectedResult:         ctrl.Result{RequeueAfter: consts.RequeueWithoutBackoff},
			expectedErr:            false,
			expectConditionPatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			ent := &configurationv1alpha1.KongService{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-service",
					Namespace:  "default",
					Generation: 1,
				},
			}

			clientBuilder := fake.NewClientBuilder().
				WithObjects(ent).
				WithStatusSubresource(ent).
				WithScheme(scheme.Get())

			if tt.interceptorFunc.SubResourcePatch != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(tt.interceptorFunc)
			}

			cl := clientBuilder.Build()

			r := &KonnectEntityReconciler[
				configurationv1alpha1.KongService, *configurationv1alpha1.KongService,
			]{
				Client: cl,
			}

			result, err := r.handleOpsErr(ctx, ent, tt.inputErr)

			assert.Equal(t, tt.expectedResult, result)

			if tt.expectedErr {
				require.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				return
			}

			require.NoError(t, err)

			if tt.expectConditionPatched {
				cond, ok := k8sutils.GetCondition(
					konnectv1alpha1.KonnectEntityProgrammedConditionType, ent,
				)
				require.True(t, ok, "expected condition to be set")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t,
					string(konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed),
					cond.Reason,
				)
			}
		})
	}
}

func TestReconcile_RemovesFinalizerWhenDeletingKongCertificateParentControlPlaneIsDeleting(t *testing.T) {
	ctx := context.Background()

	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cp-deleting",
			Namespace:         "default",
			DeletionTimestamp: lo.ToPtr(metav1.Now()),
			Finalizers:        []string{"gateway.konghq.com/konnect-cleanup"},
		},
	}

	certificate := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cert-deleting",
			Namespace:         "default",
			DeletionTimestamp: lo.ToPtr(metav1.Now()),
			Finalizers:        []string{KonnectCleanupFinalizer},
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(certificate, cp).
		WithStatusSubresource(certificate).
		Build()

	r := &KonnectEntityReconciler[configurationv1alpha1.KongCertificate, *configurationv1alpha1.KongCertificate]{
		Client: cl,
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(certificate)})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	updated := &configurationv1alpha1.KongCertificate{}
	err = cl.Get(ctx, client.ObjectKeyFromObject(certificate), updated)
	if apierrors.IsNotFound(err) {
		return
	}
	require.NoError(t, err)
	assert.NotContains(t, updated.GetFinalizers(), KonnectCleanupFinalizer)
}

func TestReconcile_RemovesFinalizerWhenDeletingKongSNIReferencedCertificateIsDeleting(t *testing.T) {
	ctx := context.Background()

	certificate := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cert-deleting",
			Namespace:         "default",
			DeletionTimestamp: lo.ToPtr(metav1.Now()),
			Finalizers:        []string{"gateway.konghq.com/konnect-cleanup"},
		},
	}

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "sni-deleting",
			Namespace:         "default",
			DeletionTimestamp: lo.ToPtr(metav1.Now()),
			Finalizers:        []string{KonnectCleanupFinalizer},
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: commonv1alpha1.NameRef{Name: certificate.Name},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(sni, certificate).
		WithStatusSubresource(sni).
		Build()

	r := &KonnectEntityReconciler[configurationv1alpha1.KongSNI, *configurationv1alpha1.KongSNI]{
		Client: cl,
	}

	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sni)})
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, res)

	updated := &configurationv1alpha1.KongSNI{}
	err = cl.Get(ctx, client.ObjectKeyFromObject(sni), updated)
	if apierrors.IsNotFound(err) {
		return
	}
	require.NoError(t, err)
	assert.NotContains(t, updated.GetFinalizers(), KonnectCleanupFinalizer)
}
