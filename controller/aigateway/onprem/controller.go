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

package onprem

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/go-logr/logr"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	dataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/finalizer"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ControllerName is the name used for logging and event recording.
const ControllerName = "onprem-aigateway"

// Reconciler reconciles an OnPremAIGateway object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time. It is used for diff-before-apply status patches.
	TypeConverter managedfields.TypeConverter

	// InstancesManager runs the in-process on-prem AI Gateway control plane instances, one per
	// OnPremAIGateway resource.
	InstancesManager *multiinstanceai.Manager

	// RestConfig, Scheme and CacheSyncTimeout are passed down to each instance so it can build
	// and run its own controller-runtime manager hosting the configuration-entity controllers.
	RestConfig       *rest.Config
	Scheme           *runtime.Scheme
	CacheSyncTimeout time.Duration

	// ClusterCASecretName and ClusterCASecretNamespace point to the Secret holding
	// the cluster CA used to sign the mTLS client certificate the instances use to
	// push configuration to their data planes' Admin API.
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string

	// WatchNamespaces is the operator's own watch scope (--watch-namespaces). An
	// empty list means the operator watches every namespace. A gateway's
	// spec.watchNamespaces cannot exceed it.
	WatchNamespaces []string

	// CertTTL is the TTL of the certificates provisioned by this controller.
	CertTTL time.Duration

	// LicenseGetter, when non-nil, provides the effective Kong license
	// (from the KongLicense resource): its availability is reported in the
	// LicenseValid status condition. The license itself is propagated to the
	// gateway pods by the AIGatewayDataPlane controller (KONG_LICENSE_DATA
	// env var).
	LicenseGetter dataplane.LicenseGetter
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aigatewayv1alpha1.OnPremAIGateway{}).
		// Watching AIGatewayDataPlane resources to requeue the OnPremAIGateway when relevant changes occur.
		Watches(
			&aigatewayv1alpha1.AIGatewayDataPlane{},
			handler.EnqueueRequestsFromMapFunc(mapAIGatewayDataPlaneToOnPremAIGateway),
		).
		// KongLicense is cluster-scoped: a license added, changed or disabled
		// affects every OnPremAIGateway, so fan out to all of them. Without
		// this watch the LicenseValid condition would stay stale until the
		// periodic resync when no AIGatewayDataPlane references the gateway.
		Watches(
			&configurationv1alpha1.KongLicense{},
			handler.EnqueueRequestsFromMapFunc(enqueueAllOnPremAIGateways(mgr.GetClient())),
		).
		// Watch the mTLS client certificate Secret: the secretcert controller renews
		// expiring certificates by deleting the Secret, and EnsureCertificate recreates
		// it under a new GenerateName. Without this watch the reconciler would never
		// learn about the new Secret and the running instance would keep pushing with
		// the stale reference.
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestForOwner(
				mgr.GetScheme(), mgr.GetRESTMapper(),
				&aigatewayv1alpha1.OnPremAIGateway{},
				handler.OnlyControllerOwner(),
			),
			builder.WithPredicates(clientCertSecretPredicate()),
		).
		// Watch WatchNamespaceGrants so that a grant created, edited or deleted
		// after the gateway re-triggers the reconcile that requested it: without
		// this watch the WatchNamespaceGrantValid condition would stay stale until
		// an unrelated event arrives.
		Watches(
			&operatorv1alpha1.WatchNamespaceGrant{},
			watchNamespaceGrantHandler(mgr.GetClient(), mgr.GetLogger()),
		).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// clientCertSecretPredicate filters Secret events to only those carrying the
// OnPremAIGateway Admin API client certificate label. Only this controller
// provisions Secrets with that label, so no further filtering is needed.
func clientCertSecretPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return false
		}
		return secret.Labels[consts.SecretOnPremAIGatewayAdminClientCertificateLabel] == "true"
	})
}

