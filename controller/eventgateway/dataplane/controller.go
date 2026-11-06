/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// Reconciler reconciles a KegDataPlane object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
	CertTTL                  time.Duration

	// OperatorNamespace is the namespace the GWAdmin controller publishes the
	// cp client CA of a GWAdmin in, which the client certificates of the data
	// planes of an OnPremEventGateway are issued from.
	OperatorNamespace string
	// GWAdminCertTTL is the validity of the certificates the GWAdmin controller
	// issues, which caps the one of those client certificates.
	GWAdminCertTTL time.Duration

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time.  It is used for both diff-before-apply and
	// structured-merge-diff based PodTemplateSpec merging.
	TypeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on KegDataPlane objects.
	eventRecorder events.EventRecorder

	// warnedExpiring holds, by KegDataPlane UID, the warnings a Manual client
	// certificate about to expire was last reported with (see
	// warnManualCertificateExpiring).
	warnedExpiring sync.Map

	// OnPremUnavailable disables the OnPremEventGateway control plane kind:
	// the manager sets it when the cluster doesn't serve every resource of
	// OnPremGroupVersionResources at startup, and registers the indexes of
	// the kind otherwise, so the watches and the indexes always agree.
	OnPremUnavailable bool
}

type sharedReconciler = shareddataplane.Reconciler[
	*eventgatewayv1alpha1.KegDataPlane,
	*configurationv1alpha1.EventGatewayDataPlaneCertificate,
]

// config returns the KegDataPlane configuration of the shared reconciler,
// with the parts that depend on the reconciler: the OnPremEventGateway
// control plane kind, whose dependencies are read from the operator
// namespace, and the certificate resolution, renewal and cleanup, which
// issue the certificates of its data planes from there.
func (r *Reconciler) config() shareddataplane.Config[
	*eventgatewayv1alpha1.KegDataPlane,
	*configurationv1alpha1.EventGatewayDataPlaneCertificate,
] {
	cfg := config
	onPrem := onPremEventGatewayKind
	onPrem.Dependencies = onPremDependenciesFunc(r.OperatorNamespace, r.SecretLabelSelector)
	if r.OnPremUnavailable {
		onPrem.DisabledMessage = onPremDisabledMessage
	}
	cfg.ControlPlanes = append(slices.Clone(config.ControlPlanes), onPrem)
	cfg.ExtraWatches = func(blder *builder.Builder, mgr ctrl.Manager) *builder.Builder {
		return extraWatches(blder, mgr, !r.OnPremUnavailable)
	}
	cfg.Certificate.Resolve = r.resolveCertificateSecret
	cfg.Certificate.CleanupStale = r.cleanupStaleCertificates
	cfg.Certificate.RequeueAfter = r.onPremCertificateRequeue
	return cfg
}

// base returns the shared generic reconciler wired with the KegDataPlane
// configuration.
func (r *Reconciler) base() *sharedReconciler {
	return &sharedReconciler{
		Client:                   r.Client,
		LoggingMode:              r.LoggingMode,
		ClusterCASecretName:      r.ClusterCASecretName,
		ClusterCASecretNamespace: r.ClusterCASecretNamespace,
		SecretLabelSelector:      r.SecretLabelSelector,
		CertTTL:                  r.CertTTL,
		TypeConverter:            r.TypeConverter,
		EventRecorder:            r.eventRecorder,
		Config:                   r.config(),
	}
}

// SetupWithManager sets up the controller with the Manager. The
// OnPremEventGateway control plane kind is only enabled when the cluster
// serves its resources: an operator managing Konnect data planes only doesn't
// need them.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	if r.OnPremUnavailable {
		ctrl.LoggerFrom(ctx).Info("OnPremEventGateway control plane kind disabled: its CRDs are not installed",
			"controller", ControllerName, "resources", OnPremGroupVersionResources())
	}
	return r.base().SetupWithManager(ctx, mgr)
}
