package konnect

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/extensions"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
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
	ls := metav1.LabelSelector{
		// A secret must have `konghq.com/konnect-dp-cert` label to be watched by the controller.
		// This constraint is added to prevent from watching all secrets which may cause high resource consumption.
		// TODO: https://github.com/Kong/gateway-operator/issues/1255 set label constraints of `Secret`s on manager level if possible.
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      SecretKonnectDataPlaneCertificateLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(ls)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&konnectv1alpha1.KonnectExtension{}).
		Watches(
			&operatorv1beta1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(listExtendableReferencedExtensions[*operatorv1beta1.DataPlane]()),
		).
		Watches(
			&operatorv1beta1.ControlPlane{},
			handler.EnqueueRequestsFromMapFunc(listExtendableReferencedExtensions[*operatorv1beta1.ControlPlane]()),
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
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueKonnectExtensionsForSecret(mgr.GetClient()),
			),
			builder.WithPredicates(
				labelSelectorPredicate,
			),
		).
		Watches(
			&konnectv1alpha1.KonnectGatewayControlPlane{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueKonnectExtensionsForKonnectGatewayControlPlane(mgr.GetClient()),
			),
		).
		Complete(r)
}

// listExtendableReferencedExtensions returns a list of all the KonnectExtensions referenced by the Extendable object.
// Maximum one reference is expected.
func listExtendableReferencedExtensions[t extensions.ExtendableT]() func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		o := obj.(t)
		if len(o.GetExtensions()) == 0 {
			return nil
		}

		recs := []reconcile.Request{}

		for _, ext := range o.GetExtensions() {
			if ext.Group != operatorv1alpha1.SchemeGroupVersion.Group ||
				ext.Kind != konnectv1alpha1.KonnectExtensionKind {
				continue
			}
			namespace := obj.GetNamespace()
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
}

