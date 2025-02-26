package konnect

import (
	"context"
	"errors"
	"reflect"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KonnectExtensionReconciler reconciles a KonnectExtension object.
type KonnectExtensionReconciler struct {
	client.Client
	developmentMode bool
	sdkFactory      sdkops.SDKFactory
}

// NewKonnectAPIAuthConfigurationReconciler creates a new KonnectAPIAuthConfigurationReconciler.
func NewKonnectExtensionReconciler(
	sdkFactory sdkops.SDKFactory,
	developmentMode bool,
	client client.Client,
) *KonnectExtensionReconciler {
	return &KonnectExtensionReconciler{
		Client:          client,
		sdkFactory:      sdkFactory,
		developmentMode: developmentMode,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectExtensionReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konnectv1alpha1.KonnectExtension{}).
		Watches(
			&operatorv1beta1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(r.listDataPlaneExtensionsReferenced),
		).
		Watches(
			&konnectv1alpha1.KonnectAPIAuthConfiguration{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueObjectsForKonnectAPIAuthConfiguration[konnectv1alpha1.KonnectExtensionList](
					mgr.GetClient(),
					IndexFieldKonnectExtensionOnAPIAuthConfiguration,
				),
			),
		).
		// TODO: watch secrets https://github.com/Kong/gateway-operator/issues/1210
		Complete(r)
}

// listDataPlaneExtensionsReferenced returns a list of all the KonnectExtensions referenced by the DataPlane object.
// Maximum one reference is expected.
func (r *KonnectExtensionReconciler) listDataPlaneExtensionsReferenced(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)
	dataPlane, ok := obj.(*operatorv1beta1.DataPlane)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "DataPlane", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	if len(dataPlane.Spec.Extensions) == 0 {
		return nil
	}

	recs := []reconcile.Request{}

	for _, ext := range dataPlane.Spec.Extensions {
		if ext.Group != operatorv1alpha1.SchemeGroupVersion.Group ||
			ext.Kind != konnectv1alpha1.KonnectExtensionKind {
			continue
		}
		namespace := dataPlane.Namespace
		if ext.Namespace != nil && *ext.Namespace != namespace {
			continue
		}
		recs = append(recs, reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: namespace,
				Name:      ext.Name,
			},
		})
	}
	return recs
}

