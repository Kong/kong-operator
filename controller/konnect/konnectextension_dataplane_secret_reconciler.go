package konnect

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// konnectExtensionSecretPurposeLabel is the label value used to identify secrets that are used by KonnectExtensions.
	konnectExtensionSecretPurposeLabel = "konnectExtension"
)

// KonnectExtensionReconciler reconciles a KonnectExtension object.
type KonnectExtensionDataplaneSecretReconciler struct {
	client.Client
	developmentMode bool
	sdkFactory      sdkops.SDKFactory
}

func NewKonnectExtensionDataplaneSecretReconciler(client client.Client, developmentMode bool, sdkFactory sdkops.SDKFactory) *KonnectExtensionDataplaneSecretReconciler {
	return &KonnectExtensionDataplaneSecretReconciler{
		Client:          client,
		developmentMode: developmentMode,
		sdkFactory:      sdkFactory,
	}
}

func (r *KonnectExtensionDataplaneSecretReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	pred, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{
			consts.SecretPurposeLabel: konnectExtensionSecretPurposeLabel,
		},
	})
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(pred)).
		Owns(&configurationv1alpha1.KongDataPlaneClientCertificate{}).
		Complete(r)
}

func (r *KonnectExtensionDataplaneSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger := log.GetLogger(ctx, "Secret", r.developmentMode)
	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling")

	var extensionList konnectv1alpha1.KonnectExtensionList

	r.List(ctx, &extensionList, client.MatchingFields{index.KonnectExtensionsSecretIndex: secret.Name})
	if len(extensionList.Items) == 0 {
		return ctrl.Result{}, nil
	}

	for _, ext := range extensionList.Items {
		secretData, ok := secret.Data[consts.TLSCRT]
		if !ok {
			log.Error(logger, fmt.Errorf("secret %s/%s malformed", secret.Namespace, secret.Name), "tls.crt is missing")
		}
		res, _, err := ensureKongDataPlaneClientCertificateForSecret(ctx, r.Client, logger, &secret, string(secretData), ext.Spec.KonnectControlPlane.ControlPlaneRef)
		if err != nil {
			return ctrl.Result{}, err
		}
		if res != op.Noop {
			return ctrl.Result{}, nil
		}
	}

	log.Debug(logger, "reconciled")
	return ctrl.Result{}, nil
}

func ensureKongDataPlaneClientCertificateForSecret(
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	secret *corev1.Secret,
	certData string,
	controlPlaneRef commonv1alpha1.ControlPlaneRef,
) (res op.Result, dpClientCert *configurationv1alpha1.KongDataPlaneClientCertificate, err error) {
	matchingLabels := k8sresources.GetManagedLabelForOwner(secret)
	certs, err := k8sutils.ListKongDataPlaneClientCertificateForOwner(
		ctx,
		cl,
		secret.Namespace,
		secret.UID,
		client.MatchingLabels(matchingLabels),
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing KongDataPlaneClientCertificates for Secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	if len(certs) > 0 {
		if err := k8sreduce.ReduceKongDataPlaneClientCertificates(ctx, cl, certs, k8sreduce.FilterKongDataPlaneClientCertificates); err != nil {
			return op.Noop, nil, fmt.Errorf("failed reducing KongDataPlaneClientCertificates for Secret %s/%s: %w", secret.Namespace, secret.Name, err)
		}
		return op.Noop, nil, nil
	}

	generatedCert, err := k8sresources.GenerateKongDataPlaneClientCertificateForSecret(secret, certData, controlPlaneRef)
	if err != nil {
		return op.Noop, nil, err
	}

	if len(certs) == 1 {
		var updated bool
		existingCert := certs[0]
		oldExistingCert := existingCert.DeepCopy()

		// ensure that object metadata is up to date
		updated, existingCert.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingCert.ObjectMeta, generatedCert.ObjectMeta)
		// ensure that certificate is up to date
		if !cmp.Equal(existingCert.Spec, generatedCert.Spec) {
			existingCert.Spec = generatedCert.Spec
			updated = true
		}

		return patch.ApplyPatchIfNotEmpty(ctx, cl, log, &existingCert, oldExistingCert, updated)
	}

	if err = cl.Create(ctx, generatedCert); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating KongDataPlaneClientCertificate for DataPlane %s: %w", secret.Name, err)
	}

	return op.Created, nil, nil

}
