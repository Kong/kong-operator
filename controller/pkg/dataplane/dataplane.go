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

// EnsureCertificateFunc provisions (or finds) a TLS certificate Secret for a
// DataPlane, signed by the cluster CA: the mTLS client certificate used for
// the outbound connection to a Konnect-backed control plane, and the Admin
// API TLS server certificate when AdminAPI is configured. It mirrors
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

	// AdminCertificateProvisionedType is the type of the Admin API server
	// certificate condition. Only used when AdminAPI is configured.
	AdminCertificateProvisionedType string
	// AdminCertificateProvisionedReason is the reason used when the Admin
	// API certificate Secret has been provisioned.
	AdminCertificateProvisionedReason string
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
	// shared machinery and appended separately when certSecretName is not empty,
	// and so is the Admin API certificate volume when adminCertSecretName is
	// not empty. cp.Object is nil when the DataPlane has no control plane
	// reference configured.
	BuildContainer func(dp T, cp ResolvedControlPlane, image, certSecretName, adminCertSecretName string) (corev1.Container, []corev1.Volume, error)
	// LabelManaged, when non-nil, marks the Deployment and its pod template as
	// managed (e.g. k8sresources.LabelObjectAsAIGatewayDataPlaneManaged).
	LabelManaged func(metav1.Object)
}

// ServiceConfig carries the type specific bits of an owned Service.
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
	// Enabled, when non-nil, reports whether this Service should be reconciled
	// for the DataPlane given its resolved control plane. A disabled Service
	// is not created, and a Service left over from an earlier reconcile in
	// which the predicate was true is removed. Only used on Services whose
	// existence is conditional (e.g. the Admin API Service derived from
	// AdminAPI): the primary Service always exists, so Config.Service.Enabled
	// must be left nil.
	Enabled func(dp T, cp ResolvedControlPlane) bool
	// SetStatusAddresses copies the Service's addresses into the DataPlane
	// status through this function; the Service's readiness feeds the
	// ServiceReady condition (see Conditions). Must be set on the primary
	// Service (Config.Service), which the status depends on; Services whose
	// existence is conditional (e.g. the Admin API Service derived from
	// AdminAPI) never set it.
	SetStatusAddresses func(dp T, addrs []operatorv1beta1.Address)
}

// AdminAPIConfig carries the configuration of the DataPlane's Admin API
// exposure: a dedicated admin Service and a TLS server certificate for it,
// both gated on the same Enabled predicate. Control plane kinds that push
// configuration to the DataPlane over its Admin API (e.g. OnPremAIGateway)
// enable it. The certificate's subject is derived from the Service name: the
// in-cluster DNS name the control plane uses to reach the Admin API.
type AdminAPIConfig[T Object] struct {
	// Enabled reports whether the Admin API should be exposed for the
	// DataPlane given its resolved control plane. cp is the zero
	// ResolvedControlPlane when the DataPlane has no control plane reference
	// configured. Must not be nil.
	Enabled func(dp T, cp ResolvedControlPlane) bool
	// ServiceNameSuffix is appended to the DataPlane name to form the admin
	// Service name (e.g. "-admin"). Must not be empty.
	ServiceNameSuffix string
	// ServicePortName is the name of the admin Service port (e.g. "admin").
	ServicePortName string
	// ServicePort is the port the admin Service exposes. Must be positive.
	ServicePort int32
	// ManagedByLabelValue is the value of the managed-by label used in the
	// admin Service selector.
	ManagedByLabelValue string
	// CertificateLabelKey marks the provisioned Admin API TLS server
	// certificate Secret. It must be distinct from Certificate.LabelKey so
	// the two Secrets never collide in the owner-scoped Secret listings
	// performed during provisioning. Must not be empty.
	CertificateLabelKey string
}

// serviceConfig derives the admin Service's ServiceConfig. Enabled is shared
// with the certificate: the Service and the certificate it serves are
// provisioned and removed together.
func (a AdminAPIConfig[T]) serviceConfig() ServiceConfig[T] {
	return ServiceConfig[T]{
		Description:         "Admin",
		NameSuffix:          a.ServiceNameSuffix,
		DefaultPortName:     a.ServicePortName,
		DefaultPort:         a.ServicePort,
		ManagedByLabelValue: a.ManagedByLabelValue,
		Enabled:             a.Enabled,
	}
}

// certificateSubject returns the subject of the Admin API TLS server
// certificate: the in-cluster DNS name of the admin Service, which is also
// the name the control plane uses to reach the Admin API.
func (a AdminAPIConfig[T]) certificateSubject(dp T) string {
	return fmt.Sprintf("%s%s.%s.svc", dp.GetName(), a.ServiceNameSuffix, dp.GetNamespace())
}