// Reconcile moves the current state of an OnPremAIGateway toward the desired state.
func (r *Reconciler) Reconcile(ctx context.Context, onprem *aigatewayv1alpha1.OnPremAIGateway) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)

	log.Trace(logger, "reconciling OnPremAIGateway resource")

	// The mgrID is used to identify the control plane instance in the multi-instance manager.
	mgrID, err := manager.NewID(string(onprem.GetUID()))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create manager ID: %w", err)
	}

	// The resource is being deleted: tear down the instance it was running.
	if !onprem.DeletionTimestamp.IsZero() {
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if _, ok := errors.AsType[instances.InstanceNotFoundError](err); ok {
				log.Debug(logger, "control plane instance not found, skipping cleanup")
			} else {
				return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
			}
		}

		if controllerutil.RemoveFinalizer(onprem, string(OnPremAIGatewayFinalizerInstanceTeardown)) {
			if err := r.Update(ctx, onprem); err != nil {
				return finalizer.HandlePatchOrUpdateError(err, logger)
			}
		}

		log.Debug(logger, "resource cleanup completed, OnPremAIGateway deleted")
		return ctrl.Result{}, nil
	}

	// Ensure the resource has a finalizer so that the instance gets torn down on delete. Without it the
	// typed reconciler never sees the deletion and the instance would keep running.
	if controllerutil.AddFinalizer(onprem, string(OnPremAIGatewayFinalizerInstanceTeardown)) {
		log.Trace(logger, "setting finalizers")
		if err := r.Update(ctx, onprem); err != nil {
			return finalizer.HandlePatchOrUpdateError(err, logger)
		}
		// Requeue to ensure that we do not miss next reconciliation request in case
		// AddFinalizer calls returned true but the update resulted in a noop.
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	// Report license availability on the gateway. The condition never gates
	// Ready (see setReadySkippingLicenseCondition); the license itself is
	// propagated to the gateway pods by the AIGatewayDataPlane controller
	// (KONG_LICENSE_DATA env var). The KongLicense watch registered in
	// SetupWithManager re-triggers this reconcile when a license is added,
	// changed or disabled.
	dataplane.SetLicenseStatusCondition(
		onprem, r.LicenseGetter,
		string(aigatewayv1alpha1.LicenseValidType),
		string(aigatewayv1alpha1.LicenseValidReason),
		string(aigatewayv1alpha1.LicenseMissingReason),
	)

	// Validate the gateway's options against the operator's configuration. If they
	// are not valid (e.g. the secret label selectors of spec.objectFilters conflict
	// with the operator's secret label selector), the OnPremAIGateway is marked
	// invalid and the reconciliation stops.
	log.Trace(logger, "validating OnPremAIGateway's options with the reconciler's configuration")
	msg, ok := r.validateOnPremAIGatewayOptions(onprem)
	if !ok {
		k8sutils.SetCondition(
			k8sutils.NewCondition(
				aigatewayv1alpha1.OnPremAIGatewayOptionsValidType,
				metav1.ConditionFalse,
				aigatewayv1alpha1.OnPremAIGatewayOptionsInvalidReason,
				msg,
			),
			onprem,
		)
		setReadySkippingLicenseCondition(onprem)
		// A gateway that narrows the operator's options (e.g. spec.watchNamespaces wider
		// than --watch-namespaces) must not keep serving the configuration it built from
		// them, and data planes gate on Ready.
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if _, ok := errors.AsType[instances.InstanceNotFoundError](err); !ok {
				return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
			}
		}
		return ctrl.Result{}, r.applyStatus(ctx, logger, onprem)
	}
	k8sutils.SetCondition(
		k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayOptionsValidType,
			metav1.ConditionTrue,
			aigatewayv1alpha1.OnPremAIGatewayOptionsValidReason,
			"Controller options are valid",
		),
		onprem,
	)

	// Namespaces the instance watches for configuration entities and Secrets.
	// Already validated above, so the error cannot trigger here.
	watchNamespaces, err := resolveWatchNamespaces(onprem)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolving watch namespaces: %w", err)
	}

	// Watching namespaces outside the gateway's own requires permission from the
	// namespace owners. The instance must not run its caches for them before the
	// grants exist, so a running instance is stopped and the gateway is not ready
	// until the grants are back.
	if err := r.validateWatchNamespaceGrants(ctx, onprem, watchNamespaces); err != nil {
		// A list failure says nothing about the grants: requeue and keep the
		// running instance, which a missing grant must stop.
		if _, ok := errors.AsType[watchNamespaceGrantListError](err); ok {
			return ctrl.Result{}, err
		}
		k8sutils.SetCondition(
			k8sutils.NewCondition(
				aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidType,
				metav1.ConditionFalse,
				aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantInvalidReason,
				fmt.Sprintf("WatchNamespaceGrants are missing or invalid for the OnPremAIGateway: %v", err),
			),
			onprem,
		)
		setReadySkippingLicenseCondition(onprem)
		// A revoked grant must stop the instance: it still caches the entities and
		// Secrets of the namespace no longer granted. The spec did not change, so
		// the config hash does not drift and would never restart it.
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if _, ok := errors.AsType[instances.InstanceNotFoundError](err); !ok {
				return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
			}
		}
		if err := r.applyStatus(ctx, logger, onprem); err != nil {
			return ctrl.Result{}, err
		}
		// The grant watch re-triggers this reconcile; the requeue is the backstop.
		return ctrl.Result{RequeueAfter: requeueAfterMissingGrant}, nil
	}
	grantMessage := "WatchNamespaceGrants are not required"
	if requiresWatchNamespaceGrants(watchNamespaces, onprem.Namespace) {
		grantMessage = "WatchNamespaceGrants are present and valid"
	}
	k8sutils.SetCondition(
		k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidType,
			metav1.ConditionTrue,
			aigatewayv1alpha1.OnPremAIGatewayWatchNamespaceGrantValidReason,
			grantMessage,
		),
		onprem,
	)

	// The mTLS client certificate is what the instance presents to the data planes'
	// Admin API when pushing configuration. Provision it before scheduling the
	// instance: an instance without it cannot push, and the push loop would only
	// accumulate retries until the Secret shows up.
	adminClientCertSecret, err := r.ensureAdminClientCertificateSecret(ctx, onprem)
	if err != nil {
		// Certificate provisioning failures are transient (CA availability, API server
		// errors): requeue with backoff.
		return ctrl.Result{}, fmt.Errorf("failed to ensure the Admin API client certificate Secret: %w", err)
	}

	cfg := r.configFromSpec(onprem, watchNamespaces)
	// Part of the hashed instance config: when the Secret is renewed under a new
	// name, the hash drifts and the instance restarts with the new reference.
	cfg.AdminClientCertSecretNN = client.ObjectKeyFromObject(adminClientCertSecret)

	log.Trace(logger, "checking readiness of the AI Gateway control plane instance")
	if err := r.InstancesManager.IsInstanceReady(mgrID); err != nil {
		log.Trace(logger, "control plane instance not ready yet", "error", err)

		if _, ok := errors.AsType[instances.InstanceNotFoundError](err); ok {
			log.Debug(logger, "control plane instance not found, creating new instance")
			if err := r.scheduleInstance(logger, mgrID, cfg, client.ObjectKeyFromObject(onprem), client.ObjectKeyFromObject(adminClientCertSecret)); err != nil {
				// The stopped instance is removed from the manager asynchronously, so it can still be
				// registered here. Requeue and reschedule once it's gone.
				if _, ok := errors.AsType[instances.InstanceWithIDAlreadyScheduledError](err); !ok {
					return ctrl.Result{}, err
				}
				log.Debug(logger, "stopped instance not reaped yet, retrying")
			}
		}
		return r.initStatusToWaitingToBecomeReady(ctx, logger, onprem)
	}

	log.Trace(logger, "checking if the AI Gateway control plane instance config matches the spec")
	hashRunning, err := r.InstancesManager.GetInstanceConfigHash(mgrID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get instance config hash: %w", err)
	}
	hashFromSpec, err := multiinstanceai.Hash(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to hash OnPremAIGateway config: %w", err)
	}

	// The running instance's config drifted from the spec: restart it with the new config.
	if hashRunning != hashFromSpec {
		log.Debug(logger, "control plane instance config does not match the spec, restarting instance")
		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
		}
		if err := r.scheduleInstance(logger, mgrID, cfg, client.ObjectKeyFromObject(onprem), client.ObjectKeyFromObject(adminClientCertSecret)); err != nil {
			// The stopped instance is removed from the manager asynchronously, so it can still be
			// registered here. Requeue and reschedule once it's gone.
			if _, ok := errors.AsType[instances.InstanceWithIDAlreadyScheduledError](err); !ok {
				return ctrl.Result{}, err
			}
			log.Debug(logger, "stopped instance not reaped yet, retrying")
		}
		return r.initStatusToWaitingToBecomeReady(ctx, logger, onprem)
	}

	onprem.Status.ConfigHash = hashRunning
	setReadySkippingLicenseCondition(onprem)

	if err := r.applyStatus(ctx, logger, onprem); err != nil {
		return ctrl.Result{}, err
	}

	log.Debug(logger, "reconciliation complete for OnPremAIGateway resource")
	return ctrl.Result{}, nil
}

