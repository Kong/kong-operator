package konnect

import (
	"context"
	"fmt"
	"time"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// APIAuthInUseFinalizer is a finalizer added to KonnectAPIAuthConfiguration resources
// that are currently in use by other Konnect resources. This finalizer prevents the
// deletion of the authentication configuration until all dependent resources have
// been properly cleaned up or updated to use a different authentication method.
const APIAuthInUseFinalizer = "konnect.konghq.com/konnectapiauth-in-use"

// KonnectAPIAuthConfigurationReconciler reconciles a KonnectAPIAuthConfiguration object.
type KonnectAPIAuthConfigurationReconciler struct {
	controllerOptions controller.Options
	sdkFactory        sdkops.SDKFactory
	client            client.Client
	loggingMode       logging.Mode
}

const (
	// SecretTokenKey is the key used to store the token in the Secret.
	SecretTokenKey = "token"
	// SecretCredentialLabel is the label used to identify Secrets holding
	// KonnectAPIAuthConfiguration tokens.
	SecretCredentialLabel = "konghq.com/credential" //nolint:gosec
	// SecretCredentialLabelValueKonnect is the value of the label used to
	// identify Secrets holding KonnectAPIAuthConfiguration tokens.
	SecretCredentialLabelValueKonnect = "konnect"
)

// NewKonnectAPIAuthConfigurationReconciler creates a new KonnectAPIAuthConfigurationReconciler.
func NewKonnectAPIAuthConfigurationReconciler(
	controllerOptions controller.Options,
	sdkFactory sdkops.SDKFactory,
	loggingMode logging.Mode,
	client client.Client,
) *KonnectAPIAuthConfigurationReconciler {
	return &KonnectAPIAuthConfigurationReconciler{
		controllerOptions: controllerOptions,
		sdkFactory:        sdkFactory,
		loggingMode:       loggingMode,
		client:            client,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectAPIAuthConfigurationReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	secretLabelPredicate, err := predicate.LabelSelectorPredicate(
		metav1.LabelSelector{
			MatchLabels: map[string]string{
				SecretCredentialLabel: SecretCredentialLabelValueKonnect,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create Secret label selector predicate: %w", err)
	}

	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.controllerOptions).
		For(&konnectv1alpha1.KonnectAPIAuthConfiguration{}).
		Named("KonnectAPIAuthConfiguration").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(
				listKonnectAPIAuthConfigurationsReferencingSecret(mgr.GetClient()),
			),
			builder.WithPredicates(secretLabelPredicate),
		).
		Watches(
			&configurationv1alpha1.KongReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueObjectsForKongReferenceGrant[konnectv1alpha1.KonnectAPIAuthConfigurationList](mgr.GetClient()),
			),
		)

	setKonnectAPIAuthConfigurationRefWatches(b)

	return b.Complete(r)
}

// Reconcile reconciles a KonnectAPIAuthConfiguration object.
func (r *KonnectAPIAuthConfigurationReconciler) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.client.Get(ctx, req.NamespacedName, &apiAuth); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var (
		entityTypeName = "KonnectAPIAuthConfiguration"
		logger         = log.GetLogger(ctx, entityTypeName, r.loggingMode)
	)

	updated, err := EnsureFinalizerOnKonnectAPIAuthConfiguration(ctx, r.client, &apiAuth)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure finalizer on KonnectAPIAuthConfiguration %s: %w", req.String(), err)
	}
	if updated {
		// update will requeue
		return ctrl.Result{}, nil
	}

	log.Debug(logger, "reconciling")
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

	token, err := getTokenFromKonnectAPIAuthConfiguration(ctx, r.client, &apiAuth)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, r.client, &apiAuth,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	server, err := server.NewServer[konnectv1alpha1.KonnectAPIAuthConfiguration](apiAuth.Spec.ServerURL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse server URL: %w", err)
	}
	sdk := r.sdkFactory.NewKonnectSDK(server, sdkops.SDKToken(token))

	// TODO(pmalek): check if api auth config has a valid status condition
	// If not then return an error.

	// NOTE: /organizations/me is not public in OpenAPI spec so we can use it
	// but not using the SDK
	// https://kongstrong.slack.com/archives/C04RXLGNB6K/p1719830395775599?thread_ts=1719406468.883729&cid=C04RXLGNB6K

	// NOTE: This is needed because currently the SDK only lists the prod global API as supported:
	// https://github.com/Kong/sdk-konnect-go/blob/999d9a987e1aa7d2e09ac11b1450f4563adf21ea/models/operations/getorganizationsme.go#L10-L12
	respOrg, err := sdk.GetMeSDK().GetOrganizationsMe(ctx, sdkkonnectops.WithServerURL(server.URL()))
	if err != nil ||
		respOrg == nil ||
		respOrg.MeOrganization == nil ||
		respOrg.MeOrganization.ID == nil {

		var errMsg string
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = "response from Konnect is nil"
		}

		logger.Error(err, "failed to get organization info from Konnect")
		if cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !ok ||
			cond.Status != metav1.ConditionFalse ||
			cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid ||
			cond.ObservedGeneration != apiAuth.GetGeneration() ||
			apiAuth.Status.OrganizationID != "" ||
			apiAuth.Status.ServerURL != server.URL() {

			old := apiAuth.DeepCopy()
			apiAuth.Status.OrganizationID = ""
			apiAuth.Status.ServerURL = server.URL()

			_ = patch.SetStatusWithConditionIfDifferent(&apiAuth,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
				errMsg,
			)

			_, errUpdate := patch.ApplyStatusPatchIfNotEmpty(ctx, r.client, ctrllog.FromContext(ctx), &apiAuth, old)
			if errUpdate != nil {
				if apierrors.IsConflict(errUpdate) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, errUpdate
			}

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	// Update the status only if it would change to prevent unnecessary updates.
	condMessage := "Token is valid"
	if apiAuth.Spec.Type == konnectv1alpha1.KonnectAPIAuthTypeSecretRef {
		nn := types.NamespacedName{
			Namespace: apiAuth.Spec.SecretRef.Namespace,
			Name:      apiAuth.Spec.SecretRef.Name,
		}
		if nn.Namespace == "" {
			nn.Namespace = apiAuth.Namespace
		}
		condMessage = fmt.Sprintf("Token from Secret %s is valid", nn)
	}
	if cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !ok ||
		cond.Status != metav1.ConditionTrue ||
		cond.Message != condMessage ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid ||
		cond.ObservedGeneration != apiAuth.GetGeneration() ||
		apiAuth.Status.OrganizationID != *respOrg.MeOrganization.ID ||
		apiAuth.Status.ServerURL != server.URL() {

		old := apiAuth.DeepCopy()

		apiAuth.Status.OrganizationID = *respOrg.MeOrganization.ID
		apiAuth.Status.ServerURL = server.URL()

		_ = patch.SetStatusWithConditionIfDifferent(&apiAuth,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
			condMessage,
		)

		_, errUpdate := patch.ApplyStatusPatchIfNotEmpty(ctx, r.client, ctrllog.FromContext(ctx), &apiAuth, old)
		if errUpdate != nil {
			if apierrors.IsConflict(errUpdate) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, errUpdate
		}

		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// getTokenFromKonnectAPIAuthConfiguration returns the token from the secret reference or the token field.
func getTokenFromKonnectAPIAuthConfiguration(
	ctx context.Context, cl client.Client, apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (string, error) {
	switch apiAuth.Spec.Type {
	case konnectv1alpha1.KonnectAPIAuthTypeToken:
		return apiAuth.Spec.Token, nil
	case konnectv1alpha1.KonnectAPIAuthTypeSecretRef:
		nn := types.NamespacedName{
			Namespace: apiAuth.Spec.SecretRef.Namespace,
			Name:      apiAuth.Spec.SecretRef.Name,
		}
		if nn.Namespace == "" {
			nn.Namespace = apiAuth.Namespace
		}

		if nn.Namespace != apiAuth.Namespace {
			gvkAPIAuth := metav1.GroupVersionKind(apiAuth.GetObjectKind().GroupVersionKind())
			gvkSecret := metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
			err := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, apiAuth.Namespace, nn.Namespace, nn.Name, gvkAPIAuth, gvkSecret)
			if err != nil {
				ctrllog.FromContext(ctx).
					Error(
						fmt.Errorf("missing KongReferenceGrant from KonnectAPIAuthConfiguration %s to Secret %s",
							client.ObjectKeyFromObject(apiAuth), nn,
						),
						"WARNING: referencing Secret in a different namespace. "+
							"This will require a KongReferenceGrant in Secret's namespace in future versions.",
					)
			}
		}

		var secret corev1.Secret
		if err := cl.Get(ctx, nn, &secret); err != nil {
			return "", fmt.Errorf("failed to get Secret %s: %w", nn, err)
		}
		if secret.Labels == nil || secret.Labels[SecretCredentialLabel] != SecretCredentialLabelValueKonnect {
			return "", fmt.Errorf("secret %s does not have label %s: %s", nn, SecretCredentialLabel, SecretCredentialLabelValueKonnect)
		}
		if secret.Data == nil {
			return "", fmt.Errorf("secret %s has no data", nn)
		}
		if _, ok := secret.Data[SecretTokenKey]; !ok {
			return "", fmt.Errorf("secret %s does not have key %s", nn, SecretTokenKey)
		}
		return string(secret.Data[SecretTokenKey]), nil
	}

	return "", fmt.Errorf("unknown KonnectAPIAuthType: %s", apiAuth.Spec.Type)
}

// EnsureFinalizerOnKonnectAPIAuthConfiguration ensures that the KonnectAPIAuthConfiguration
// has a finalizer if there are any resources referencing it, or removes the finalizer if there
// are no referencing resources.
//
// It iterates through all types in KonnectAPIAuthReferencingTypeListsWithIndexes and checks if
// any resources of those types reference the given KonnectAPIAuthConfiguration. If at least one
// referencing resource is found, it adds the APIAuthInUseFinalizer to the KonnectAPIAuthConfiguration.
// If no referencing resources are found, it removes the finalizer.
//
// Parameters:
//   - ctx: The context for the operation
//   - cl: The Kubernetes client used to list resources and patch the KonnectAPIAuthConfiguration
//   - apiAuth: The KonnectAPIAuthConfiguration to ensure the finalizer on
//
// Returns:
//   - patched: true if the KonnectAPIAuthConfiguration was modified (finalizer added or removed), false otherwise
//   - err: An error if the operation failed, nil otherwise
func EnsureFinalizerOnKonnectAPIAuthConfiguration(
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (patched bool, err error) {
	var needsFinalizer bool
	for t, i := range konnectAPIAuthReferencingTypeListsWithIndexes {
		list := t.DeepCopyObject().(client.ObjectList)
		err := cl.List(ctx,
			list,
			client.MatchingFields{
				i: apiAuth.Namespace + "/" + apiAuth.Name,
			},
		)
		if err != nil {
			return false, fmt.Errorf("failed to list objects of type %T referencing KonnectAPIAuthConfiguration %s: %w", t, apiAuth.Name, err)
		}

		items, err := meta.ExtractList(list)
		if err != nil {
			return false, fmt.Errorf("failed to extract items from list: %w", err)
		}
		if len(items) > 0 {
			needsFinalizer = true
			break
		}
	}

	var updated bool
	// Add or remove finalizer based on whether there are referencing resources
	if needsFinalizer {
		updated, _, err = patch.WithFinalizer(ctx, cl, client.Object(apiAuth), APIAuthInUseFinalizer)
		if err != nil {
			return false, err
		}
	} else {
		updated, _, err = patch.WithoutFinalizer(ctx, cl, client.Object(apiAuth), APIAuthInUseFinalizer)
		if err != nil {
			return false, err
		}
	}

	return updated, nil
}
