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

// Package dataplane contains the DataPlane reconciliation logic shared by the
// specialized DataPlane controllers (AIGatewayDataPlane, KegDataPlane).
// The generic Reconciler is parameterized over the DataPlane type, the Konnect
// control plane type it references and the Konnect certificate type it
// provisions; type specific behavior is injected through Config.
package dataplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/address"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// Object is the constraint for the reconciled DataPlane types
// (e.g. AIGatewayDataPlane, KegDataPlane).
type Object interface {
	client.Object
	k8sutils.ConditionsAware
}

// ControlPlaneObject is the constraint for the Konnect control plane types
// referenced by the DataPlane (e.g. KonnectAIGateway, KonnectEventGateway).
type ControlPlaneObject interface {
	client.Object
	GetConditions() []metav1.Condition
}

// CertificateObject is the constraint for the Konnect certificate types
// provisioned for the DataPlane (e.g. AIGatewayDataPlaneCertificate,
// EventGatewayDataPlaneCertificate).
type CertificateObject interface {
	client.Object
	GetConditions() []metav1.Condition
}

// EnsureCertificateFunc provisions (or finds) the mTLS client certificate
// Secret for a DataPlane, signed by the cluster CA. It mirrors
// secrets.EnsureCertificate, which cannot be called from generic code because
// of its union type constraint; pass an explicitly instantiated
// secrets.EnsureCertificate[*YourDataPlane] here.
type EnsureCertificateFunc[T Object] func(
	ctx context.Context,
	owner T,
	subject string,
	mtlsCASecretNN types.NamespacedName,
	usages []certificatesv1.KeyUsage,
	cl client.Client,
	additionalMatchingLabels client.MatchingLabels,
	certTTL time.Duration,
) (op.Result, *corev1.Secret, error)

// Conditions carries the condition types, reasons and messages used by the
// reconciler. Values are supplied by each specialized controller from its API
// package constants so the shared logic stays bound to the API definitions.
type Conditions struct {
	// ReadyType is the type of the top-level Ready condition.
	ReadyType string
	// ResourceReadyReason is the reason used when the resource is ready.
	ResourceReadyReason string
	// DependenciesNotReadyReason is the reason used when another condition is not true.
	DependenciesNotReadyReason string
	// DependenciesNotReadyMessage is the message used when the Deployment does not exist yet.
	DependenciesNotReadyMessage string
	// WaitingToBecomeReadyReason is the reason used while the Deployment rollout is in progress.
	WaitingToBecomeReadyReason string
	// WaitingToBecomeReadyMessage is the message used while the Deployment rollout is in progress.
	WaitingToBecomeReadyMessage string
	// UnableToProvisionReason is the reason used when provisioning a resource fails.
	UnableToProvisionReason string

	// CertificateProvisionedType is the type of the mTLS certificate Secret condition.
	CertificateProvisionedType string
	// CertificateProvisionedReason is the reason used when the certificate Secret has been provisioned.
	CertificateProvisionedReason string

	// ControlPlaneResolvedType is the type of the control plane resolution condition.
	ControlPlaneResolvedType string
	// ControlPlaneResolvedReason is the reason used when the control plane has been resolved.
	ControlPlaneResolvedReason string
	// ControlPlaneResolvedMessage is the message used when the control plane has been resolved.
	ControlPlaneResolvedMessage string
	// ControlPlaneNotFoundReason is the reason used when the control plane was not found.
	ControlPlaneNotFoundReason string
	// ControlPlaneNotFoundMessage is the message used when the control plane was not found.
	ControlPlaneNotFoundMessage string
	// ControlPlaneNotProgrammedReason is the reason used when the control plane is not yet Programmed.
	ControlPlaneNotProgrammedReason string
	// ControlPlaneNotProgrammedMessage is the message used when the control plane is not yet Programmed.
	ControlPlaneNotProgrammedMessage string

	// KonnectCertificateRegisteredType is the type of the Konnect certificate registration condition.
	KonnectCertificateRegisteredType string
	// KonnectCertificateRegisteredReason is the reason used when the certificate is ensured and programmed.
	KonnectCertificateRegisteredReason string
	// KonnectCertificateRegistrationFailedReason is the reason used when the certificate could not be ensured.
	KonnectCertificateRegistrationFailedReason string
	// KonnectCertificateNotProgrammedReason is the reason used when the certificate is not yet programmed on Konnect.
	KonnectCertificateNotProgrammedReason string

	// ServiceReadyType is the type of the Service readiness condition.
	ServiceReadyType string
	// ServiceReadyReason is the reason used when the Service is ready.
	ServiceReadyReason string
	// ServiceReadyMessage is the message used when the Service is ready.
	ServiceReadyMessage string
	// WaitingForAddressReason is the reason used while the Service waits for an external address.
	WaitingForAddressReason string
	// WaitingForAddressMessage is the message used while the Service waits for an external address.
	WaitingForAddressMessage string
}