// setReadySkippingLicenseCondition sets the Ready condition without letting
// the informational LicenseValid condition gate it: SetReadyWithGeneration
// re-checks every condition via AreAllConditionsHaveTrueStatus and has no way
// to skip LicenseValid, so a missing license (the default state) would flip
// Ready to False and block every referencing AIGatewayDataPlane. Any other
// non-True condition still blocks readiness, as with SetReadyWithGeneration.
func setReadySkippingLicenseCondition(onprem *aigatewayv1alpha1.OnPremAIGateway) {
	ready := true
	blockedMessage := ""
	for _, c := range onprem.GetConditions() {
		if c.Type == string(aigatewayv1alpha1.ReadyType) || c.Type == string(aigatewayv1alpha1.LicenseValidType) {
			continue
		}
		if c.Status != metav1.ConditionTrue {
			ready = false
			blockedMessage = c.Message
		}
	}
	status := metav1.ConditionTrue
	reason := aigatewayv1alpha1.ResourceReadyReason
	if !ready {
		status = metav1.ConditionFalse
		reason = aigatewayv1alpha1.DependenciesNotReadyReason
	}
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			aigatewayv1alpha1.ReadyType, status, reason, blockedMessage, onprem.GetGeneration(),
		),
		onprem,
	)
}

// configFromSpec builds the control plane instance configuration from the OnPremAIGateway spec.
// AIGatewayModel assembly and dbless rendering live in Instance.sendConfig, which converts
// non-strict until spec.conversion options drive convert.Options: dangling references are
// warnings, not fatal errors.
// watchNamespaces is the resolved spec.watchNamespaces (nil means every namespace).
// TODO: https://github.com/Kong/kong-operator/issues/5569 - fill in the remaining config
// from the spec; restore the (context, logger, error) signature and the caller's
// error-handling branch when config building can fail.
func (r *Reconciler) configFromSpec(
	onprem *aigatewayv1alpha1.OnPremAIGateway,
	watchNamespaces []string,
) multiinstanceai.Config {
	cfg := multiinstanceai.Config{
		WatchNamespaces: watchNamespaces,
	}
	if dps := onprem.Spec.DataPlaneSync; dps != nil {
		if dps.IntervalSeconds != nil {
			cfg.SyncInterval = time.Duration(*dps.IntervalSeconds) * time.Second
		}
		if dps.TimeoutSeconds != nil {
			cfg.SyncTimeout = time.Duration(*dps.TimeoutSeconds) * time.Second
		}
	}
	if of := onprem.Spec.ObjectFilters; of != nil && of.Secrets != nil {
		cfg.SecretLabelFilters = of.Secrets.MatchLabels
	}
	return cfg
}

