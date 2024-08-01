package konnect

import (
	"context"
	"fmt"
	"time"

	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/log"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KonnectAPIAuthConfigurationReconciler reconciles a KonnectAPIAuthConfiguration object.
type KonnectAPIAuthConfigurationReconciler struct {
	SDKFactory      SDKFactory
	DevelopmentMode bool
	Client          client.Client
}

// NewKonnectAPIAuthConfigurationReconciler creates a new KonnectAPIAuthConfigurationReconciler.
func NewKonnectAPIAuthConfigurationReconciler(
	sdkFactory SDKFactory,
	developmentMode bool,
	client client.Client,
) *KonnectAPIAuthConfigurationReconciler {
	return &KonnectAPIAuthConfigurationReconciler{
		SDKFactory:      sdkFactory,
		DevelopmentMode: developmentMode,
		Client:          client,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectAPIAuthConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		Named("KonnectAPIAuthConfiguration")

	return b.Complete(r)
}

// Reconcile reconciles a KonnectAPIAuthConfiguration object.
func (r *KonnectAPIAuthConfigurationReconciler) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Client.Get(ctx, req.NamespacedName, &apiAuth); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var (
		entityTypeName = "KonnectAPIAuthConfiguration"
		logger         = log.GetLogger(ctx, entityTypeName, r.DevelopmentMode)
	)

	log.Debug(logger, "reconciling", apiAuth)
	if !apiAuth.GetDeletionTimestamp().IsZero() {
		logger.Info("resource is being deleted")
		// wait for termination grace period before cleaning up
		if apiAuth.GetDeletionTimestamp().After(time.Now()) {
			logger.Info("resource still under grace period, requeueing")
			return ctrl.Result{
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(apiAuth.GetDeletionTimestamp().Time),
			}, nil
		}

		return ctrl.Result{}, nil
	}

	sdk := r.SDKFactory.NewKonnectSDK(
		"https://"+apiAuth.Spec.ServerURL,
		SDKToken(apiAuth.Spec.Token),
	)

	// TODO(pmalek): check if api auth config has a valid status condition
	// If not then return an error.

	// NOTE: /organizations/me is not public in OpenAPI spec so we can use it
	// but not using the SDK
	// https://kongstrong.slack.com/archives/C04RXLGNB6K/p1719830395775599?thread_ts=1719406468.883729&cid=C04RXLGNB6K

	// NOTE: This is needed because currently the SDK only lists the prod global API as supported:
	// https://github.com/Kong/sdk-konnect-go/blob/999d9a987e1aa7d2e09ac11b1450f4563adf21ea/models/operations/getorganizationsme.go#L10-L12
	respOrg, err := sdk.Me.GetOrganizationsMe(ctx, sdkkonnectgoops.WithServerURL("https://"+apiAuth.Spec.ServerURL))
	if err != nil {
		if cond, ok := k8sutils.GetCondition(KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !ok ||
			cond.Status != metav1.ConditionFalse ||
			cond.Reason != KonnectEntityAPIAuthConfigurationReasonInvalid ||
			cond.ObservedGeneration != apiAuth.GetGeneration() ||
			apiAuth.Status.OrganizationID != "" ||
			apiAuth.Status.ServerURL != apiAuth.Spec.ServerURL {

			apiAuth.Status.OrganizationID = ""
			apiAuth.Status.ServerURL = apiAuth.Spec.ServerURL

			res, err := updateStatusWithCondition(
				ctx, r.Client, &apiAuth,
				KonnectEntityAPIAuthConfigurationValidConditionType,
				metav1.ConditionFalse,
				KonnectEntityAPIAuthConfigurationReasonInvalid,
				err.Error(),
			)
			if err != nil || res.Requeue {
				return res, err
			}
			return ctrl.Result{}, fmt.Errorf("failed to get organization info from Konnect: %w", err)
		}
		return ctrl.Result{}, fmt.Errorf("failed to get organization info from Konnect: %w", err)
	}

	// Update the status only if it would change to prevent unnecessary updates.
	if cond, ok := k8sutils.GetCondition(KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !ok ||
		cond.Status != metav1.ConditionTrue ||
		cond.Message != "" ||
		cond.Reason != KonnectEntityAPIAuthConfigurationReasonValid ||
		cond.ObservedGeneration != apiAuth.GetGeneration() ||
		apiAuth.Status.OrganizationID != *respOrg.MeOrganization.ID ||
		apiAuth.Status.ServerURL != apiAuth.Spec.ServerURL {

		apiAuth.Status.OrganizationID = *respOrg.MeOrganization.ID
		apiAuth.Status.ServerURL = apiAuth.Spec.ServerURL

		res, err := updateStatusWithCondition(
			ctx, r.Client, &apiAuth,
			KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionTrue,
			KonnectEntityAPIAuthConfigurationReasonValid,
			fmt.Sprintf("Referenced KonnectAPIAuthConfiguration %s is valid", client.ObjectKeyFromObject(&apiAuth)),
		)
		if err != nil || res.Requeue {
			return res, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}