// DeploymentConfig carries the type specific bits of the owned Deployment.
type DeploymentConfig[T Object, CP ControlPlaneObject] struct {
	// ContainerName is the name of the DataPlane container (e.g. "aigw", "keg").
	ContainerName string
	// RelatedImageEnvVar is the environment variable used to override the container image.
	RelatedImageEnvVar string
	// DefaultImage is the fallback container image.
	DefaultImage string
	// ManagedByLabelValue is the value of the managed-by label (e.g. "aigateway-dataplane", "dataplane").
	ManagedByLabelValue string

	// PodTemplateSpec returns the user-provided pod template overlay, or nil.
	PodTemplateSpec func(T) *corev1.PodTemplateSpec
	// DeploymentLabels returns the user-provided Deployment labels, or nil.
	DeploymentLabels func(T) map[string]string
	// DeploymentAnnotations returns the user-provided Deployment annotations, or nil.
	DeploymentAnnotations func(T) map[string]string
	// Replicas returns the replica count to seed on the Deployment: the static
	// replica count, or the HPA minReplicas when horizontal scaling is configured.
	Replicas func(T) *int32

	// BuildContainer builds the DataPlane container and the additional volumes
	// it requires. The Konnect certificate volume is appended by the reconciler.
	// cp is nil when the DataPlane has no control plane reference configured.
	BuildContainer func(dp T, cp CP, image string) (corev1.Container, []corev1.Volume, error)
	// LabelManaged, when non-nil, marks the Deployment and its pod template as
	// managed (e.g. k8sresources.LabelObjectAsAIGatewayDataPlaneManaged).
	LabelManaged func(metav1.Object)
}

// ServiceConfig carries the type specific bits of the owned Service.
type ServiceConfig[T Object] struct {
	// Description is the human-readable Service description used in logs,
	// errors and events (e.g. "Ingress", "Kafka").
	Description string
	// NameSuffix is appended to the DataPlane name to form the Service name (e.g. "-ingress").
	NameSuffix string
	// DefaultPortName is the name of the default port (e.g. "ingress").
	DefaultPortName string
	// DefaultPort is the port exposed by default.
	DefaultPort int32
	// ManagedByLabelValue is the value of the managed-by label used in the
	// Service selector (e.g. "aigateway-dataplane", "dataplane").
	ManagedByLabelValue string
	// Options returns the user-provided Service options, or nil.
	Options func(T) *ServiceOptions
}

// Config wires the type specific behavior of a specialized DataPlane
// reconciler into the shared generic Reconciler.
type Config[T Object, CP ControlPlaneObject, Cert CertificateObject] struct {
	// ControllerName is the name used for logging and event recording.
	ControllerName string
	// Kind is the human-readable DataPlane kind used in logs, errors and
	// events (e.g. "AIGatewayDataPlane", "KegDataPlane").
	Kind string

	// NewObject returns a new empty DataPlane object.
	NewObject func() T
	// NewControlPlaneObject returns a new empty control plane object.
	NewControlPlaneObject func() CP
	// NewCertificateObject returns a new empty certificate object.
	NewCertificateObject func() Cert
	// NewObjectList returns a new empty DataPlane list object, used by the
	// control plane watch to list the DataPlanes referencing it.
	NewObjectList func() client.ObjectList

	// ControlPlaneRefName returns the name of the referenced control plane,
	// or "" when the DataPlane has no control plane reference configured.
	ControlPlaneRefName func(T) string
	// ControlPlaneKind is the human-readable control plane kind used in logs
	// and errors (e.g. "KonnectAIGateway", "KonnectEventGateway").
	ControlPlaneKind string
	// ControlPlaneRefIndexField is the field index used to list DataPlanes by
	// their control plane reference.
	ControlPlaneRefIndexField string

	// Conditions carries the condition types, reasons and messages.
	Conditions Conditions

	// CertificateLabelKey marks the provisioned mTLS certificate Secret.
	CertificateLabelKey string
	// CertificateKind is the certificate resource kind used in logs, errors
	// and events (e.g. "AIGatewayDataPlaneCertificate").
	CertificateKind string
	// BuildCertificate builds the desired certificate object for the DataPlane.
	BuildCertificate func(dp T, cp CP, certSecretName string) Cert
	// EnsureCertificate provisions the mTLS client certificate Secret.
	EnsureCertificate EnsureCertificateFunc[T]

	// Deployment configures the owned Deployment.
	Deployment DeploymentConfig[T, CP]
	// Service configures the owned Service.
	Service ServiceConfig[T]

	// HPAScalingSpec returns the HPA scaling spec, or nil when horizontal
	// scaling is not configured.
	HPAScalingSpec func(T) *k8sresources.HPAScalingSpec
	// SetStatusReplicas copies the Deployment replica counts into the DataPlane status.
	SetStatusReplicas func(dp T, replicas, readyReplicas int32)
	// SetStatusAddresses converts the Service addresses into the DataPlane status.
	SetStatusAddresses func(dp T, addrs []operatorv1beta1.Address)
}

