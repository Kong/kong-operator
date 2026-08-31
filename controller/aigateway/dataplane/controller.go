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
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/address"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
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
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}).
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

	defer func() {
		err = errors.Join(err, ensureReadyStatus(ctx, r.Client, aigwdp))
		err = errors.Join(err, r.applyStatus(ctx, logger, aigwdp))
	}()

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
	// When no ControlPlaneRef is configured, there's no KonnectAIGateway to
	// register the certificate against, so Konnect cert automation is skipped.
	certProgrammed := true
	if aigatewaycp != nil {
		certProgrammed, err = r.ensureKonnectCertificate(ctx, logger, aigwdp, aigatewaycp, certSecret)
		if err != nil {
			return ctrl.Result{}, err
		}
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

	// Reconcile the HPA if horizontal scaling is configured.
	if err := r.ensureHPA(ctx, logger, aigwdp, aigwdp.Name); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure the Ingress Service and set its readiness condition.
	// nil svc means the cache hasn't caught up yet; the Owns() watch will
	// trigger another reconcile once the Service appears.
	svc, err := r.ensureIngressService(ctx, logger, aigwdp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if svc != nil {
		if err := r.ensureServiceReadyCondition(aigwdp, svc); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Debug(logger, "reconciliation complete for AIGatewayDataPlane resource")
	return ctrl.Result{}, nil
}

// ensureServiceReadyCondition sets the ServiceReady condition and populates
// Status.Addresses based on the live ingress Service.
func (r *Reconciler) ensureServiceReadyCondition(
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	svc *corev1.Service,
) error {
	svcAddrs, err := address.AddressesFromService(svc)
	if err != nil {
		return fmt.Errorf("failed to get addresses from Ingress Service for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}
	addrs := make([]aigatewayv1alpha1.Address, len(svcAddrs))
	for i, a := range svcAddrs {
		addrs[i] = aigatewayv1alpha1.Address{
			Value:      a.Value,
			SourceType: aigatewayv1alpha1.AddressSourceType(a.SourceType),
		}
		if a.Type != nil {
			t := aigatewayv1alpha1.AddressType(*a.Type)
			addrs[i].Type = &t
		}
	}
	aigwdp.Status.Addresses = addrs

	if ingressServiceIsReady(svc) {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				aigatewayv1alpha1.ServiceReadyType,
				metav1.ConditionTrue,
				aigatewayv1alpha1.ServiceReadyReason,
				aigatewayv1alpha1.ServiceReadyMessage,
				aigwdp.Generation,
			),
			aigwdp,
		)
	} else {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				aigatewayv1alpha1.ServiceReadyType,
				metav1.ConditionFalse,
				aigatewayv1alpha1.WaitingForAddressReason,
				aigatewayv1alpha1.WaitingForAddressMessage,
				aigwdp.Generation,
			),
			aigwdp,
		)
	}
	return nil
}

// ingressServiceIsReady reports whether the ingress Service has an external
// address. Non-LoadBalancer Services are always considered ready.
func ingressServiceIsReady(svc *corev1.Service) bool {
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return true
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.Hostname != "" || ing.IP != "" {
			return true
		}
	}
	return false
}
