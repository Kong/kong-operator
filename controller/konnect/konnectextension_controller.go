package konnect

import (
	"context"
	"errors"
	"strings"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/extensions"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
	konnectresource "github.com/kong/gateway-operator/pkg/utils/konnect/resources"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KonnectExtensionReconciler reconciles a KonnectExtension object.
type KonnectExtensionReconciler struct {
	client.Client
	developmentMode bool
	sdkFactory      sdkops.SDKFactory
	syncPeriod      time.Duration
}

// NewKonnectAPIAuthConfigurationReconciler creates a new KonnectAPIAuthConfigurationReconciler.
func NewKonnectExtensionReconciler(
	sdkFactory sdkops.SDKFactory,
	developmentMode bool,
	client client.Client,
	syncPeriod time.Duration,
) *KonnectExtensionReconciler {
	return &KonnectExtensionReconciler{
		Client:          client,
		sdkFactory:      sdkFactory,
		developmentMode: developmentMode,
		syncPeriod:      syncPeriod,
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
	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling")

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

	var updated, cleanup bool

	// if the extension is marked for deletion and no object is using it, we can proceed with the cleanup.
	if !ext.DeletionTimestamp.IsZero() &&
		ext.DeletionTimestamp.Before(lo.ToPtr(metav1.Now())) &&
		len(dataPlaneList.Items)+len(controlPlaneList.Items) == 0 {
		cleanup = true
	}

	switch {
	case len(dataPlaneList.Items)+len(controlPlaneList.Items) == 0:
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
		log.Debug(logger, "Extension-in-use finalizer changed on KonnectExtension")
		return ctrl.Result{}, nil
	}

	if !ext.DeletionTimestamp.IsZero() {
		if ext.DeletionTimestamp.After(time.Now()) {
			log.Debug(logger, "deletion still under grace period")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Until(ext.DeletionTimestamp.Time),
			}, nil
		}

		certificateSecret, err := getCertificateSecret(ctx, r.Client, ext)
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}

		// if the secret does not have the konnect-cleanup finalizer, the certificate has been properly cleaned up
		// in Konnect.
		if !controllerutil.ContainsFinalizer(certificateSecret, KonnectCleanupFinalizer) {
			// remove the secret-in-use finalizer from the secret.
			updated = controllerutil.RemoveFinalizer(certificateSecret, consts.SecretKonnectExtensionFinalizer)
			if updated {
				if err := r.Client.Update(ctx, certificateSecret); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, err
				}
				log.Debug(logger, "Secret-in-use finalizer removed from Secret")
			}
			// remove the konnect-cleanup finalizer from the KonnectExtension.
			updated = controllerutil.RemoveFinalizer(&ext, KonnectCleanupFinalizer)
			if updated {
				if err := r.Client.Update(ctx, &ext); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, err
				}
				log.Debug(logger, "Konnect-cleanup finalizer removed from KonnectExtension")
			}
			return ctrl.Result{}, nil
		}
	}

	// ready condition initialized as under provisioning
	readyCondition := metav1.Condition{
		Type:    konnectv1alpha1.KonnectExtensionReadyConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  konnectv1alpha1.KonnectExtensionReadyReasonProvisioning,
		Message: "provisioning in progress",
	}

	// if the konnectExtension is marked as pending, set it to provisioning
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectExtensionReadyConditionType, &ext); !present ||
		(cond.Status == metav1.ConditionFalse && cond.Reason == konnectv1alpha1.KonnectExtensionReadyReasonPending) ||
		cond.ObservedGeneration != ext.GetGeneration() {
		if res, updated, err := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			readyCondition,
		); err != nil || updated || !res.IsZero() {
			return res, err
		}
	}

	apiAuthRef, err := getKonnectAPIAuthRefNN(ctx, r.Client, &ext)

	if err != nil {
		return ctrl.Result{}, err
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err = r.Client.Get(ctx, apiAuthRef, &apiAuth)
	if requeue, res, retErr := handleAPIAuthStatusCondition(
		ctx,
		r.Client,
		&ext,
		apiAuth,
		err,
		readyCondition,
	); requeue {
		return res, retErr
	}

	apiAuthConfigValidCond := metav1.Condition{
		Type:    konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
		Message: "APIAuthConfiguration is valid",
	}

	token, err := getTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		apiAuthConfigValidCond.Status = metav1.ConditionFalse
		apiAuthConfigValidCond.Reason = konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid
		apiAuthConfigValidCond.Message = err.Error()
		if res, updated, errStatus := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			readyCondition,
			apiAuthConfigValidCond,
		); errStatus != nil || updated || !res.IsZero() {
			return res, errStatus
		}
		log.Debug(logger, "token retrieval failed")
		return ctrl.Result{}, err
	}

	log.Debug(logger, "API token retrieved from KonnectAPIAuthConfiguration", "apiAuthRef", apiAuth.Name)

	// NOTE: We need to create a new SDK instance for each reconciliation
	// because the token is retrieved in runtime through KonnectAPIAuthConfiguration.
	serverURL := ops.NewServerURL[*konnectv1alpha1.KonnectExtension](apiAuth.Spec.ServerURL)
	sdk := r.sdkFactory.NewKonnectSDK(
		serverURL.String(),
		sdkops.SDKToken(token),
	)

	// get the Konnect Control Plane
	cp, res, err := r.getKonnectControlPlane(ctx, logger, sdk.GetControlPlaneSDK(), ext)
	if err != nil || !res.IsZero() {
		return res, err
	}

	log.Debug(logger, "controlPlane reference validity checked")

	certProvisionedCond := metav1.Condition{
		Type:    konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.DataPlaneCertificateProvisionedReasonProvisioned,
		Message: "DataPlane client certificate is provisioned",
	}

	// get the Kubernetes secret holding the certificate.
	certificateSecret, err := getCertificateSecret(ctx, r.Client, ext)
	if err != nil {
		certProvisionedCond.Status = metav1.ConditionFalse
		certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonRefNotFound
		certProvisionedCond.Message = err.Error()
		if res, updated, err := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			readyCondition,
			certProvisionedCond,
		); err != nil || updated || !res.IsZero() {
			return res, err
		}
		log.Debug(logger, "certificate secret retrieval failed")
		return ctrl.Result{}, err
	}

	// check if the secret contains a valid tls certificate
	certData, ok := certificateSecret.Data[consts.TLSCRT]
	if !ok {
		certProvisionedCond.Status = metav1.ConditionFalse
		certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonInvalidSecret
		certProvisionedCond.Message = "the secret does not contain a valid tls secret"
		if res, updated, err := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			readyCondition,
			certProvisionedCond,
		); err != nil || updated || !res.IsZero() {
			return res, err
		}
		log.Debug(logger, "referenced secret malformed")
		return ctrl.Result{}, nil
	}

	// enforce a finalizer on the secret to prevent it from being deleted while in use.
	updated = controllerutil.AddFinalizer(certificateSecret, consts.SecretKonnectExtensionFinalizer) ||
		controllerutil.AddFinalizer(certificateSecret, KonnectCleanupFinalizer)
	if updated {
		if err := r.Client.Update(ctx, certificateSecret); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}

		log.Info(logger, "secret-in-use finalizer on the referenced secret updated")
		return ctrl.Result{}, nil
	}

	log.Debug(logger, "DataPlane certificate validity checked")

	// get the list of DataPlane client certificates in Konnect
	dpCertificates, err := ops.ListKongDataPlaneClientCertificates(ctx, sdk.GetDataPlaneCertificatesSDK(), cp.ID)
	if err != nil {
		certProvisionedCond.Status = metav1.ConditionFalse
		certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed
		certProvisionedCond.Message = err.Error()
		if res, updated, err := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			readyCondition,
			certProvisionedCond,
		); err != nil || updated || !res.IsZero() {
			return res, err
		}

		log.Debug(logger, "DataPlane client certificate list retrieval failed in Konnect")
		return ctrl.Result{RequeueAfter: r.syncPeriod}, err
	}

	var (
		cert      sdkkonnectcomp.DataPlaneClientCertificate
		certFound bool
	)
	// retrieve all the konnect certificates bound to this secret
	mappedIDs := lo.FilterMap(dpCertificates, func(c sdkkonnectcomp.DataPlaneClientCertificate, _ int) (k string, include bool) {
		if c.Cert != nil && c.ID != nil {
			certStr := sanitizeCert(*c.Cert)
			certDataStr := sanitizeCert(string(certData))
			if certStr == certDataStr {
				cert = c
				certFound = true
				return *c.ID, true
			}
		}
		return "", false
	})

	// update the secret annotation with the IDs of the mapped certificates
	newMappedIDsStr := strings.Join(mappedIDs, ",")
	if certificateSecret.Annotations[consts.DataPlaneCertificateIDAnnotationKey] != newMappedIDsStr {
		if certificateSecret.Annotations == nil {
			certificateSecret.Annotations = map[string]string{}
		}
		certificateSecret.Annotations[consts.DataPlaneCertificateIDAnnotationKey] = newMappedIDsStr
		if err := r.Client.Update(ctx, certificateSecret); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	switch {
	case !cleanup:
		if !certFound {
			log.Debug(logger, "DataPlane client certificate enforced in Konnect")
			dpCert := konnectresource.GenerateKongDataPlaneClientCertificate(
				certificateSecret.Name,
				certificateSecret.Namespace,
				&ext.Spec.KonnectControlPlane.ControlPlaneRef,
				string(certificateSecret.Data[consts.TLSCRT]),
				func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) {
					dpCert.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
						// setting the controlPlane ID in the status as a workaround for the GetControlPlaneID method,
						// that expects the ControlPlaneID to be set in the status.
						ControlPlaneID: cp.ID,
					}
				},
			)
			if err := ops.CreateKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), &dpCert); err != nil {
				certProvisionedCond.Status = metav1.ConditionFalse
				certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed
				certProvisionedCond.Message = err.Error()
				if res, updated, err := patch.StatusWithConditions(
					ctx,
					r.Client,
					&ext,
					readyCondition,
					certProvisionedCond,
				); err != nil || updated || !res.IsZero() {
					return res, err
				}
				return ctrl.Result{RequeueAfter: r.syncPeriod}, err
			}
		}
		updated = controllerutil.AddFinalizer(&ext, KonnectCleanupFinalizer)
		if updated {
			if err := r.Client.Update(ctx, &ext); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
			log.Info(logger, "konnectCleanup finalizer addded on konnectExtension")
			return ctrl.Result{}, nil
		}
	case cleanup:
		if certFound {
			// This should never happen, but checking to make the dereference below bullet-proof
			if cert.ID == nil {
				return ctrl.Result{}, errors.New("cannot cleanup certificate in Konnect without ID")
			}
			dpCert := konnectresource.GenerateKongDataPlaneClientCertificate(
				certificateSecret.Name,
				certificateSecret.Namespace,
				&ext.Spec.KonnectControlPlane.ControlPlaneRef,
				string(certificateSecret.Data[consts.TLSCRT]),
				func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) {
					dpCert.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
						// setting the controlPlane ID in the status as a workaround for the GetControlPlaneID method,
						// that expects the ControlPlaneID to be set in the status.
						ControlPlaneID: cp.ID,
						// setting the ID in the status as a workaround for the DeleteKongDataPlaneClientCertificate method,
						// that expects the ID to be set in the status.
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: *cert.ID,
						},
					}
				},
			)
			if err := ops.DeleteKongDataPlaneClientCertificate(ctx, sdk.GetDataPlaneCertificatesSDK(), &dpCert); err != nil {
				certProvisionedCond.Status = metav1.ConditionFalse
				certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonKonnectAPIOpFailed
				certProvisionedCond.Message = err.Error()
				if res, updated, err := patch.StatusWithConditions(
					ctx,
					r.Client,
					&ext,
					readyCondition,
					certProvisionedCond,
				); err != nil || updated || !res.IsZero() {
					return res, err
				}
				// In case of an error in the Konnect ops, the resync period will take care of a new creation attempt.
				return ctrl.Result{RequeueAfter: r.syncPeriod}, err
			}
		}

		// in case no IDs are mapped to the secret, we can remove the finalizer from the secret.
		if len(mappedIDs) == 0 {
			updated = controllerutil.RemoveFinalizer(certificateSecret, KonnectCleanupFinalizer)
			if updated {
				if err := r.Client.Update(ctx, certificateSecret); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, err
				}
				log.Info(logger, "Secret finalizer removed")
			}
			log.Debug(logger, "DataPlane client certificate Deleted in Konnect")
			return ctrl.Result{Requeue: true}, nil
		}
	}
	// set the certificateProvisioned condition to true
	if res, updated, err := patch.StatusWithConditions(
		ctx,
		r.Client,
		&ext,
		certProvisionedCond,
	); err != nil || updated || !res.IsZero() {
		return res, err
	}

	var updateExtensionStatus bool
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
				Name: certificateSecret.Name,
			},
		}
		updateExtensionStatus = true
	}

	if ext.Status.DataPlaneClientAuth == nil ||
		ext.Status.DataPlaneClientAuth.CertificateSecretRef == nil ||
		ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name != certificateSecret.Name {
		ext.Status.DataPlaneClientAuth = &konnectv1alpha1.DataPlaneClientAuthStatus{
			CertificateSecretRef: &konnectv1alpha1.SecretRef{
				Name: certificateSecret.Name,
			},
		}
		updateExtensionStatus = true
	}
	if updateExtensionStatus {
		log.Debug(logger, "updating KonnectExtension status")
		return ctrl.Result{}, r.Client.Status().Update(ctx, &ext)
	}

	readyCondition = metav1.Condition{
		Type:    konnectv1alpha1.KonnectExtensionReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.KonnectExtensionReadyReasonReady,
		Message: "KonnectExtension is ready",
	}

	if res, updated, err := patch.StatusWithConditions(
		ctx,
		r.Client,
		&ext,
		readyCondition,
	); err != nil || updated || !res.IsZero() {
		return res, err
	}

	log.Debug(logger, "reconciled")
	return ctrl.Result{
		RequeueAfter: r.syncPeriod,
	}, nil
}