// validateOnPremAIGatewayOptions checks if the OnPremAIGateway's options are valid with
// the operator specific configuration. It returns a boolean as the validation result and
// a string as the error message when the validation fails. For example, it fails when a
// matchLabels key of spec.objectFilters.secrets conflicts with the operator's secret
// label selector or with the operator-managed Admin API client certificate label, or when
// spec.watchNamespaces exceeds the operator's own watch scope.
func (r *Reconciler) validateOnPremAIGatewayOptions(onprem *aigatewayv1alpha1.OnPremAIGateway) (string, bool) {
	if _, err := resolveWatchNamespaces(onprem); err != nil {
		return err.Error(), false
	}
	if err := validateWatchNamespacesScope(onprem, r.WatchNamespaces); err != nil {
		return err.Error(), false
	}

	if onprem.Spec.ObjectFilters == nil || onprem.Spec.ObjectFilters.Secrets == nil {
		return "", true
	}
	for key := range onprem.Spec.ObjectFilters.Secrets.MatchLabels {
		if r.SecretLabelSelector != "" && key == r.SecretLabelSelector {
			return fmt.Sprintf("secret label selector in spec.objectFilters conflicts with operator's secret label filter '%s'", r.SecretLabelSelector), false
		}
		if key == consts.SecretOnPremAIGatewayAdminClientCertificateLabel {
			return fmt.Sprintf("secret label selector in spec.objectFilters conflicts with the operator-managed label '%s'", key), false
		}
	}
	return "", true
}

// scheduleInstance creates a new control plane instance and schedules it in the multi-instance manager.
//
// The instance runs its own controller-runtime manager hosting the configuration-entity
// controllers and the SecretReconciler Secret watcher (controller/aigateway/onpremconfig),
// with a Secret cache scoped to the gateway's watch namespaces (cluster-wide for type: all)
// and mirroring the operator's --secret-label-selector, so only Secrets carrying that
// selector label are read there
// (user Secrets must carry it to be ingestible; the Admin API client certificate Secret
// gets it from ensureAdminClientCertificateSecret).
func (r *Reconciler) scheduleInstance(
	logger logr.Logger,
	mgrID manager.ID,
	cfg multiinstanceai.Config,
	gatewayNN k8stypes.NamespacedName,
	adminClientCertSecretNN k8stypes.NamespacedName,
) error {
	log.Debug(logger, "creating new instance", "manager_id", mgrID, "manager_config", cfg)
	if err := r.InstancesManager.ScheduleInstance(multiinstanceai.NewInstance(
		mgrID, logger, cfg,
		multiinstanceai.Env{
			RestConfig:       r.RestConfig,
			Scheme:           r.Scheme,
			CacheSyncTimeout: r.CacheSyncTimeout,
			// Used by the instance to discover the AIGatewayDataPlanes referencing
			// this gateway and their Admin API endpoints.
			GatewayNN: gatewayNN,
			// Used by the instance to load the mTLS client certificate it presents
			// to the data planes' Admin API when pushing configuration.
			AdminClientCertSecretNN: adminClientCertSecretNN,
			TypeConverter:           r.TypeConverter,
			SecretLabelSelector:     r.SecretLabelSelector,
		},
	)); err != nil {
		return fmt.Errorf("failed to schedule instance: %w", err)
	}
	return nil
}

