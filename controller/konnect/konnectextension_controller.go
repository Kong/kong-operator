package konnect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/extensions"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	konnectresource "github.com/kong/kong-operator/v2/pkg/utils/konnect/resources"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// KonnectExtensionReconciler reconciles a KonnectExtension object.
type KonnectExtensionReconciler struct {
	client.Client

	ControllerOptions        controller.Options
	LoggingMode              logging.Mode
	SdkFactory               sdkops.SDKFactory
	SyncPeriod               time.Duration
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectExtensionReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	ls := metav1.LabelSelector{
		// A secret must have `konghq.com/konnect-dp-cert` label to be watched by the controller.
		// This constraint is added to prevent from watching all secrets which may cause high resource consumption.
		// TODO: https://github.com/kong/kong-operator/issues/1255 set label constraints of `Secret`s on manager level if possible.
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
		For(&konnectv1alpha2.KonnectExtension{}).
		WithOptions(r.ControllerOptions).
		Watches(
			&operatorv1beta1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(listExtendableReferencedExtensions[*operatorv1beta1.DataPlane]),
		).
		Watches(
			&gwtypes.ControlPlane{},
			handler.EnqueueRequestsFromMapFunc(listExtendableReferencedExtensions[*gwtypes.ControlPlane]),
		).
		Watches(
			&konnectv1alpha1.KonnectAPIAuthConfiguration{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueObjectsForKonnectAPIAuthConfiguration[konnectv1alpha2.KonnectExtensionList](
					mgr.GetClient(),
					index.IndexFieldKonnectExtensionOnAPIAuthConfiguration,
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
			&konnectv1alpha2.KonnectGatewayControlPlane{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueKonnectExtensionsForKonnectGatewayControlPlane(mgr.GetClient()),
			),
		).
		Watches(
			&configurationv1alpha1.KongDataPlaneClientCertificate{},
			handler.EnqueueRequestForOwner(r.Scheme(), mgr.GetRESTMapper(), &konnectv1alpha2.KonnectExtension{}),
		).
		Complete(r)
}

// listExtendableReferencedExtensions returns a list of all the KonnectExtensions referenced by the Extendable object.
// Maximum one reference is expected.
func listExtendableReferencedExtensions[t extensions.ExtendableT](_ context.Context, obj client.Object) []reconcile.Request {
	o := obj.(t)
	if len(o.GetExtensions()) == 0 {
		return nil
	}

	recs := []reconcile.Request{}

	for _, ext := range o.GetExtensions() {
		if ext.Group != konnectv1alpha2.SchemeGroupVersion.Group ||
			ext.Kind != konnectv1alpha2.KonnectExtensionKind {
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

// Reconcile reconciles a KonnectExtension object.
func (r *KonnectExtensionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ext konnectv1alpha2.KonnectExtension
	if err := r.Get(ctx, req.NamespacedName, &ext); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.GetLogger(ctx, konnectv1alpha2.KonnectExtensionKind, r.LoggingMode)

	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling")

	var (
		dataPlaneList    operatorv1beta1.DataPlaneList
		controlPlaneList gwtypes.ControlPlaneList
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

	var updated, cleanup bool

	// If the extension is marked for deletion and no object is using it, we can proceed with the cleanup.
	if !ext.DeletionTimestamp.IsZero() &&
		ext.DeletionTimestamp.Before(new(metav1.Now())) &&
		len(dataPlaneList.Items)+len(controlPlaneList.Items) == 0 {
		cleanup = true
	}

	var isFinalizerToBeRemoved bool
	switch {
	case len(dataPlaneList.Items)+len(controlPlaneList.Items) == 0:
		updated = controllerutil.RemoveFinalizer(&ext, consts.ExtensionInUseFinalizer)
		isFinalizerToBeRemoved = true
	default:
		updated = controllerutil.AddFinalizer(&ext, consts.ExtensionInUseFinalizer)
	}
	if updated {
		if err := r.Update(ctx, &ext); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			// in case the finalizer removal fails because the resource does not exist, ignore the error.
			if isFinalizerToBeRemoved && apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
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

		res, certificateSecret, err := r.getCertificateSecret(ctx, ext, true)
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		if res != op.Noop {
			return ctrl.Result{}, nil
		}

		certExists := !apierrors.IsNotFound(err)
		// if the certificate exists and the cleanup in Konnect has been performed, we can remove the secret-in-use finalizer from the secret.
		if certExists && !controllerutil.ContainsFinalizer(certificateSecret, KonnectCleanupFinalizer) {
			// remove the secret-in-use finalizer from the secret.
			if op, res, err := enforceSecretInUseFinalizer(ctx, r.Client, certificateSecret, logger, SecretInUseEnforceRemove); err != nil || !res.IsZero() || op {
				return res, err
			}
		}

		// if the certificate does not exist, or the cleanup in Konnect has been performed, we can remove the konnect-cleanup finalizer from the konnectExtension.
		if !certExists || ext.Status.Konnect == nil || !controllerutil.ContainsFinalizer(certificateSecret, KonnectCleanupFinalizer) {
			// remove the konnect-cleanup finalizer from the KonnectExtension.
			updated = controllerutil.RemoveFinalizer(&ext, KonnectCleanupFinalizer)
			if updated {
				if err := r.Update(ctx, &ext); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, err
				}
				log.Debug(logger, "Konnect-cleanup finalizer removed from KonnectExtension")
				return ctrl.Result{}, nil
			}
		}
	}

	// ready condition initialized as under provisioning
	readyCondition := metav1.Condition{
		Type:    konnectv1alpha2.KonnectExtensionReadyConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  konnectv1alpha2.KonnectExtensionReadyReasonProvisioning,
		Message: "provisioning in progress",
	}

	// if the konnectExtension is marked as pending, set it to provisioning
	if cond, present := k8sutils.GetCondition(konnectv1alpha2.KonnectExtensionReadyConditionType, &ext); !present ||
		(cond.Status == metav1.ConditionFalse && cond.Reason == konnectv1alpha2.KonnectExtensionReadyReasonPending) ||
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

	certProvisionedCond := metav1.Condition{
		Type:    konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  konnectv1alpha1.DataPlaneCertificateProvisionedReasonProvisioning,
		Message: "DataPlane client certificate is provisioning",
	}
	// get the Kubernetes secret holding the certificate.
	opRes, certificateSecret, err := r.getCertificateSecret(ctx, ext, false)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}
	if opRes != op.Noop {
		return ctrl.Result{}, nil
	}
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

	log.Debug(logger, "DataPlane certificate validity checked")

	// Get the GatewayKonnectControlPlane and set conditions accordingly, also
	// obtain valid ControlPlane ID or requeue. When KonnectExtension is during deletion,
	// we proceed with the cleanup even if the ControlPlane is not found.
	cpID := lo.FromPtr(ext.Status.Konnect).ControlPlaneID
	cp, res, err := r.getGatewayKonnectControlPlane(ctx, ext)
	if err != nil || !res.IsZero() {
		switch {
		case !apierrors.IsNotFound(err) && !errors.Is(err, extensionserrors.ErrKonnectGatewayControlPlaneNotProgrammed):
			return res, err
		case apierrors.IsNotFound(err):
			if cleanup {
				log.Debug(logger, "ControlPlane not found, for KonnectExtension during deletion, proceeding with cleanup")
			}

			// When the referenced ControlPlane is not found, we need to cleanup
			// all the KongDataPlaneClientCertificates referencing it as they don't exist
			// in Konnect anymore.
			var dpCerts configurationv1alpha1.KongDataPlaneClientCertificateList
			err = r.List(ctx, &dpCerts,
				client.InNamespace(ext.Namespace),
				client.MatchingFields{
					index.IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner: ext.Name,
				},
			)
			if err != nil {
				log.Debug(logger, "Couldn't delete all KongDataPlaneClientCertificates referencing not existing ControlPlane", "error", err)
				// Continue with cleanup.
			} else {
				for _, dp := range dpCerts.Items {
					if err := r.Delete(ctx, &dp); client.IgnoreNotFound(err) != nil {
						log.Debug(logger,
							"Couldn't delete KongDataPlaneClientCertificate during ControlPlane not found cleanup",
							"dataPlaneClientCertificate", client.ObjectKeyFromObject(&dp),
							"error", err)
					}
				}
			}

			// Removed the secret in use finalizer from Secret as the ControlPlane so any
			// certificate using this Secret has been already removed from Konnect along with the ControlPlane.
			if op, res, err := enforceSecretInUseFinalizer(ctx, r.Client, certificateSecret, logger, SecretInUseEnforceRemove); err != nil || !res.IsZero() || op {
				return res, err
			}

			if !cleanup {
				// ControlPlane not found and we're not in cleanup mode.
				// The controlPlaneRefValid condition has already been set to false in getGatewayKonnectControlPlane.
				// We've done the necessary cleanup above, now wait for the CP to be created or the reference to be corrected.
				log.Debug(logger, "ControlPlane not found, waiting for it to be available")
				return res, nil
			}

		default:
			log.Debug(logger, "ControlPlane not ready yet")
			return res, nil
		}
	} else {
		// Update the controlPlane ID to the value presented in the status by ControlPlane itself.
		cpID = cp.Status.ID

		if op, res, err := enforceSecretInUseFinalizer(ctx, r.Client, certificateSecret, logger, SecretInUseEnforceAdd); err != nil || !res.IsZero() || op {
			return res, err
		}
	}

	log.Debug(logger, "controlPlane reference validity checked")

	apiAuthRef, err := getKonnectAPIAuthRefNN(cp, &ext)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		if cleanup {
			// In case if KonnectExtension is during deletion and respective KonnectGatewayControlPlane
			// has been already deleted, take apiAuthRef from the status, because it contains the last
			// known reference and it is needed to perform all reconciliation steps.
			apiAuthRef = types.NamespacedName{
				Name: ext.Status.Konnect.AuthRef.Name,
				// ext.Status.Konnect.AuthRef.Namespace is never nil as enforced in the status update.
				Namespace: *ext.Status.Konnect.AuthRef.Namespace,
			}
		} else {
			// Requeue until the reference becomes valid.
			return ctrl.Result{}, nil
		}
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err = r.Get(ctx, apiAuthRef, &apiAuth)
	if requeue, res, retErr := handleAPIAuthStatusCondition(ctx, r.Client, &ext, apiAuth, apiAuthRef, err, readyCondition); requeue {
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
	// TODO: remove SDK usage in https://github.com/Kong/kong-operator/issues/2630
	server, err := server.NewServer[*konnectv1alpha2.KonnectExtension](apiAuth.Spec.ServerURL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse server URL: %w", err)
	}
	sdk := r.SdkFactory.NewKonnectSDK(server, sdkops.SDKToken(token))

	// Get the list of in cluster DataPlane client certificates.
	var dpCertificates configurationv1alpha1.KongDataPlaneClientCertificateList
	err = r.List(ctx, &dpCertificates,
		client.InNamespace(ext.Namespace),
		client.MatchingFields{
			index.IndexFieldKongDataPlaneClientCertificateOnKonnectExtensionOwner: ext.Name,
		},
	)
	if err != nil && !ops.ErrIsNotFound(err) {
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

		log.Debug(logger, "DataPlane client certificate list retrieval failed in Konnect", "error", err.Error())
		return ctrl.Result{RequeueAfter: r.SyncPeriod}, err
	}

	var (
		cert        configurationv1alpha1.KongDataPlaneClientCertificate
		certFound   bool
		certDataStr = sanitizeCert(string(certData))
	)
	// retrieve all the konnect certificates bound to this secret
	mappedIDs := lo.FilterMap(dpCertificates.Items, func(c configurationv1alpha1.KongDataPlaneClientCertificate, _ int) (k string, include bool) {
		if c.Spec.Cert != "" && c.GetKonnectID() != "" {
			certStr := sanitizeCert(c.Spec.Cert)
			if certStr == certDataStr {
				cert = c
				certFound = true
				return c.GetKonnectID(), true
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
		if err := r.Update(ctx, certificateSecret); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	var (
		certSDK      sdkkonnectcomp.DataPlaneClientCertificate
		certSDKFound bool
	)

	// If there are no mapped IDs from KongDataPlaneClientCertificates in cluster,
	// then let's use the SDK to query Konnect directly to find any existing certificates.
	// This is just to make sure that users migrating from older versions of the operator
	// where the dataplane client certificates were not managed using
	// KongDataPlaneClientCertificate CRs don't end up creating duplicate
	// certificates in Konnect.
	// TODO: https://github.com/Kong/kong-operator/issues/2630
	// remove this block in future major release after several operator releases.
	if len(mappedIDs) == 0 {
		// Get the list of DataPlane client certificates in Konnect.
		dpCertificates, err := ops.ListKongDataPlaneClientCertificates(ctx, sdk.GetDataPlaneCertificatesSDK(), cpID)
		if err != nil && !ops.ErrIsNotFound(err) {
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
			return ctrl.Result{RequeueAfter: r.SyncPeriod}, nil
		}

		// retrieve all the konnect certificates bound to this secret
		mappedIDs = lo.FilterMap(dpCertificates, func(c sdkkonnectcomp.DataPlaneClientCertificate, _ int) (k string, include bool) {
			if c.Cert != nil && c.ID != nil {
				certStr := sanitizeCert(*c.Cert)
				certDataStr := sanitizeCert(string(certData))
				if certStr == certDataStr {
					certSDK = c
					certSDKFound = true
					return *c.ID, true
				}
			}
			return "", false
		})
	}

	switch {
	case !cleanup:

		if !certFound && !certSDKFound {
			log.Debug(logger, "Creating KongDataPlaneClientCertificate custom resource")
			dpCert := konnectresource.GenerateKongDataPlaneClientCertificate(
				certificateSecret.Name,
				certificateSecret.Namespace,
				&ext.Spec.Konnect.ControlPlane.Ref,
				string(certificateSecret.Data[consts.TLSCRT]),
				&ext,
				func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) {
					dpCert.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						// setting the controlPlane ID in the status as a workaround for the GetControlPlaneID method,
						// that expects the ControlPlaneID to be set in the status.
						ControlPlaneID: cpID,
					}
				},
			)

			err := controllerutil.SetOwnerReference(&ext, &dpCert, r.Scheme(), controllerutil.WithBlockOwnerDeletion(true))
			if err != nil {
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, &dpCert); err != nil {
				if errS, ok := errors.AsType[*apierrors.StatusError](err); ok {
					if errS.ErrStatus.Reason == metav1.StatusReasonAlreadyExists {
						log.Debug(logger, "DataPlane client certificate already exists", "name", dpCert.Name, "namespace", dpCert.Namespace)
					}
				} else {
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
					// In case of an error when creating the object in Konnect, we requeue "immediately"
					// so that the operation is retried without waiting for the resync period.
					return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
			}
			updated, res, err := patch.WithFinalizer(ctx, r.Client, certificateSecret, KonnectCleanupFinalizer)
			if err != nil || !res.IsZero() {
				return res, err
			}
			if updated {
				log.Info(logger, "konnect-cleanup finalizer on the referenced secret updated")
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
		} else if certSDKFound && !certFound {
			// In case the certificate exists in Konnect but not in cluster,
			// we create the KongDataPlaneClientCertificate resource in cluster
			// to adopt the existing certificate in Konnect.
			dpCert := konnectresource.GenerateKongDataPlaneClientCertificate(
				certificateSecret.Name,
				certificateSecret.Namespace,
				&ext.Spec.Konnect.ControlPlane.Ref,
				string(certificateSecret.Data[consts.TLSCRT]),
				&ext,
				func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) {
					dpCert.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						// setting the controlPlane ID in the status as a workaround for the GetControlPlaneID method,
						// that expects the ControlPlaneID to be set in the status.
						ControlPlaneID: cpID,
					}
					dpCert.Spec.Adopt = &commonv1alpha1.AdoptOptions{
						From: commonv1alpha1.AdoptSourceKonnect,
						Mode: commonv1alpha1.AdoptModeMatch,
						Konnect: &commonv1alpha1.AdoptKonnectOptions{
							ID: lo.FromPtr(certSDK.ID),
						},
					}
				},
			)
			err := controllerutil.SetOwnerReference(&ext, &dpCert, r.Scheme(), controllerutil.WithBlockOwnerDeletion(true))
			if err != nil {
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, &dpCert); err != nil {
				if errS, ok := errors.AsType[*apierrors.StatusError](err); ok {
					if errS.ErrStatus.Reason == metav1.StatusReasonAlreadyExists {
						log.Debug(logger, "DataPlane client certificate already exists", "name", dpCert.Name, "namespace", dpCert.Namespace)
					}
				} else {
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
					// In case of an error when creating the object in Konnect, we requeue "immediately"
					// so that the operation is retried without waiting for the resync period.
					return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
			}
			updated, res, err := patch.WithFinalizer(ctx, r.Client, certificateSecret, KonnectCleanupFinalizer)
			if err != nil || !res.IsZero() {
				return res, err
			}
			if updated {
				log.Info(logger, "konnect-cleanup finalizer on the referenced secret updated")
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
		}

		updated, res, err := patch.WithFinalizer(ctx, r.Client, &ext, KonnectCleanupFinalizer)
		if err != nil || !res.IsZero() {
			return res, err
		}
		if updated {
			log.Info(logger, "KonnectExtension finalizer added", "finalizer", KonnectCleanupFinalizer)
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
	case cleanup:
		// If there are no mapped IDs from KongDataPlaneClientCertificates in cluster,
		// then let's use the SDK to query Konnect directly to find any existing certificates.
		// This is just to make sure that users migrating from older versions of the operator
		// where the dataplane client certificates were not managed using
		// KongDataPlaneClientCertificate CRs don't end up creating duplicate
		// certificates in Konnect.
		// TODO: https://github.com/Kong/kong-operator/issues/2630
		// remove this block in future major release after several operator releases.
		if certSDKFound {
			if certSDK.ID == nil {
				return ctrl.Result{}, errors.New("cannot cleanup DataPlane certificate in Konnect without ID")
			}

			dpCert := konnectresource.GenerateKongDataPlaneClientCertificate(
				certificateSecret.Name,
				certificateSecret.Namespace,
				&ext.Spec.Konnect.ControlPlane.Ref,
				string(certificateSecret.Data[consts.TLSCRT]),
				&ext,
				func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) {
					dpCert.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
						// setting the controlPlane ID in the status as a workaround for the GetControlPlaneID method,
						// that expects the ControlPlaneID to be set in the status.
						ControlPlaneID: cpID,
						// setting the ID in the status as a workaround for the DeleteKongDataPlaneClientCertificate method,
						// that expects the ID to be set in the status.
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: *certSDK.ID,
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
				return ctrl.Result{RequeueAfter: r.SyncPeriod}, err
			}

		}

		if certFound {
			// This should never happen, but checking to make the dereference below bullet-proof
			if cert.GetKonnectID() == "" {
				return ctrl.Result{}, errors.New("cannot cleanup DataPlane certificate in Konnect without ID")
			}
			if err := r.Delete(ctx, &cert); err != nil {
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
				// In case of an error in the Konnect ops, we requeue so that
				// the operation is retried after the resync period.
				return ctrl.Result{RequeueAfter: r.SyncPeriod}, err
			}
			log.Debug(logger, "KongDataPlaneClientCertificate deleted", "name", cert.Name, "namespace", cert.Namespace)
			return ctrl.Result{Requeue: true}, err
		}

		// in case no IDs are mapped to the secret, we can remove the finalizer from the secret.
		if len(mappedIDs) == 0 {
			updated = controllerutil.RemoveFinalizer(certificateSecret, KonnectCleanupFinalizer)
			if updated {
				if err := r.Update(ctx, certificateSecret); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, err
				}
				log.Info(logger, "Secret finalizer removed")
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// If the KongDataPlaneClientCertificate exists in cluster, check if it's programmed in Konnect
	// and proceed only after it is programmed.
	if certFound {
		var (
			dpCert   configurationv1alpha1.KongDataPlaneClientCertificate
			dpCertNN = types.NamespacedName{
				Name:      certificateSecret.Name,
				Namespace: certificateSecret.Namespace,
			}
		)
		if err = r.Get(ctx, dpCertNN, &dpCert); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Debug(logger, "DataPlane client certificate retrieval failed in cluster",
					"namespace", dpCertNN.Namespace,
					"name", dpCertNN.Name,
				)
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		if !k8sutils.IsProgrammed(&dpCert) {
			log.Debug(logger, "DataPlane client certificate not yet programmed in Konnect",
				"namespace", dpCertNN.Namespace,
				"name", dpCertNN.Name,
			)
			// set the certificateProvisioned condition to true
			if res, updated, err := patch.StatusWithConditions(
				ctx,
				r.Client,
				&ext,
				certProvisionedCond,
			); err != nil || updated || !res.IsZero() {
				return res, err
			}
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
	}

	certProvisionedCond.Status = metav1.ConditionTrue
	certProvisionedCond.Reason = konnectv1alpha1.DataPlaneCertificateProvisionedReasonProvisioned
	certProvisionedCond.Message = "DataPlane client certificate provisioned successfully"

	// set the certificateProvisioned condition to true
	if res, updated, err := patch.StatusWithConditions(
		ctx,
		r.Client,
		&ext,
		certProvisionedCond,
	); err != nil || updated || !res.IsZero() {
		return res, err
	}

	authRef := konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name:      apiAuth.Name,
		Namespace: &apiAuth.Namespace,
	}
	if enforceKonnectExtensionStatus(cp, authRef, *certificateSecret, &ext) {
		log.Debug(logger, "updating KonnectExtension status")
		err := r.Client.Status().Update(ctx, &ext)
		if apierrors.IsConflict(err) {
			// in case the err is of type conflict, don't return it and instead trigger
			// another reconciliation.
			// This is just to prevent spamming of conflict errors.
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	readyCondition = metav1.Condition{
		Type:    konnectv1alpha2.KonnectExtensionReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha2.KonnectExtensionReadyReasonReady,
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

	if res, err := r.ensureExtendablesReferencesInStatus(ctx, &ext, dataPlaneList.Items, controlPlaneList.Items); err != nil || !res.IsZero() {
		return res, err
	}

	// NOTE: We requeue here to keep enforcing the state of the resource in Konnect.
	// Konnect does not allow subscribing to changes so we need to keep pushing the
	// desired state periodically.
	// Setting "Requeue: true" along with RequeueAfter makes the controller bulletproof, as
	// if the syncPeriod is set to zero, the controller won't requeue.
	log.Debug(logger, "reconciled")
	return ctrl.Result{
		Requeue:      true,
		RequeueAfter: r.SyncPeriod,
	}, nil
}

// SecretInUseEnforceT is an enum type for enforcing or removing the secret-in-use finalizer.
type SecretInUseEnforceT string

const (
	// SecretInUseEnforceAdd enforces the secret-in-use finalizer.
	SecretInUseEnforceAdd SecretInUseEnforceT = "add"
	// SecretInUseEnforceRemove removes the secret-in-use finalizer.
	SecretInUseEnforceRemove SecretInUseEnforceT = "remove"
)

// enforceSecretInUseFinalizer adds or removes the secret-in-use finalizer on the given secret.
// Returns true if the reconciliation should return.
func enforceSecretInUseFinalizer(
	ctx context.Context,
	cl client.Client,
	certificateSecret *corev1.Secret,
	logger logr.Logger,
	enforce SecretInUseEnforceT,
) (bool, ctrl.Result, error) {
	var updated bool
	const finalizer = consts.KonnectExtensionSecretInUseFinalizer

	switch enforce {
	case SecretInUseEnforceAdd:
		updated = controllerutil.AddFinalizer(certificateSecret, finalizer)
	case SecretInUseEnforceRemove:
		updated = controllerutil.RemoveFinalizer(certificateSecret, finalizer)
	}

	if !updated {
		return false, ctrl.Result{}, nil
	}
	if err := cl.Update(ctx, certificateSecret); err != nil {
		if apierrors.IsConflict(err) {
			return true, ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return true, ctrl.Result{}, err
	}
	log.Debug(logger,
		finalizer+" finalizer enforced on Secret",
		"secret", client.ObjectKeyFromObject(certificateSecret),
		"operation", enforce,
	)
	return true, ctrl.Result{}, nil
}