// CertificateConfig groups the configuration of the DataPlane's TLS
// certificates: the mTLS client certificate used for the outbound connection
// to a Konnect-backed control plane (provisioning, optional manual resolution,
// Konnect registration and stale cleanup) and — when AdminAPI is
// configured — the Admin API TLS server certificate provisioned for on-prem
// control planes. Both Secrets are provisioned through the same Ensure hook.
type CertificateConfig[T Object, Cert CertificateObject] struct {
	// LabelKey marks the provisioned mTLS client certificate Secret. It must
	// be distinct from AdminAPI.CertificateLabelKey so the two Secrets never
	// collide in the owner-scoped Secret listings performed during
	// provisioning.
	LabelKey string
	// Kind is the certificate resource kind used in logs, errors and events
	// (e.g. "AIGatewayDataPlaneCertificate").
	Kind string
	// Build builds the desired certificate object for the DataPlane.
	// certChecksum identifies the certificate Secret's content ("" when
	// checksum tracking is not configured): controllers that name the
	// certificate entity after the content should derive both the object name
	// and the Konnect title from it so a rotation registers a new entity
	// instead of mutating the previous one in place.
	// Only called for Konnect-backed control planes (cp.IsKonnect).
	Build func(dp T, cp ResolvedControlPlane, certSecretName, certChecksum string) Cert
	// Ensure provisions (or finds) the DataPlane's TLS certificate Secrets,
	// signed by the cluster CA: the mTLS client certificate Secret, and the
	// Admin API TLS server certificate Secret when AdminAPI is
	// configured. It is invoked on both provisioning paths, so overriding it
	// affects both.
	Ensure EnsureCertificateFunc[T]
	// Resolve, when non-nil, resolves the mTLS client certificate Secret
	// before the default Ensure-based provisioning runs. It may:
	//   - return a non-nil Secret to use it as-is (e.g. a manually
	//     provisioned, user-referenced Secret);
	//   - return (op.Result, nil, nil) to wire no certificate at all (a
	//     condition explaining why is expected to be set on dp);
	//   - call resolveAutomatic to fall back to the default operator-managed
	//     provisioning.
	Resolve func(
		ctx context.Context,
		cl client.Client,
		dp T,
		cp ResolvedControlPlane,
		resolveAutomatic func(ctx context.Context, dp T) (op.Result, *corev1.Secret, error),
	) (op.Result, *corev1.Secret, error)
	// Requested, when non-nil, reports whether the DataPlane spec asks for
	// the mTLS client certificate at all. When resolution produced no
	// Secret, the reconcile only stops early if a certificate was requested
	// but could not be resolved (a condition explaining why is expected to
	// be set on dp); a DataPlane that requests no certificate proceeds to
	// reconcile its Deployment without any certificate wiring. When nil, a
	// nil resolved Secret always stops the reconcile early.
	Requested func(dp T) bool
	// Checksum, when non-nil, returns a stable checksum of the certificate
	// Secret content, recorded as a Pod-template annotation so that an
	// in-place Secret edit rolls the Deployment.
	Checksum func(secret *corev1.Secret) string
	// ChecksumAnnotation is the Pod-template annotation key used with
	// Checksum. Required when Checksum is set.
	ChecksumAnnotation string
	// CleanupStale, when non-nil, removes stale Konnect certificate entities
	// and operator-provisioned Secrets once the rollout onto the current
	// certificate completed.
	// Unlike Konnect certificate registration, this hook is intentionally
	// not gated on cp.IsKonnect: stale Konnect certificate entities still
	// need cleanup after a DataPlane switches to a non-Konnect control
	// plane. Implementations must tolerate a resolved control plane of any
	// configured kind (or an unconfigured one).
	CleanupStale func(ctx context.Context, cl client.Client, logger logr.Logger, dp T, cp ResolvedControlPlane, certChecksum string) error
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

	// Certificate configures the DataPlane's TLS certificates.
	Certificate CertificateConfig[T, Cert]
	// ExtraWatches, when non-nil, registers additional watches on the
	// controller builder (e.g. a watch on user-referenced certificate
	// Secrets). It receives the in-progress builder and must return it.
	ExtraWatches func(blder *builder.Builder, mgr ctrl.Manager) *builder.Builder

	// Deployment configures the owned Deployment.
	Deployment DeploymentConfig[T]

	// Service configures the primary Service: the one feeding the DataPlane
	// status (SetStatusAddresses) and driving the ServiceReady condition.
	// It is always reconciled and never removed, so Enabled does not apply
	// to it and must be left nil.
	Service ServiceConfig[T]

	// AdminAPI, when non-nil, enables the exposure of the DataPlane's Admin
	// API: a dedicated admin Service and a TLS server certificate for it,
	// both gated on AdminAPI.Enabled. The certificate Secret is provisioned
	// through Certificate.Ensure, which has it signed by the cluster CA, and
	// its name is passed to Deployment.BuildContainer so the container can
	// wire the admin listener to it.
	AdminAPI *AdminAPIConfig[T]

	// HPAScalingSpec returns the HPA scaling spec, or nil when horizontal
	// scaling is not configured.
	HPAScalingSpec func(T) *k8sresources.HPAScalingSpec
	// SetStatusReplicas copies the Deployment replica counts into the DataPlane status.
	SetStatusReplicas func(dp T, replicas, readyReplicas int32)
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
	if err := r.validateConfig(); err != nil {
		return fmt.Errorf("invalid %s reconciler config: %w", r.Config.Kind, err)
	}

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

// validateConfig checks the invariants of the Service and AdminAPI
// configuration: the primary Service must feed the DataPlane status
// (SetStatusAddresses, which also drives the ServiceReady condition) and
// must not be gated by Enabled (a Service the status depends on must never
// become deletable), and an AdminAPI block, when present, must be fully
// populated with a CertificateLabelKey distinct from Certificate.LabelKey
// (so the two Secrets never collide in the owner-scoped Secret listings
// performed during provisioning).
func (r *Reconciler[T, Cert]) validateConfig() error {
	if r.Config.Service.SetStatusAddresses == nil {
		return fmt.Errorf("Service: SetStatusAddresses must be set: the primary Service feeds the DataPlane status")
	}
	if r.Config.Service.Enabled != nil {
		return fmt.Errorf("Service: Enabled must be nil: the primary Service always exists and must never become deletable")
	}
	if adminAPI := r.Config.AdminAPI; adminAPI != nil {
		switch {
		case adminAPI.Enabled == nil:
			return fmt.Errorf("AdminAPI: Enabled must not be nil")
		case adminAPI.ServiceNameSuffix == "":
			return fmt.Errorf("AdminAPI: ServiceNameSuffix must not be empty")
		case adminAPI.ServiceNameSuffix == r.Config.Service.NameSuffix:
			return fmt.Errorf("AdminAPI: ServiceNameSuffix must be distinct from Service.NameSuffix")
		case adminAPI.ServicePort <= 0:
			return fmt.Errorf("AdminAPI: ServicePort must be positive")
		case adminAPI.ServicePortName == "":
			return fmt.Errorf("AdminAPI: ServicePortName must not be empty")
		case adminAPI.ManagedByLabelValue == "":
			return fmt.Errorf("AdminAPI: ManagedByLabelValue must not be empty")
		case adminAPI.CertificateLabelKey == "":
			return fmt.Errorf("AdminAPI: CertificateLabelKey must not be empty")
		case adminAPI.CertificateLabelKey == r.Config.Certificate.LabelKey:
			return fmt.Errorf("AdminAPI: CertificateLabelKey must be distinct from Certificate.LabelKey")
		case r.Config.Conditions.AdminCertificateProvisionedType == "":
			return fmt.Errorf("Conditions: AdminCertificateProvisionedType must be set when AdminAPI is configured")
		case r.Config.Conditions.AdminCertificateProvisionedReason == "":
			return fmt.Errorf("Conditions: AdminCertificateProvisionedReason must be set when AdminAPI is configured")
		}
	}
	return nil
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
	if r.Config.Certificate.Resolve != nil {
		certResult, certSecret, err = r.Config.Certificate.Resolve(ctx, r.Client, dp, cp, r.ensureCertificateSecret)
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

	// A nil certSecret with a custom Certificate.Resolve means the
	// controller decided no certificate should be wired at all. That blocks
	// the reconcile only when the DataPlane actually requested a certificate
	// (e.g. an invalid manual reference, where a condition is already set and
	// there is nothing more to do until the user fixes it); a DataPlane that
	// requests no certificate proceeds below without any cert wiring.
	if certSecret == nil && r.Config.Certificate.Resolve != nil &&
		(r.Config.Certificate.Requested == nil || r.Config.Certificate.Requested(dp)) {
		return ctrl.Result{}, nil
	}

	// certChecksum identifies the certificate's content; when configured it
	// is recorded as a Pod-template annotation so an in-place Secret edit
	// rolls the Deployment.
	var certSecretName, certChecksum string
	if certSecret != nil {
		certSecretName = certSecret.Name
		if r.Config.Certificate.Checksum != nil {
			certChecksum = r.Config.Certificate.Checksum(certSecret)
		}
	}

	// Ensure the certificate is registered with Konnect.
	// Return early if not yet programmed; the Owns() watch retriggers once
	// the Konnect controller flips Programmed to True.
	// Certificate automation only applies to Konnect-backed control planes.
	certProgrammed := true
	if cp.IsResolved() && cp.IsKonnect && certSecret != nil {
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

	// Provision the Admin API TLS server certificate Secret when the resolved
	// control plane kind requires one (control planes that push configuration
	// to the DataPlane's Admin API). When the requirement goes away (e.g. the
	// control plane reference changed to a kind that doesn't consume the Admin
	// API, or was removed), any leftover certificate Secret and the
	// corresponding condition are removed below, once the Deployment has been
	// rebuilt without the admin certificate wiring.
	var adminCertSecretName string
	var removeAdminCertificate bool
	switch {
	case r.Config.AdminAPI == nil:
	case r.Config.AdminAPI.Enabled(dp, cp):
		adminCertResult, adminCertSecret, err := r.ensureAdminCertificateSecret(ctx, dp, r.Config.AdminAPI)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Return early if the Secret was just created/updated so the
		// Deployment picks up the Secret name on the next reconcile. No
		// explicit requeue is needed, the watch on the owned Secret triggers it.
		if adminCertResult != op.Noop {
			return ctrl.Result{}, nil
		}
		if adminCertSecret != nil {
			adminCertSecretName = adminCertSecret.Name
		}
	default:
		removeAdminCertificate = true
	}

	// Reconcile the full Deployment spec.
	if err := r.ensureDeployment(ctx, logger, dp, cp, certSecretName, adminCertSecretName, certChecksum); err != nil {
		return ctrl.Result{}, err
	}

	// Only once the Deployment no longer mounts the admin certificate is it
	// safe to remove the leftover Admin API certificate Secret(s) from an
	// earlier reconcile in which AdminAPI.Enabled was true: deleting
	// before the rebuild would leave the Deployment referencing a Secret that
	// no longer exists, so new pods could not start.
	if removeAdminCertificate {
		if err := r.deleteAdminCertificateSecretsIfOwned(ctx, logger, dp, r.Config.AdminAPI); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Once the rollout onto the current certificate is confirmed complete (no
	// replica can still be relying on a previous one), it's safe to remove
	// stale certificate resources left over from an earlier rotation or
	// provisioning-mode switch. Until then they're deliberately left in place
	// so replicas still running the old certificate keep working.
	if r.Config.Certificate.CleanupStale != nil {
		complete, err := r.rolloutOntoCertificateComplete(ctx, dp, certChecksum)
		if err != nil {
			return ctrl.Result{}, err
		}
		if complete {
			if err := r.Config.Certificate.CleanupStale(ctx, r.Client, logger, dp, cp, certChecksum); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Reconcile the HPA if horizontal scaling is configured.
	if err := r.ensureHPA(ctx, logger, dp, dp.GetName()); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure the Services: the primary one plus the admin Service derived
	// from AdminAPI, when configured. The primary Service feeds the
	// DataPlane status (SetStatusAddresses) and drives the ServiceReady
	// condition; the admin Service is gated by AdminAPI.Enabled: a gated-off
	// admin Service is removed instead.
	services := make([]ServiceConfig[T], 0, 2)
	services = append(services, r.Config.Service)
	if r.Config.AdminAPI != nil {
		services = append(services, r.Config.AdminAPI.serviceConfig())
	}
	for _, svcCfg := range services {
		if svcCfg.SetStatusAddresses == nil && svcCfg.Enabled != nil && !svcCfg.Enabled(dp, cp) {
			if err := r.deleteServiceIfOwned(ctx, logger, dp, svcCfg); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		// nil svc means the cache hasn't caught up yet; the Owns() watch will
		// trigger another reconcile once the Service appears.
		svc, err := r.ensureService(ctx, logger, dp, svcCfg)
		if err != nil {
			return ctrl.Result{}, err
		}
		if svcCfg.SetStatusAddresses != nil && svc != nil {
			if err := r.ensureServiceReadyCondition(dp, svcCfg, svc); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	log.Debug(logger, "reconciliation complete for "+r.Config.Kind+" resource")
	return ctrl.Result{}, nil
}

// ensureServiceReadyCondition sets the ServiceReady condition and populates
// the status addresses based on the live status-feeding Service.
func (r *Reconciler[T, Cert]) ensureServiceReadyCondition(
	dp T,
	svcCfg ServiceConfig[T],
	svc *corev1.Service,
) error {
	svcAddrs, err := address.AddressesFromService(svc)
	if err != nil {
		return fmt.Errorf("failed to get addresses from %s Service for %s %s/%s: %w",
			svcCfg.Description, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	svcCfg.SetStatusAddresses(dp, svcAddrs)

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
