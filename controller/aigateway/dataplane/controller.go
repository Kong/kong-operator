/*
Copyright 2026 Kong, Inc.

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
	"errors"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// Reconciler reconciles an AIGatewayDataPlane object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
	CertTTL                  time.Duration

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time.  It is used for both diff-before-apply and
	// structured-merge-diff based PodTemplateSpec merging.
	TypeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on AIGatewayDataPlane objects.
	eventRecorder events.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&aigatewayv1alpha1.AIGatewayDataPlane{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&configurationv1alpha1.AIGatewayDataPlaneCertificate{}).
		Watches(
			&konnectv1alpha1.KonnectAIGateway{},
			handler.EnqueueRequestsFromMapFunc(enqueueForKonnectAIGatewayRef(mgr.GetClient())),
		).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of an AIGatewayDataPlane toward the desired state.
func (r *Reconciler) Reconcile(ctx context.Context, aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) (res ctrl.Result, err error) {
	logger := log.GetLogger(ctx, "aigw-dataplane", r.LoggingMode)

	log.Trace(logger, "reconciling AIGatewayDataPlane resource")

	defer func() { err = errors.Join(err, r.applyStatus(ctx, logger, aigwdp)) }()

	// Resolve referenced KonnectAIGateway and set resolution condition.
	aigatewaycp, err := r.resolveKonnectAIGateway(ctx, logger, aigwdp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure mTLS client certificate secret and set certificate condition.
	certResult, certSecret, err := r.ensureCertificateSecret(ctx, aigwdp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the Secret was just created/updated so the Deployment
	// picks up the correct Secret name on the next reconcile. No explicit
	// requeue is needed, the watch on the owned Secret triggers it.
	if certResult != op.Noop {
		return ctrl.Result{}, nil
	}

	// Ensure the AIGatewayDataPlaneCertificate is registered with Konnect.
	// Return early if not yet programmed; the Owns() watch retriggeres once
	// the Konnect controller flips Programmed to True.
	certProgrammed, err := r.ensureKonnectCertificate(ctx, logger, aigwdp, aigatewaycp, certSecret)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If the certificate is not yet programmed on Konnect, return early.
	// Without this, we would create a deployment that uses a cert secret not yet present in Konnect.
	if !certProgrammed {
		return ctrl.Result{}, nil
	}

	// Reconcile the full AI Gateway Deployment spec.
	if err := r.ensureDeployment(ctx, logger, aigwdp, aigatewaycp, certSecret.Name); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure the Ingress Service.
	if err := r.ensureIngressService(ctx, logger, aigwdp); err != nil {
		return ctrl.Result{}, err
	}

	// Compute Ready condition; deferred applyStatus flushes status.
	if err := ensureReadyStatus(ctx, r.Client, aigwdp); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for AIGatewayDataPlane resource")
	return ctrl.Result{}, nil
}