// Reconciler reconciles a specialized DataPlane object (e.g.
// AIGatewayDataPlane, KegDataPlane) using the behavior provided by Config.
type Reconciler[T Object, CP ControlPlaneObject, Cert CertificateObject] struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
	CertTTL                  time.Duration

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time. It is used for both diff-before-apply and
	// structured-merge-diff based PodTemplateSpec merging.
	TypeConverter managedfields.TypeConverter

	// EventRecorder records Kubernetes events on the DataPlane objects.
	EventRecorder events.EventRecorder

	// Config wires the type specific behavior.
	Config Config[T, CP, Cert]
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler[T, CP, Cert]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(r.Config.NewObject()).
		Owns(&appsv1.Deployment{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(r.Config.NewCertificateObject()).
		Watches(
			r.Config.NewControlPlaneObject(),
			handler.EnqueueRequestsFromMapFunc(EnqueueDataPlanesForControlPlane(
				mgr.GetClient(),
				r.Config.NewObjectList,
				r.Config.ControlPlaneRefIndexField,
				r.Config.Kind,
				r.Config.ControlPlaneKind,
			)),
		).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of a DataPlane toward the desired state.
func (r *Reconciler[T, CP, Cert]) Reconcile(ctx context.Context, dp T) (res ctrl.Result, err error) {
	logger := log.GetLogger(ctx, r.Config.ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling "+r.Config.Kind+" resource")

	defer func() {
		err = errors.Join(err, r.ensureReadyStatus(ctx, dp))
		err = errors.Join(err, r.applyStatus(ctx, logger, dp))
	}()

	// Resolve the referenced control plane and set the resolution condition.
	// cpName is empty when the DataPlane has no control plane reference
	// configured; in that case resolution and Konnect certificate automation
	// are skipped.
	var cp CP
	cpName := r.Config.ControlPlaneRefName(dp)
	if cpName != "" {
		cp, err = r.resolveControlPlane(ctx, logger, dp, cpName)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Ensure mTLS client certificate secret and set certificate condition.
	certResult, certSecret, err := r.ensureCertificateSecret(ctx, dp)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the Secret was just created/updated so the Deployment
	// picks up the correct Secret name on the next reconcile. No explicit
	// requeue is needed, the watch on the owned Secret triggers it.
	if certResult != op.Noop {
		return ctrl.Result{}, nil
	}

	// Ensure the certificate is registered with Konnect.
	// Return early if not yet programmed; the Owns() watch retriggeres once
	// the Konnect controller flips Programmed to True.
	certProgrammed := true
	if cpName != "" {
		certProgrammed, err = r.ensureKonnectCertificate(ctx, logger, dp, cp, certSecret)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	// If the certificate is not yet programmed on Konnect, return early.
	// Without this, we would create a deployment that uses a cert secret not yet present in Konnect.
	if !certProgrammed {
		return ctrl.Result{}, nil
	}

	// Reconcile the full Deployment spec.
	if err := r.ensureDeployment(ctx, logger, dp, cp, certSecret.Name); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile the HPA if horizontal scaling is configured.
	if err := r.ensureHPA(ctx, logger, dp, dp.GetName()); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure the Service and set its readiness condition.
	// nil svc means the cache hasn't caught up yet; the Owns() watch will
	// trigger another reconcile once the Service appears.
	svc, err := r.ensureService(ctx, logger, dp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if svc != nil {
		if err := r.ensureServiceReadyCondition(dp, svc); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Debug(logger, "reconciliation complete for "+r.Config.Kind+" resource")
	return ctrl.Result{}, nil
}

// ensureServiceReadyCondition sets the ServiceReady condition and populates
// the status addresses based on the live Service.
func (r *Reconciler[T, CP, Cert]) ensureServiceReadyCondition(
	dp T,
	svc *corev1.Service,
) error {
	svcAddrs, err := address.AddressesFromService(svc)
	if err != nil {
		return fmt.Errorf("failed to get addresses from %s Service for %s %s/%s: %w",
			r.Config.Service.Description, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	r.Config.SetStatusAddresses(dp, svcAddrs)

	if serviceIsReady(svc) {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgconsts.ConditionType(r.Config.Conditions.ServiceReadyType),
				metav1.ConditionTrue,
				kcfgconsts.ConditionReason(r.Config.Conditions.ServiceReadyReason),
				r.Config.Conditions.ServiceReadyMessage,
				dp.GetGeneration(),
			),
			dp,
		)
	} else {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgconsts.ConditionType(r.Config.Conditions.ServiceReadyType),
				metav1.ConditionFalse,
				kcfgconsts.ConditionReason(r.Config.Conditions.WaitingForAddressReason),
				r.Config.Conditions.WaitingForAddressMessage,
				dp.GetGeneration(),
			),
			dp,
		)
	}
	return nil
}

// serviceIsReady reports whether the Service has an external address.
// Non-LoadBalancer Services are always considered ready.
func serviceIsReady(svc *corev1.Service) bool {
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
