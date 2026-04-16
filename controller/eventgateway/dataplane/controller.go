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
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
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

	// typeConverter is initialised once during SetupWithManager from the API
	// server's OpenAPI v3 schemas. It supports all types (core K8s + CRDs) and
	// is used for both diff-before-apply and structured-merge-diff based
	// PodTemplateSpec merging.
	typeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on KegDataPlane objects.
	eventRecorder events.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Initialise the TypeConverter from API server OpenAPI v3 schemas.
	// This is done once at startup.
	tc, err := initTypeConverter(mgr)
	if err != nil {
		return fmt.Errorf("DataPlane controller: failed to initialize TypeConverter: %w", err)
	}
	r.typeConverter = tc
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&eventgatewayv1alpha1.KegDataPlane{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&konnectv1alpha1.KonnectEventDataPlaneCertificate{}).
		Watches(
			&konnectv1alpha1.KonnectEventControlPlane{},
			handler.EnqueueRequestsFromMapFunc(enqueueForKonnectEventGatewayRef(mgr.GetClient())),
		).
		Complete(r)
}

// Reconcile moves the current state of a KegDataPlane toward the desired state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	logger := log.GetLogger(ctx, "keg-dataplane", r.LoggingMode)

	log.Trace(logger, "reconciling KegDataPlane resource")

	egdp := new(eventgatewayv1alpha1.KegDataPlane)
	if err := r.Get(ctx, req.NamespacedName, egdp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer func() { err = errors.Join(err, r.applyStatus(ctx, logger, egdp)) }()

	// Resolve referenced KonnectEventControlPlane and set resolution condition.
	keg, err := r.resolveKonnectEventGateway(ctx, logger, egdp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Ensure mTLS client certificate secret and set certificate condition.
	certResult, certSecret, err := r.ensureCertificateSecret(ctx, egdp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the Secret was just created/updated so the Deployment
	// picks up the correct Secret name on the next reconcile. No explicit
	// requeue is needed, the watch on the owned Secret triggers it.
	if certResult != op.Noop {
		return ctrl.Result{}, nil
	}

	// Ensure the KonnectEventDataPlaneCertificate is registered with Konnect.
	// Return early if not yet programmed; the Owns() watch retriggeres once
	// the Konnect controller flips Programmed to True.
	certProgrammed, err := r.ensureKonnectCertificate(ctx, logger, egdp, keg, certSecret)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If the certificate is not yet programmed on Konnect, return early.
	// Without this, we would create a deployment that uses a cert secret not yet present in Konnect.
	if !certProgrammed {
		return ctrl.Result{}, nil
	}

	// Reconcile the full Keg Deployment spec.
	if err := r.ensureDeployment(ctx, logger, egdp, keg, certSecret.Name); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure the Kafka Service.
	if err := r.ensureKafkaService(ctx, logger, egdp); err != nil {
		return ctrl.Result{}, err
	}

	// Compute Ready condition; deferred applyStatus flushes status.
	if err := ensureReadyStatus(ctx, r.Client, egdp); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for DataPlane resource")
	return ctrl.Result{}, nil
}