// Reconcile reconciles a KonnectExtension object.
func (r *KonnectExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ext konnectv1alpha1.KonnectExtension
	if err := r.Client.Get(ctx, req.NamespacedName, &ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.GetLogger(ctx, konnectv1alpha1.KonnectExtensionKind, r.developmentMode)

	var (
		dataPlaneList    operatorv1beta1.DataPlaneList
		controlPlaneList operatorv1beta1.ControlPlaneList
	)
	if err := r.List(ctx, &dataPlaneList, client.MatchingFields{
		index.KonnectExtensionIndex: client.ObjectKeyFromObject(&ext).String(),
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.List(ctx, &controlPlaneList, client.MatchingFields{
		index.KonnectExtensionIndex: client.ObjectKeyFromObject(&ext).String(),
	}); err != nil {
		return ctrl.Result{}, err
	}

	var updated bool
	switch len(dataPlaneList.Items) + len(controlPlaneList.Items) {
	case 0:
		updated = controllerutil.RemoveFinalizer(&ext, consts.ExtensionInUseFinalizer)
	default:
		updated = controllerutil.AddFinalizer(&ext, consts.ExtensionInUseFinalizer)
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
			kcfgconsts.ConditionType(konnectv1alpha1.KonnectExtensionReadyConditionType),
			metav1.ConditionFalse,
			kcfgconsts.ConditionReason(konnectv1alpha1.KonnectExtensionReadyReasonProvisioning),
			"provisioning in progress",
		); err != nil || !res.IsZero() {
			return res, err
		}
	}

	apiAuthRef, err := getKonnectAPIAuthRefNN(ctx, r.Client, &ext)

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

	var konnectCPID string
	switch ext.Spec.KonnectControlPlane.ControlPlaneRef.Type {
	case commonv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		cpRef := ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef
		cpNamepace := ext.Namespace
		// TODO: get namespace from cpRef.Namespace when allowed to reference CP from another namespace.
		kgcp := &konnectv1alpha1.KonnectGatewayControlPlane{}
		err := r.Client.Get(ctx, client.ObjectKey{
			Namespace: cpNamepace,
			Name:      cpRef.Name,
		}, kgcp)

		if err != nil {
			_, errPatch := patch.StatusWithCondition(
				ctx, r.Client, &ext,
				kcfgconsts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
				metav1.ConditionFalse,
				kcfgconsts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonInvalid),
				err.Error(),
			)
			return ctrl.Result{}, errPatch
		}

		if !lo.ContainsBy(kgcp.Status.Conditions, func(cond metav1.Condition) bool {
			return cond.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				cond.Status == metav1.ConditionTrue
		}) {
			_, errPatch := patch.StatusWithCondition(
				ctx, r.Client, &ext,
				kcfgconsts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
				metav1.ConditionFalse,
				kcfgconsts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonInvalid),
				fmt.Sprintf("Konnect control plane %s/%s not programmed yet", cpNamepace, cpRef.Name),
			)
			// Update of KonnectGatewayControlPlane will trigger reconciliation of KonnectExtension.
			return ctrl.Result{}, errPatch
		}
		konnectCPID = kgcp.GetKonnectID()
	case commonv1alpha1.ControlPlaneRefKonnectID:
		konnectCPID = *ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectID
	}

	cp, err := ops.GetControlPlaneByID(ctx, sdk.GetControlPlaneSDK(), konnectCPID)
	if err != nil {
		_, err := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			kcfgconsts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
			metav1.ConditionFalse,
			kcfgconsts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonInvalid),
			err.Error(),
		)
		return ctrl.Result{}, err
	}
	if res, err := patch.StatusWithCondition(
		ctx, r.Client, &ext,
		kcfgconsts.ConditionType(konnectv1alpha1.ControlPlaneRefValidConditionType),
		metav1.ConditionTrue,
		kcfgconsts.ConditionReason(konnectv1alpha1.ControlPlaneRefReasonValid),
		"ControlPlaneRef is valid",
	); err != nil || !res.IsZero() {
		return res, err
	}

	secretRef, err := getCertificateSecretRef(ctx, r.Client, ext)
	if err != nil {
		_, err := patch.StatusWithCondition(
			ctx, r.Client, &ext,
			kcfgconsts.ConditionType(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType),
			metav1.ConditionFalse,
			kcfgconsts.ConditionReason(konnectv1alpha1.DataPlaneCertificateProvisionedReasonRefNotFound),
			err.Error(),
		)
		return ctrl.Result{}, err
	}
	if res, err := patch.StatusWithCondition(
		ctx, r.Client, &ext,
		kcfgconsts.ConditionType(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType),
		metav1.ConditionTrue,
		kcfgconsts.ConditionReason(konnectv1alpha1.DataPlaneCertificateProvisionedReasonProvisioned),
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
		kcfgconsts.ConditionType(konnectv1alpha1.KonnectExtensionReadyConditionType),
		metav1.ConditionTrue,
		kcfgconsts.ConditionReason(konnectv1alpha1.KonnectExtensionReadyReasonReady),
		"KonnectExtension is ready",
	); err != nil || !res.IsZero() {
		return res, err
	}

	return ctrl.Result{}, nil
}

func getKonnectAPIAuthRefNN(ctx context.Context, cl client.Client, ext *konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {

	if ext.Spec.KonnectConfiguration != nil {
		// TODO: handle cross namespace refs when supported.
		return types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		}, nil
	}

	// In case the KonnectConfiguration is not set, we fetch the KonnectGatewayControlPlane
	// and get the KonnectConfiguration from `spec.konnectControlPlane.controlPlane.konnectNamespacedRef`.
	// KonnectGatewayControlPlane reference and KonnectConfiguration
	// are mutually exclusive in the KonnectExtension API.
	cpRef := ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef
	kgcp := &konnectv1alpha1.KonnectGatewayControlPlane{}
	err := cl.Get(ctx, client.ObjectKey{
		// TODO: handle cross namespace refs to KonnectGatewayControlPlane when referencing CP from another namespace is supported.
		Namespace: ext.Namespace,
		Name:      cpRef.Name,
	}, kgcp)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{
		Namespace: kgcp.Namespace,
		Name:      kgcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
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