// ensureAdminClientCertificateSecret provisions (or finds) the mTLS client certificate
// Secret the control plane instance uses to authenticate against the Admin API of the
// AIGatewayDataPlanes referencing the gateway. The data planes' Admin API listeners
// verify client certificates against the cluster CA, so the certificate is signed by
// the cluster CA; its subject is arbitrary.
func (r *Reconciler) ensureAdminClientCertificateSecret(
	ctx context.Context,
	onprem *aigatewayv1alpha1.OnPremAIGateway,
) (*corev1.Secret, error) {
	matchingLabels := client.MatchingLabels{
		consts.SecretOnPremAIGatewayAdminClientCertificateLabel: "true",
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	// Stamp the user's spec.objectFilters.secrets matchLabels onto the cert Secret so
	// it always matches the instance's filtered Secret cache (validated in
	// validateOnPremAIGatewayOptions, so it cannot collide with the operator-managed
	// labels above).
	if filters := onprem.Spec.ObjectFilters; filters != nil && filters.Secrets != nil {
		maps.Copy(matchingLabels, filters.Secrets.MatchLabels)
	}
	_, secret, err := secrets.EnsureCertificate(
		ctx,
		onprem,
		fmt.Sprintf("%s.%s", onprem.GetName(), onprem.GetNamespace()),
		k8stypes.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		[]certificatesv1.KeyUsage{
			certificatesv1.UsageKeyEncipherment,
			certificatesv1.UsageDigitalSignature,
			certificatesv1.UsageClientAuth,
		},
		r.Client,
		matchingLabels,
		r.CertTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("ensuring the Admin API client certificate Secret for %s: %w", client.ObjectKeyFromObject(onprem), err)
	}
	return secret, nil
}

// initStatusToWaitingToBecomeReady marks the resource as not ready yet and requeues it so that the
// instance's readiness is re-checked once it had a chance to boot.
func (r *Reconciler) initStatusToWaitingToBecomeReady(
	ctx context.Context,
	logger logr.Logger,
	onprem *aigatewayv1alpha1.OnPremAIGateway,
) (ctrl.Result, error) {
	k8sutils.SetCondition(
		k8sutils.NewCondition(
			aigatewayv1alpha1.ReadyType,
			metav1.ConditionFalse,
			aigatewayv1alpha1.WaitingToBecomeReadyReason,
			aigatewayv1alpha1.WaitingToBecomeReadyMessage,
		),
		onprem,
	)
	if err := r.applyStatus(ctx, logger, onprem); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfterBoot}, nil
}

// applyStatus patches the OnPremAIGateway status subresource via SSA.
//
// The instance (pkg/multiinstance/aigateway) owns the DataPlanesConfigured and
// ConfigurationTranslated conditions, each under its own per-condition-type field
// manager (see instanceFieldManagerFor), so they are excluded from the controller's
// apply payload: applying the full cached status under ForceOwnership would steal
// those conditions' ownership on every reconcile with a stale cache, and the
// instance would steal it back (flapping ownership, duplicate Warning events).
func (r *Reconciler) applyStatus(ctx context.Context, logger logr.Logger, onprem *aigatewayv1alpha1.OnPremAIGateway) error {
	k8sutils.RemoveCondition(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType, onprem)
	k8sutils.RemoveCondition(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedType, onprem)
	result, err := controllerpkgssa.ApplyStatusIfChanged(ctx, logger, r.Client, r.TypeConverter, onprem, controllerpkgssa.FieldManager)
	if err != nil {
		log.Error(logger, err, "failed to patch OnPremAIGateway status")
		return err
	}
	if result == op.Updated {
		log.Debug(logger, "OnPremAIGateway status updated")
	}
	return nil
}