// Reconcile reconciles a KonnectExtension object.
func (r *KonnectExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ext konnectv1alpha1.KonnectExtension
	if err := r.Client.Get(ctx, req.NamespacedName, &ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.GetLogger(ctx, konnectv1alpha1.KonnectExtensionKind, r.developmentMode)
	var dataPlaneList operatorv1beta1.DataPlaneList
	if err := r.List(ctx, &dataPlaneList, client.MatchingFields{
		index.KonnectExtensionIndex: client.ObjectKeyFromObject(&ext).String(),
	}); err != nil {
		return ctrl.Result{}, err
	}

	var updated bool
	switch len(dataPlaneList.Items) {
	case 0:
		updated = controllerutil.RemoveFinalizer(&ext, consts.DataPlaneExtensionFinalizer)
	default:
		updated = controllerutil.AddFinalizer(&ext, consts.DataPlaneExtensionFinalizer)
	}
	if updated {
		if err := r.Client.Update(ctx, &ext); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}

		log.Info(logger, "KonnectExtension finalizer updated")
	}

	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectExtensionReadyConditionType, &ext); !present ||
		(cond.Status == metav1.ConditionFalse && cond.Reason == konnectv1alpha1.KonnectExtensionReadyReasonPending) ||
		cond.ObservedGeneration != ext.GetGeneration() {
		if res, err := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			consts.ConditionType(konnectv1alpha1.KonnectExtensionReadyConditionType),
			metav1.ConditionFalse,
			consts.ConditionReason(konnectv1alpha1.KonnectExtensionReadyReasonProvisioning),
			"provisioning in progress",
		); err != nil || !res.IsZero() {
			return res, err
		}
	}

	apiAuthRef, err := getKonnectAPIAuthRefNN(ctx, r.Client, &ext)
	// returning an error here instead of setting status conditions, as no error is returned at all
	// once https://github.com/Kong/gateway-operator/issues/889#issue-2695605217 is implemented.
	if err != nil {
		return ctrl.Result{}, err
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err = r.Client.Get(ctx, apiAuthRef, &apiAuth)
	if requeue, res, retErr := handleAPIAuthStatusCondition(ctx, r.Client, &ext, apiAuth, err); requeue {
		return res, retErr
	}

	token, err := getTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	// NOTE: We need to create a new SDK instance for each reconciliation
	// because the token is retrieved in runtime through KonnectAPIAuthConfiguration.
	serverURL := ops.NewServerURL[*konnectv1alpha1.KonnectExtension](apiAuth.Spec.ServerURL)
	sdk := r.sdkFactory.NewKonnectSDK(
		serverURL.String(),
		sdkops.SDKToken(token),
	)

	cp, err := ops.GetControlPlaneByID(ctx, sdk.GetControlPlaneSDK(), *ext.Spec.ControlPlaneRef.KonnectID)
	if err != nil {
		_, err := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			consts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
			metav1.ConditionFalse,
			consts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonInvalid),
			err.Error(),
		)
		return ctrl.Result{}, err
	}
	if res, err := patch.StatusWithCondition(
		ctx, r.Client, &ext,
		consts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
		metav1.ConditionTrue,
		consts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonValid),
		"ControlPlaneRef is valid",
	); err != nil || !res.IsZero() {
		return res, err
	}

	secretRef, err := getCertificateSecretRef(ctx, r.Client, ext)
	if err != nil {
		_, err := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			consts.ConditionType(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType),
			metav1.ConditionFalse,
			consts.ConditionReason(konnectv1alpha1.DataPlaneCertificateProvisionedReasonRefNotFound),
			err.Error(),
		)
		return ctrl.Result{}, err
	}
	if res, err := patch.StatusWithCondition(
		ctx, r.Client, &ext,
		consts.ConditionType(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType),
		metav1.ConditionTrue,
		consts.ConditionReason(konnectv1alpha1.DataPlaneCertificateProvisionedReasonProvisioned),
		"DataPlane client certificate is provisioned",
	); err != nil || !res.IsZero() {
		return res, err
	}

	if ext.Status.Konnect == nil {
		ext.Status.Konnect = &konnectv1alpha1.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: cp.ID,
			ClusterType:    konnectClusterTypeToCRDClusterType(cp.Config.ClusterType),
			Endpoints: konnectv1alpha1.KonnectEndpoints{
				ControlPlaneEndpoint: cp.Config.ControlPlaneEndpoint,
				TelemetryEndpoint:    cp.Config.TelemetryEndpoint,
			},
		}
		ext.Status.DataPlaneClientAuth = &konnectv1alpha1.DataPlaneClientAuthStatus{
			CertificateSecretRef: &konnectv1alpha1.SecretRef{
				Name: secretRef.Name,
			},
		}
		if err := r.Client.Status().Update(ctx, &ext); err != nil {
			return ctrl.Result{}, err
		}
	}

	if res, err := patch.StatusWithCondition(
		ctx, r.Client, &ext,
		consts.ConditionType(konnectv1alpha1.KonnectExtensionReadyConditionType),
		metav1.ConditionTrue,
		consts.ConditionReason(konnectv1alpha1.KonnectExtensionReadyReasonReady),
		"KonnectExtension is ready",
	); err != nil || !res.IsZero() {
		return res, err
	}

	return ctrl.Result{}, nil
}

func getKonnectAPIAuthRefNN(_ context.Context, _ client.Client, ext *konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {
	// In case the KonnectConfiguration is not set, we fetch the KonnectGatewayControlPlane
	// and get the KonnectConfiguration from there. KonnectGatewayControlPlane reference and KonnectConfiguration
	// are mutually exclusive in the KonnectExtension API.
	if ext.Spec.KonnectConfiguration == nil {
		// TODO: https://github.com/Kong/gateway-operator/issues/889
		return types.NamespacedName{}, errors.New("KonnectGatewayControlPlane references not supported yet")
	}

	// TODO: handle cross namespace refs
	return types.NamespacedName{
		Namespace: ext.Namespace,
		Name:      ext.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
	}, nil
}

func getCertificateSecretRef(ctx context.Context, cl client.Client, ext konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {
	var certificateSecret corev1.Secret
	switch *ext.Spec.DataPlaneClientAuth.CertificateSecret.Provisioning {
	case konnectv1alpha1.ManualSecretProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		if err := cl.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.DataPlaneClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, &certificateSecret); err != nil {
			return types.NamespacedName{}, err
		}
	default:
		return types.NamespacedName{}, errors.New("automatic secret provisioning not supported yet")
	}
	return client.ObjectKeyFromObject(&certificateSecret), nil
}

func konnectClusterTypeToCRDClusterType(clusterType sdkkonnectcomp.ControlPlaneClusterType) konnectv1alpha1.KonnectExtensionClusterType {
	switch clusterType {
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane:
		return konnectv1alpha1.ClusterTypeControlPlane
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController:
		return konnectv1alpha1.ClusterTypeK8sIngressController
	default:
		// default never happens as the validation is at the CRD level
		return ""
	}
}
