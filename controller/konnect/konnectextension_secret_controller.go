package konnect

import (
	"context"
	"time"

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/modules/manager/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// KonnectExtensionReconciler reconciles a KonnectExtension's Secret object.
type KonnectExtensionSecretReconciler struct {
	client.Client
	LoggingMode              logging.Mode
	SdkFactory               sdkops.SDKFactory
	SyncPeriod               time.Duration
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	ClusterCAKeyConfig       secrets.KeyConfig
}

var (
	konnectDataPlaneCertificateLabelMatchExpression = metav1.LabelSelectorRequirement{
		Key:      SecretKonnectDataPlaneCertificateLabel,
		Operator: metav1.LabelSelectorOpExists,
	}
	konnectDataplaneCertificateReconcilerMatchExpression = metav1.LabelSelectorRequirement{
		Key:      SecretKonnectDataPlaneCertificateReconcilerLabel,
		Operator: metav1.LabelSelectorOpExists,
	}
)

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectExtensionSecretReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	var konnectExtensionSecretLabelSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			konnectDataPlaneCertificateLabelMatchExpression,
			konnectDataplaneCertificateReconcilerMatchExpression,
		},
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(konnectExtensionSecretLabelSelector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		// TODO: watch dataplane cert
		WithEventFilter(labelSelectorPredicate).
		Complete(r)
}

func (r *KonnectExtensionSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "dataplane", r.LoggingMode)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Trace(logger, "ensuring KongDataPlaneClientCertificate", "secret", secret.Name)
	res, dataplaneCert, err := ensureKongDataPlaneCertificate(ctx, r.Client, &secret,
		client.HasLabels{
			SecretKonnectDataPlaneCertificateLabel,
			SecretKonnectDataPlaneCertificateReconcilerLabel,
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	switch res {
	case op.Created, op.Updated:
		log.Debug(logger, "KongDataPlaneClientCertificate modified", "service", dataplaneCert.Name, "reason", res)
		return ctrl.Result{}, nil // KongDataPlaneClientCertificate creation/update will trigger reconciliation
	case op.Noop:
	case op.Deleted: // This should not happen.
	}

	return ctrl.Result{}, nil
}
