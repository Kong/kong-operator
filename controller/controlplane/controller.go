package controlplane

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	ctrlconsts "github.com/kong/kong-operator/controller/consts"
	"github.com/kong/kong-operator/controller/pkg/extensions"
	extensionserrors "github.com/kong/kong-operator/controller/pkg/extensions/errors"
	extensionskonnect "github.com/kong/kong-operator/controller/pkg/extensions/konnect"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	ingresserrors "github.com/kong/kong-operator/ingress-controller/pkg/errors"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// requeueAfterBoot gives the instance of ControlPlane controller in goroutine some time to start up.
const requeueAfterBoot = time.Second

// Reconciler reconciles a ControlPlane object
type Reconciler struct {
	client.Client
	CacheSyncPeriod          time.Duration
	CacheSyncTimeout         time.Duration
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	ClusterCAKeyConfig       secrets.KeyConfig

	RestConfig              *rest.Config
	KubeConfigPath          string
	InstancesManager        *multiinstance.Manager
	KonnectEnabled          bool
	EnforceConfig           bool
	LoggingMode             logging.Mode
	AnonymousReportsEnabled bool
	ClusterDomain           string
	EmitKubernetesEvents    bool

	// SecretLabelSelector is the label selector configured at the operator level.
	// When not empty, it is used as the secret label selector of all ingress cotrollers' managers.
	SecretLabelSelector string
	// ConfigMapLabelSelector is the label selector configured at the oprator level.
	// When not empty, it is used as the config map label selector of all ingress cotrollers' managers.
	ConfigMapLabelSelector string

	// WatchNamespaces is a list of namespaces to watch. If empty (default), all namespaces are watched.
	WatchNamespaces []string
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			CacheSyncTimeout: r.CacheSyncTimeout,
		}).
		For(&ControlPlane{}).
		// Watch for changes in Secret objects that are owned by ControlPlane objects.
		Owns(&corev1.Secret{}).
		Watches(
			&operatorv1alpha1.WatchNamespaceGrant{},
			handler.EnqueueRequestsFromMapFunc(r.listControlPlanesForWatchNamespaceGrants)).
		Watches(
			&operatorv1beta1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlanesFromDataPlane)).
		// Watch for changes in the DataPlane deployments, as we want to be aware of all
		// the DataPlane pod changes (every time a new pod gets ready, the deployment
		// status gets updated accordingly, leading to a reconciliation loop trigger).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.getControlPlanesFromDataPlaneDeployment))

	if r.KonnectEnabled {
		// Watch for changes in KonnectExtension objects that are referenced by ControlPlane objects.
		// They may trigger reconciliation of DataPlane resources.
		builder.WatchesRawSource(
			source.Kind(
				mgr.GetCache(),
				&konnectv1alpha2.KonnectExtension{},
				handler.TypedEnqueueRequestsFromMapFunc(index.ListObjectsReferencingKonnectExtension(mgr.GetClient(), &operatorv1beta1.DataPlaneList{})),
			),
		)
	}

	return builder.Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "controlplane", r.LoggingMode)

	log.Trace(logger, "reconciling ControlPlane resource")
	cp := new(ControlPlane)
	if err := r.Get(ctx, req.NamespacedName, cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// The mgrID is used to identify the ControlPlane instance in the multi-instance manager.
	// It is also used as UUID for the ControlPlane instance in Konnect. If changing the UUID format,
	// ensure that it is compatible with the Konnect API.
	mgrID, err := manager.NewID(string(cp.GetUID()))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create manager ID: %w", err)
	}

	// controlplane is deleted, just run garbage collection for cluster wide resources.
	if !cp.DeletionTimestamp.IsZero() {
		// wait for termination grace period before cleaning up roles and bindings
		if cp.DeletionTimestamp.After(metav1.Now().Time) {
			log.Debug(logger, "control plane deletion still under grace period")
			return ctrl.Result{
				Requeue: true,
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(cp.DeletionTimestamp.Time),
			}, nil
		}

		if err := r.InstancesManager.StopInstance(mgrID); err != nil {
			if errors.As(err, &multiinstance.InstanceNotFoundError{}) {
				log.Debug(logger, "control plane instance not found, skipping cleanup")
			} else {
				return ctrl.Result{}, fmt.Errorf("failed to stop instance: %w", err)
			}
		}

		// remove finalizer
		if controllerutil.RemoveFinalizer(cp, string(ControlPlaneFinalizerCPInstanceTeardown)) {
			if err := r.Update(ctx, cp); err != nil {
				if k8serrors.IsConflict(err) {
					log.Debug(logger, "conflict found when updating ControlPlane, retrying")
					return ctrl.Result{Requeue: true, RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
			}
		}

		// cleanup completed
		log.Debug(logger, "resource cleanup completed, controlplane deleted")
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	if controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCPInstanceTeardown)) {
		log.Trace(logger, "setting finalizers")
		if err := r.Update(ctx, cp); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane finalizer, retrying",
					"finalizer", string(ControlPlaneFinalizerCPInstanceTeardown),
				)
				return ctrl.Result{Requeue: true, RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizer %s: %w",
				string(ControlPlaneFinalizerCPInstanceTeardown),
				err,
			)
		}
		// Requeue to ensure that we do not miss next reconciliation request in case
		// AddFinalizer calls returned true but the update resulted in a noop.
		return ctrl.Result{Requeue: true, RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	k8sutils.InitReady(cp)

	log.Trace(logger, "validating ControlPlane resource conditions")
	if r.ensureIsMarkedScheduled(cp) {
		res, err := r.patchStatus(ctx, logger, cp)
		if err != nil {
			log.Debug(logger, "unable to update ControlPlane resource", "error", err)
			return res, err
		}
		if !res.IsZero() {
			log.Debug(logger, "unable to update ControlPlane resource")
			return res, nil
		}

		log.Debug(logger, "ControlPlane resource now marked as scheduled")
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	// Set DataPlane in ControlPlane's status.
	dataplaneName, res, err := r.enforceDataPlaneNameInStatus(ctx, cp)
	if res != op.Noop || err != nil {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "applying extensions")
	konnectExtensionProcessor := &extensionskonnect.ControlPlaneKonnectExtensionProcessor{}
	stop, result, err := extensions.ApplyExtensions(ctx, r.Client, cp, r.KonnectEnabled, konnectExtensionProcessor)
	if err != nil {
		if extensionserrors.IsKonnectExtensionError(err) {
			log.Debug(logger, "failed to apply extensions", "err", err)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if stop || !result.IsZero() {
		return result, nil
	}

	log.Trace(logger, "retrieving connected DataPlane")
	var (
		dataplane operatorv1beta1.DataPlane
		dpNN      = types.NamespacedName{
			Name:      dataplaneName,
			Namespace: cp.Namespace,
		}
	)
	if err := r.Get(ctx, dpNN, &dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DataPlane %s for ControlPlane: %w", dpNN, err)
	}
	dataplaneIngressServiceName, err := gatewayutils.GetDataPlaneServiceName(ctx, r.Client, &dataplane, consts.DataPlaneIngressServiceLabelValue)
	if err != nil {
		log.Debug(logger, "no existing dataplane ingress service for controlplane", "error", err)
		return ctrl.Result{}, err
	}

	dataplaneAdminServiceName, err := gatewayutils.GetDataPlaneServiceName(ctx, r.Client, &dataplane, consts.DataPlaneAdminServiceLabelValue)
	if err != nil {
		log.Debug(logger, "no existing dataplane admin service for controlplane", "error", err)
		return ctrl.Result{}, err
	}

	log.Trace(logger, "configuring ControlPlane resource")

	log.Trace(logger, "validating ControlPlane's DataPlane status")
	dataplaneIsSet, err := r.ensureDataPlaneStatus(cp, &dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure DataPlane status: %w", err)
	}
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned")
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant")
	}

	log.Trace(logger, "validating watch namespaces for the ControlPlane")
	if err := validateWatchNamespaces(cp, r.WatchNamespaces); err != nil {
		// TODO: Set status condition on the ControlPlane.
		// https://github.com/Kong/kong-operator/issues/1975
		log.Debug(logger, "watch namespaces validation failed", "error", err)
		return ctrl.Result{}, err
	}

	log.Trace(logger, "validating WatchNamespaceGrants exist for the ControlPlane")
	validatedWatchNamespaces, err := r.validateWatchNamespaceGrants(ctx, cp)
	if err != nil {
		// If there was an error validating the WatchNamespaceGrants, we set the condition
		// to false indicating that the WatchNamespaceGrants are invalid or missing.

		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid,
				metav1.ConditionFalse,
				kcfgcontrolplane.ConditionReasonWatchNamespaceGrantInvalid,
				fmt.Sprintf("WatchNamespaceGrant(s) are missing or invalid for the ControlPlane: %v", err),
				cp.GetGeneration(),
			),
			cp,
		)
		// We do not return here as we want to proceed with reconciling the Deployment.
		// This will prevent users using the ControlPlane with previous
		// WatchNamespaces spec.
		// We do not patch the status here either because that's done below.
	} else {
		// If the WatchNamespaceGrants are present and valid, we set the condition to true.
		// We note that grants are not expected if the watch namespaces are not
		// specified in the ControlPlane spec.

		msg := "WatchNamespaceGrant(s) are present and valid"
		if cp.Spec.WatchNamespaces != nil {
			msg = "WatchNamespaceGrant(s) not required"
		}
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid,
				metav1.ConditionTrue,
				kcfgcontrolplane.ConditionReasonWatchNamespaceGrantValid,
				msg,
				cp.GetGeneration(),
			),
			cp,
		)
	}

	// Validate the control plane options against the configuration of the operator.
	// If the control plane options are not valid with operator's settings
	// (For example, the label selectors of secrets conflicts with operator's selector),
	// The ControlPlane is marked invalid and the reconciliation stops.
	log.Trace(logger, "validating ControlPlane's options with the reconciler's configuration")
	msg, ok := r.validateControlPlaneOptions(cp)
	if ok {
		k8sutils.SetCondition(k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeOptionsValid,
			metav1.ConditionTrue,
			kcfgcontrolplane.ConditionReasonOptionsValid,
			"Controller options are valid",
		), cp)
	} else {
		k8sutils.SetCondition(k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeOptionsValid,
			metav1.ConditionFalse,
			kcfgcontrolplane.ConditionReasonOptionsInvalid,
			msg,
		), cp)
		return r.patchStatus(ctx, logger, cp)
	}

	var caSecret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: r.ClusterCASecretNamespace,
		Name:      r.ClusterCASecretName,
	}, &caSecret); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get CA secret: %w", err)
	}

	log.Trace(logger, "ensuring mTLS certificate secret exists")
	res, mtlsSecret, err := r.ensureAdminMTLSCertificateSecret(ctx, cp)
	if err != nil || res != op.Noop {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "checking readiness of ControlPlane instance")
	if err := r.InstancesManager.IsInstanceReady(mgrID); err != nil {
		log.Trace(logger, "control plane instance not ready yet", "error", err)

		if errors.As(err, &multiinstance.InstanceNotFoundError{}) {

			log.Debug(logger, "control plane instance not found, creating new instance")
			cfgOpts, err := r.constructControlPlaneManagerConfigOptions(
				logger, cp, &caSecret, mtlsSecret, dataplaneAdminServiceName, dataplaneIngressServiceName,
				r.RestConfig.Burst, r.RestConfig.QPS, validatedWatchNamespaces, konnectExtensionProcessor.GetKonnectConfig(),
			)
			if err != nil {
				return ctrl.Result{}, err
			}

			mgrCfg, err := manager.NewConfig(cfgOpts...)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create manager config: %w", err)
			}
			if err := r.scheduleInstance(ctx, logger, mgrID, mgrCfg); err != nil {
				return r.handleScheduleInstanceOutcome(ctx, logger, cp, err)
			}

			r.ensureControlPlaneStatus(cp, mgrCfg)
		}
		return r.initStatusToWaitingToBecomeReady(ctx, logger, cp)
	}

	log.Trace(logger, "checking if ControlPlane instance config matches the spec")
	if hashRunning, err := r.InstancesManager.GetInstanceConfigHash(mgrID); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get instance config: %w", err)
	} else {
		// Calculate the hash of config from the ControlPlane spec.
		cfgOpts, err := r.constructControlPlaneManagerConfigOptions(
			logger, cp, &caSecret, mtlsSecret, dataplaneAdminServiceName, dataplaneIngressServiceName,
			r.RestConfig.Burst, r.RestConfig.QPS, validatedWatchNamespaces, konnectExtensionProcessor.GetKonnectConfig(),
		)
		if err != nil {
			return ctrl.Result{}, err
		}

		mgrCfg, err := manager.NewConfig(cfgOpts...)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create manager config: %w", err)
		}
		hashFromSpec, errSpec := managercfg.Hash(mgrCfg)
		if errSpec != nil {
			return ctrl.Result{}, fmt.Errorf("failed to hash ControlPlane config options: %w", errSpec)
		}

		// Compare the 2 hashes to determine if the running instance's config matches the spec.
		if hashRunning != hashFromSpec {
			if err := r.InstancesManager.StopInstance(mgrID); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.scheduleInstance(ctx, logger, mgrID, mgrCfg); err != nil {
				return r.handleScheduleInstanceOutcome(ctx, logger, cp, err)
			}
			r.ensureControlPlaneStatus(cp, mgrCfg)

			return r.initStatusToWaitingToBecomeReady(ctx, logger, cp)
		}
	}

	markAsProvisioned(cp)
	k8sutils.SetReady(cp)

	result, err = r.patchStatus(ctx, logger, cp)
	if err != nil {
		log.Debug(logger, "unable to patch ControlPlane status", "error", err)
		return ctrl.Result{}, err
	}
	if !result.IsZero() {
		log.Debug(logger, "unable to patch ControlPlane status")
		return result, nil
	}

	log.Debug(logger, "reconciliation complete for ControlPlane resource")
	return ctrl.Result{}, nil
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *Reconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *ControlPlane) (ctrl.Result, error) {
	current := &ControlPlane{}

	err := r.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if controlPlaneStatusEqual(current, updated) {
		return ctrl.Result{}, nil
	}

	log.Debug(logger, "patching ControlPlane status", "status", updated.Status)
	if err := r.Client.Status().Patch(ctx, updated, client.MergeFrom(current)); err != nil {
		if k8serrors.IsConflict(err) {
			log.Debug(logger, "conflict found when updating ControlPlane, retrying")
			return ctrl.Result{Requeue: true, RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's status : %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *Reconciler) constructControlPlaneManagerConfigOptions(
	logger logr.Logger,
	cp *ControlPlane,
	caSecret *corev1.Secret,
	mtlsSecret *corev1.Secret,
	dataplaneAdminServiceName string,
	dataplaneIngressServiceName string,
	apiServerBurst int,
	apiServerQPS float32,
	validatedWatchNamespaces []string,
	konnectConfig *managercfg.KonnectConfig,
) ([]managercfg.Opt, error) {
	// TODO: https://github.com/kong/kong-operator/issues/1361
	// Configure the manager with Konnect options if KonnectExtension is attached to the ControlPlane.

	clientCert, ok := mtlsSecret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("failed to get client certificate from mTLS secret %s", client.ObjectKeyFromObject(mtlsSecret))
	}
	clientKey, ok := mtlsSecret.Data["tls.key"]
	if !ok {
		return nil, fmt.Errorf("failed to get client key from mTLS secret %s", client.ObjectKeyFromObject(mtlsSecret))
	}

	payloadCustomizer, err := defaultPayloadCustomizer()
	if err != nil {
		return nil, err
	}

	cfgOpts := []managercfg.Opt{
		WithRestConfig(r.RestConfig, r.KubeConfigPath),
		WithCacheSyncPeriod(r.CacheSyncPeriod),
		WithKongAdminService(types.NamespacedName{
			Name:      dataplaneAdminServiceName,
			Namespace: cp.Namespace,
		}),
		WithKongAdminServicePortName(consts.DataPlaneAdminServicePortName),
		WithKongAdminInitializationRetryDelay(5 * time.Second),
		// We only want to retry once as the constructor can be called multiple times.
		// Retries will be handled on the reconciler level.
		WithKongAdminInitializationRetries(1),
		WithGatewayAPIControllerName(),
		WithKongAdminAPIConfig(managercfg.AdminAPIClientConfig{
			CACert: string(caSecret.Data["tls.crt"]),
			TLSClient: managercfg.TLSClientConfig{
				Cert: string(clientCert),
				Key:  string(clientKey),
			},
		}),
		WithDisabledLeaderElection(),
		WithPublishService(types.NamespacedName{
			Namespace: cp.Namespace,
			Name:      dataplaneIngressServiceName,
		}),
		WithFeatureGates(logger, cp.Spec.FeatureGates),
		WithControllers(logger, cp.Spec.Controllers),
		WithIngressClass(cp.Spec.IngressClass),
		// We disable the metrics server by default, as all the ControlPlane metrics
		// are exposed via the operator's metrics server (through a shared, global
		// metrics registry handle).
		WithMetricsServerOff(),
		WithAnonymousReports(r.AnonymousReportsEnabled),
		WithAnonymousReportsFixedPayloadCustomizer(payloadCustomizer),
		WithClusterDomain(r.ClusterDomain),
		WithQPSAndBurst(apiServerQPS, apiServerBurst),
		WithEmitKubernetesEvents(r.EmitKubernetesEvents),
		WithTranslationOptions(cp.Spec.Translation),
		WithWatchNamespaces(validatedWatchNamespaces),
		WithKonnectOptions(cp.Spec.Konnect, konnectConfig),
	}

	if r.SecretLabelSelector != "" {
		cfgOpts = append(cfgOpts, WithSecretLabelSelectorMatchLabel(r.SecretLabelSelector, "true"))
	}
	if cp.Spec.ObjectFilters != nil && cp.Spec.ObjectFilters.Secrets != nil {
		for k, v := range cp.Spec.ObjectFilters.Secrets.MatchLabels {
			if k == r.SecretLabelSelector {
				return nil, fmt.Errorf("ControlPlane's secret label selector conficts with operator's secret label selector: %s", k)
			}
			cfgOpts = append(cfgOpts, WithSecretLabelSelectorMatchLabel(k, v))
		}
	}

	if r.ConfigMapLabelSelector != "" {
		cfgOpts = append(cfgOpts, WithConfigMapLabelSelectorMatchLabel(r.ConfigMapLabelSelector, "true"))
	}
	if cp.Spec.ObjectFilters != nil && cp.Spec.ObjectFilters.ConfigMaps != nil {
		for k, v := range cp.Spec.ObjectFilters.ConfigMaps.MatchLabels {
			if k == r.ConfigMapLabelSelector {
				return nil, fmt.Errorf("ControlPlane's configMap label selector conficts with operator's configMap label selector: %s", k)
			}
			cfgOpts = append(cfgOpts, WithConfigMapLabelSelectorMatchLabel(k, v))
		}
	}

	if dps := cp.Spec.DataPlaneSync; dps != nil {
		cfgOpts = append(
			cfgOpts,
			WithReverseSync(dps.ReverseSync),
		)
	}

	if cpgd := cp.Spec.GatewayDiscovery; cpgd != nil {
		cfgOpts = append(
			cfgOpts,
			WithGatewayDiscoveryReadinessCheckInterval(cpgd.ReadinessCheckInterval),
			WithGatewayDiscoveryReadinessCheckTimeout(cpgd.ReadinessCheckTimeout),
		)
	}

	if cp.Spec.Cache != nil && cp.Spec.Cache.InitSyncDuration != nil {
		cfgOpts = append(cfgOpts,
			WithInitCacheSyncDuration(cp.Spec.Cache.InitSyncDuration.Duration),
		)
	}

	if cp.Spec.DataPlaneSync != nil {
		cfgOpts = append(cfgOpts,
			WithDataPlaneSyncOptions(*cp.Spec.DataPlaneSync),
		)
	}

	if cp.Spec.ConfigDump != nil {
		if cp.Spec.ConfigDump.State == operatorv2beta1.ConfigDumpStateEnabled {
			cfgOpts = append(cfgOpts, WithConfigDumpEnabled(true))
		}
		if cp.Spec.ConfigDump.DumpSensitive == operatorv2beta1.ConfigDumpStateEnabled {
			cfgOpts = append(cfgOpts, WithSensitiveConfigDumpEnabled(true))
		}
	}

	switch cp.Spec.DataPlane.Type {
	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
		// If the ControlPlane is owned by a Gateway, we set the Gateway to be the only one to reconcile.
		owner, hasOwner := lo.Find(cp.GetOwnerReferences(), func(owner metav1.OwnerReference) bool {
			return strings.HasPrefix(owner.APIVersion, gatewayv1.GroupName) &&
				owner.Kind == "Gateway"
		})

		if hasOwner {
			cfgOpts = append(cfgOpts,
				WithGatewayToReconcile(types.NamespacedName{
					Namespace: cp.Namespace,
					Name:      owner.Name,
				}),
			)
		} else {
			return nil, fmt.Errorf(
				"no Gateway owner but spec.dataplane.type set to %s",
				gwtypes.ControlPlaneDataPlaneTargetManagedByType,
			)
		}

	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		cfgOpts = append(cfgOpts, WithGatewayAPIControllersDisabled())

	}

	return cfgOpts, nil
}

func (r *Reconciler) scheduleInstance(
	ctx context.Context,
	logger logr.Logger,
	mgrID manager.ID,
	cfg managercfg.Config,
) error {
	log.Debug(logger, "creating new instance", "manager_id", mgrID, "manager_config", cfg)
	mgr, err := manager.NewManager(ctx, mgrID, logger, cfg)
	if err != nil {
		return fmt.Errorf("failed to create manager: %w", err)
	}

	if err := r.InstancesManager.ScheduleInstance(mgr); err != nil {
		return fmt.Errorf("failed to schedule instance: %w", err)
	}
	return nil
}

func (r *Reconciler) ensureControlPlaneStatus(
	cp *ControlPlane,
	mgrCfg managercfg.Config,
) {
	cp.Status.Controllers = managerConfigToStatusControllers(mgrCfg)
	cp.Status.FeatureGates = managerConfigToStatusFeatureGates(mgrCfg)
}

func (r *Reconciler) initStatusToWaitingToBecomeReady(
	ctx context.Context,
	logger logr.Logger,
	cp *ControlPlane,
) (ctrl.Result, error) {
	k8sutils.SetCondition(
		k8sutils.NewCondition(
			kcfgdataplane.ReadyType,
			metav1.ConditionFalse,
			kcfgdataplane.WaitingToBecomeReadyReason,
			kcfgdataplane.WaitingToBecomeReadyMessage,
		),
		cp,
	)
	res, err := r.patchStatus(ctx, logger, cp)
	if err != nil {
		log.Debug(logger, "unable to patch ControlPlane status", "error", err)
		return ctrl.Result{}, err
	}
	if !res.IsZero() {
		log.Debug(logger, "unable to patch ControlPlane status")
		return res, nil
	}
	return ctrl.Result{RequeueAfter: requeueAfterBoot}, nil
}

// handleScheduleInstanceOutcome handles the outcome of r.scheduleInstance.
// It checks for transient errors, logs them, and requeues the resource with a patched status.
func (r *Reconciler) handleScheduleInstanceOutcome(
	ctx context.Context,
	logger logr.Logger,
	cp *ControlPlane,
	err error,
) (ctrl.Result, error) {
	var (
		endpointsError   = &ingresserrors.NoAvailableEndpointsError{}
		kongClientError  = &ingresserrors.KongClientNotReadyError{}
		conditionMessage string
	)

	// If the error is transient, we log it and requeue the resource. Such errors include:
	// - NoAvailableEndpointsError: indicates that there are no available endpoints for the dataplane;
	// - KongClientNotReadyError: indicates that the Kong client is not ready.
	// These errors are considered transient and will be retried after a delay.
	if errors.As(err, endpointsError) {
		conditionMessage = endpointsError.Error()
	} else if errors.As(err, kongClientError) {
		conditionMessage = kongClientError.Error()
	}
	if conditionMessage != "" {
		logger.Info("Transient error encountered while creating kong api clients, retrying after delay", "error", err, "retryDelay", requeueAfterBoot)
		k8sutils.SetCondition(
			k8sutils.NewCondition(
				kcfgdataplane.ReadyType,
				metav1.ConditionFalse,
				kcfgdataplane.WaitingToBecomeReadyReason,
				fmt.Sprintf("Unable to connect to data plane: %s", conditionMessage),
			),
			cp,
		)
		res, patchErr := r.patchStatus(ctx, logger, cp)
		if patchErr != nil {
			logger.Error(patchErr, "Failed to patch ControlPlane status")
			return ctrl.Result{}, patchErr
		}
		if !res.IsZero() {
			return res, nil
		}
		return ctrl.Result{RequeueAfter: requeueAfterBoot}, nil
	}
	return ctrl.Result{}, err
}

// validateControlPlaneOptions checks if the ControlPlane's options are valid with the operator specific configuration.
// It returns a boolean as the validation result and a string as the error message when the validation fails.
// For example, it fails when the label selectors of `Secret` and `ConfigMap` in `spec.objectFilters` conflict with opertor's label selectors for them.
func (r *Reconciler) validateControlPlaneOptions(cp *ControlPlane) (string, bool) {
	if cp.Spec.ObjectFilters != nil {

		if cp.Spec.ObjectFilters.Secrets != nil &&
			cp.Spec.ObjectFilters.Secrets.MatchLabels != nil &&
			r.SecretLabelSelector != "" {
			_, hasLabel := cp.Spec.ObjectFilters.Secrets.MatchLabels[r.SecretLabelSelector]
			if hasLabel {
				return fmt.Sprintf("secret label selector in spec.objectFilter conflicts with operator's secret label filter '%s'", r.SecretLabelSelector), false
			}
		}

		if cp.Spec.ObjectFilters.ConfigMaps != nil &&
			cp.Spec.ObjectFilters.ConfigMaps.MatchLabels != nil &&
			r.ConfigMapLabelSelector != "" {
			_, hasLabel := cp.Spec.ObjectFilters.ConfigMaps.MatchLabels[r.ConfigMapLabelSelector]
			if hasLabel {
				return fmt.Sprintf("configMap label selector in spec.objectFilter conflicts with operator's configMap label filter '%s'", r.ConfigMapLabelSelector), false
			}
		}
	}

	return "", true
}

func (r *Reconciler) enforceDataPlaneNameInStatus(
	ctx context.Context,
	cp *gwtypes.ControlPlane,
) (string, op.Result, error) {
	var dataplaneName string
	switch cp.Spec.DataPlane.Type {
	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		dataplaneName = cp.Spec.DataPlane.Ref.Name
	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
		owner, hasOwner := lo.Find(cp.GetOwnerReferences(), func(owner metav1.OwnerReference) bool {
			return strings.HasPrefix(owner.APIVersion, gatewayv1.GroupName) &&
				owner.Kind == "Gateway"
		})

		if !hasOwner {
			return "", op.Noop, fmt.Errorf(
				"no Gateway owner but spec.dataplane.type set to %s",
				gwtypes.ControlPlaneDataPlaneTargetManagedByType,
			)
		}

		var dataplaneList operatorv1beta1.DataPlaneList
		err := r.List(ctx, &dataplaneList, client.MatchingFields{
			index.DataPlaneOnOwnerGatewayIndex: cp.Namespace + "/" + owner.Name,
		})
		if err != nil {
			return "", op.Noop, fmt.Errorf(
				"failed to find DataPlanes owned by Gateway %s/%s: %w",
				cp.GetNamespace(), owner.Name, err,
			)
		}
		if l := len(dataplaneList.Items); l != 1 {
			return "", op.Noop, fmt.Errorf(
				"expected 1 but got %d DataPlanes owned by Gateway %s/%s",
				l, cp.GetNamespace(), owner.Name,
			)
		}
		dataplaneName = dataplaneList.Items[0].Name
	}

	if (cp.Status.DataPlane != nil && dataplaneName == cp.Status.DataPlane.Name) ||
		(cp.Status.DataPlane == nil && dataplaneName == "") {
		return dataplaneName, op.Noop, nil
	}
	oldControlPlane := cp.DeepCopy()
	cp.Status.DataPlane = &gwtypes.ControlPlaneDataPlaneStatus{
		Name: dataplaneName,
	}
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(oldControlPlane)); err != nil {
		return "", op.Noop, fmt.Errorf(
			"failed to patch ControlPlane status with DataPlane name %s: %w",
			dataplaneName, err,
		)
	}
	return dataplaneName, op.Updated, nil
}

func controlPlaneStatusEqual(
	a, b *ControlPlane,
) bool {
	return !k8sutils.ConditionsNeedsUpdate(a, b) &&
		reflect.DeepEqual(b.Status.Controllers, a.Status.Controllers) &&
		reflect.DeepEqual(b.Status.FeatureGates, a.Status.FeatureGates) &&
		reflect.DeepEqual(b.Status.DataPlane, a.Status.DataPlane)
}
