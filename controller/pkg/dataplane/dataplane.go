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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
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
type DeploymentConfig[T Object] struct {
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
	// it requires. The Konnect certificate volume itself is managed by the
	// shared machinery and appended separately when certSecretName is not empty.
	// cp.Object is nil when the DataPlane has no control plane reference
	// configured.
	BuildContainer func(dp T, cp ResolvedControlPlane, image, certSecretName string) (corev1.Container, []corev1.Volume, error)
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
type Config[T Object, Cert CertificateObject] struct {
	// ControllerName is the name used for logging and event recording.
	ControllerName string
	// Kind is the human-readable DataPlane kind used in logs, errors and
	// events (e.g. "AIGatewayDataPlane", "KegDataPlane").
	Kind string

	// NewObject returns a new empty DataPlane object.
	NewObject func() T
	// NewCertificateObject returns a new empty certificate object.
	NewCertificateObject func() Cert
	// NewObjectList returns a new empty DataPlane list object, used by the
	// control plane watches to list the DataPlanes referencing them.
	NewObjectList func() client.ObjectList

	// ControlPlaneRef returns the kind and name of the control plane referenced
	// by the DataPlane. Name is empty when the DataPlane has no control plane
	// reference configured; in that case control plane resolution and Konnect
	// certificate automation are skipped.
	// The returned Kind must match the Kind of one of the ControlPlanes entries.
	ControlPlaneRef func(T) ControlPlaneRef
	// ControlPlanes lists the supported control plane kinds for this DataPlane
	// kind (e.g. KonnectAIGateway and OnPremAIGateway for AIGatewayDataPlane).
	// One watch and one index field are registered per entry.
	ControlPlanes []ControlPlaneKindConfig

	// Conditions carries the condition types, reasons and messages.
	Conditions Conditions

	// CertificateLabelKey marks the provisioned mTLS certificate Secret.
	CertificateLabelKey string
	// CertificateKind is the certificate resource kind used in logs, errors
	// and events (e.g. "AIGatewayDataPlaneCertificate").
	CertificateKind string
	// BuildCertificate builds the desired certificate object for the
	// DataPlane. certChecksum identifies the certificate Secret's content
	// ("" when checksum tracking is not configured): controllers that name
	// the certificate entity after the content should derive both the object
	// name and the Konnect title from it so a rotation registers a new entity
	// instead of mutating the previous one in place.
	// Only called for Konnect-backed control planes (cp.IsKonnect).
	BuildCertificate func(dp T, cp ResolvedControlPlane, certSecretName, certChecksum string) Cert
	// EnsureCertificate provisions the mTLS client certificate Secret.
	EnsureCertificate EnsureCertificateFunc[T]
	// ResolveCertificateSecret, when non-nil, resolves the certificate Secret
	// before the default EnsureCertificate-based provisioning runs. It may:
	//   - return a non-nil Secret to use it as-is (e.g. a manually
	//     provisioned, user-referenced Secret);
	//   - return (op.Result, nil, nil) to wire no certificate at all (a
	//     condition explaining why is expected to be set on dp);
	//   - call resolveAutomatic to fall back to the default operator-managed
	//     provisioning.
	ResolveCertificateSecret func(
		ctx context.Context,
		cl client.Client,
		dp T,
		cp ResolvedControlPlane,
		resolveAutomatic func(ctx context.Context, dp T) (op.Result, *corev1.Secret, error),
	) (op.Result, *corev1.Secret, error)
	// CertificateRequested, when non-nil, reports whether the DataPlane spec
	// asks for a certificate at all. When resolution produced no Secret, the
	// reconcile only stops early if a certificate was requested but could not
	// be resolved (a condition explaining why is expected to be set on dp);
	// a DataPlane that requests no certificate proceeds to reconcile its
	// Deployment without any certificate wiring. When nil, a nil resolved
	// Secret always stops the reconcile early.
	CertificateRequested func(dp T) bool
	// CertificateChecksum, when non-nil, returns a stable checksum of the
	// certificate Secret content, recorded as a Pod-template annotation so
	// that an in-place Secret edit rolls the Deployment.
	CertificateChecksum func(secret *corev1.Secret) string
	// CertificateChecksumAnnotation is the Pod-template annotation key used
	// with CertificateChecksum. Required when CertificateChecksum is set.
	CertificateChecksumAnnotation string
	// CleanupStaleCertificates, when non-nil, removes stale Konnect
	// certificate entities and operator-provisioned Secrets once the rollout
	// onto the current certificate completed.
	// Unlike Konnect certificate registration, this hook is intentionally not
	// gated on cp.IsKonnect: stale Konnect certificate entities still need
	// cleanup after a DataPlane switches to a non-Konnect control plane.
	// Implementations must tolerate a resolved control plane of any configured
	// kind (or an unconfigured one).
	CleanupStaleCertificates func(ctx context.Context, cl client.Client, logger logr.Logger, dp T, cp ResolvedControlPlane, certChecksum string) error
	// ExtraWatches, when non-nil, registers additional watches on the
	// controller builder (e.g. a watch on user-referenced certificate
	// Secrets). It receives the in-progress builder and must return it.
	ExtraWatches func(blder *builder.Builder, mgr ctrl.Manager) *builder.Builder

	// Deployment configures the owned Deployment.
	Deployment DeploymentConfig[T]
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

// ControlPlaneKindConfig returns the ControlPlaneKindConfig for the given kind.
func (c Config[T, Cert]) ControlPlaneKindConfig(kind string) (ControlPlaneKindConfig, bool) {
	for _, cpKindCfg := range c.ControlPlanes {
		if cpKindCfg.Kind == kind {
			return cpKindCfg, true
		}
	}
	return ControlPlaneKindConfig{}, false
}

// Reconciler reconciles a specialized DataPlane object (e.g.
// AIGatewayDataPlane, KegDataPlane) using the behavior provided by Config.
type Reconciler[T Object, Cert CertificateObject] struct {
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
	Config Config[T, Cert]
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler[T, Cert]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	blder := ctrl.NewControllerManagedBy(mgr).
		For(r.Config.NewObject()).
		Owns(&appsv1.Deployment{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(r.Config.NewCertificateObject())
	// One watch per supported control plane kind: a change to a control plane
	// object re-triggers reconciliation of every DataPlane referencing it.
	for _, cpKind := range r.Config.ControlPlanes {
		blder = blder.Watches(
			cpKind.NewObject(),
			handler.EnqueueRequestsFromMapFunc(EnqueueDataPlanesForControlPlane(
				mgr.GetClient(),
				r.Config.NewObjectList,
				cpKind.ControlPlaneRefIndexField,
				r.Config.Kind,
				cpKind.Kind,
			)),
		)
	}
	if r.Config.ExtraWatches != nil {
		blder = r.Config.ExtraWatches(blder, mgr)
	}
	return blder.Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of a DataPlane toward the desired state.
func (r *Reconciler[T, Cert]) Reconcile(ctx context.Context, dp T) (res ctrl.Result, err error) {
	logger := log.GetLogger(ctx, r.Config.ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling "+r.Config.Kind+" resource")

	defer func() {
		err = errors.Join(err, r.ensureReadyStatus(ctx, dp))
		err = errors.Join(err, r.applyStatus(ctx, logger, dp))
	}()

	// Resolve the referenced control plane and set the resolution condition.
	// ref.Name is empty when the DataPlane has no control plane reference
	// configured; in that case resolution and Konnect certificate automation
	// are skipped.
	var cp ResolvedControlPlane
	ref := r.Config.ControlPlaneRef(dp)
	if ref.Name != "" {
		cp, err = r.resolveControlPlane(ctx, logger, dp, ref)
		if err != nil {
			// A missing, not yet Programmed, or not yet Ready control plane is
			// an expected, user-fixable state: resolveControlPlane has set the
			// resolution condition and the control plane watch re-triggers the
			// reconcile once the control plane appears or becomes healthy, so
			// there is no need to retry with error backoff.
			if apierrors.IsNotFound(err) ||
				errors.Is(err, errControlPlaneNotProgrammed) ||
				errors.Is(err, errControlPlaneNotReady) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}

	// Resolve the mTLS client certificate secret and set the certificate
	// condition.
	var certResult op.Result
	var certSecret *corev1.Secret
	if r.Config.ResolveCertificateSecret != nil {
		certResult, certSecret, err = r.Config.ResolveCertificateSecret(ctx, r.Client, dp, cp, r.ensureCertificateSecret)
	} else {
		certResult, certSecret, err = r.ensureCertificateSecret(ctx, dp)
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the Secret was just created/updated so the Deployment
	// picks up the correct Secret name on the next reconcile. No explicit
	// requeue is needed, the watch on the owned Secret triggers it.
	if certResult != op.Noop {
		return ctrl.Result{}, nil
	}

	// A nil certSecret with a custom ResolveCertificateSecret means the
	// controller decided no certificate should be wired at all. That blocks
	// the reconcile only when the DataPlane actually requested a certificate
	// (e.g. an invalid manual reference, where a condition is already set and
	// there is nothing more to do until the user fixes it); a DataPlane that
	// requests no certificate proceeds below without any cert wiring.
	if certSecret == nil && r.Config.ResolveCertificateSecret != nil &&
		(r.Config.CertificateRequested == nil || r.Config.CertificateRequested(dp)) {
		return ctrl.Result{}, nil
	}

	// certChecksum identifies the certificate's content; when configured it
	// is recorded as a Pod-template annotation so an in-place Secret edit
	// rolls the Deployment.
	var certSecretName, certChecksum string
	if certSecret != nil {
		certSecretName = certSecret.Name
		if r.Config.CertificateChecksum != nil {
			certChecksum = r.Config.CertificateChecksum(certSecret)
		}
	}

	// Ensure the certificate is registered with Konnect.
	// Return early if not yet programmed; the Owns() watch retriggers once
	// the Konnect controller flips Programmed to True.
	// Certificate automation only applies to Konnect-backed control planes.
	certProgrammed := true
	if cp.IsConfigured() && cp.IsKonnect && certSecret != nil {
		certProgrammed, err = r.ensureKonnectCertificate(ctx, logger, dp, cp, certSecret, certChecksum)
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
	if err := r.ensureDeployment(ctx, logger, dp, cp, certSecretName, certChecksum); err != nil {
		return ctrl.Result{}, err
	}

	// Once the rollout onto the current certificate is confirmed complete (no
	// replica can still be relying on a previous one), it's safe to remove
	// stale certificate resources left over from an earlier rotation or
	// provisioning-mode switch. Until then they're deliberately left in place
	// so replicas still running the old certificate keep working.
	if r.Config.CleanupStaleCertificates != nil {
		complete, err := r.rolloutOntoCertificateComplete(ctx, dp, certChecksum)
		if err != nil {
			return ctrl.Result{}, err
		}
		if complete {
			if err := r.Config.CleanupStaleCertificates(ctx, r.Client, logger, dp, cp, certChecksum); err != nil {
				return ctrl.Result{}, err
			}
		}
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
func (r *Reconciler[T, Cert]) ensureServiceReadyCondition(
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
