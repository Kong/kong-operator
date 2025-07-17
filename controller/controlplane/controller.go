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

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	ctrlconsts "github.com/kong/kong-operator/controller/consts"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
	operatorerrors "github.com/kong/kong-operator/internal/errors"
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
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			CacheSyncTimeout: r.CacheSyncTimeout,
		}).
		For(&ControlPlane{}).
		// Watch for changes in Secret objects that are owned by ControlPlane objects.
		Owns(&corev1.Secret{})

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

	log.Trace(logger, "retrieving connected dataplane")
	dataplane, err := GetDataPlaneForControlPlane(ctx, r.Client, cp)
	var dataplaneIngressServiceName, dataplaneAdminServiceName string
	if err != nil {
		if !errors.Is(err, operatorerrors.ErrDataPlaneNotSet) {
			return ctrl.Result{}, err
		}
		log.Debug(logger, "no existing dataplane for controlplane", "error", err)
	} else {
		dataplaneIngressServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneIngressServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane ingress service for controlplane", "error", err)
			return ctrl.Result{}, err
		}

		dataplaneAdminServiceName, err = gatewayutils.GetDataPlaneServiceName(ctx, r.Client, dataplane, consts.DataPlaneAdminServiceLabelValue)
		if err != nil {
			log.Debug(logger, "no existing dataplane admin service for controlplane", "error", err)
			return ctrl.Result{}, err
		}
	}

	log.Trace(logger, "configuring ControlPlane resource")

	log.Trace(logger, "validating ControlPlane's DataPlane status")
	dataplaneIsSet, err := r.ensureDataPlaneStatus(cp, dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure DataPlane status: %w", err)
	}
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned")
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant")
	}

	// TODO: Make sure there are no error logs from the manager constructor when DP instances are not ready.
	// https://github.com/kong/kong-operator/issues/1375

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
			)
			if err != nil {
				return ctrl.Result{}, err
			}

			mgrCfg, err := manager.NewConfig(cfgOpts...)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create manager config: %w", err)
			}
			if err := r.scheduleInstance(ctx, logger, mgrID, mgrCfg); err != nil {
				return ctrl.Result{}, err
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
				return ctrl.Result{}, fmt.Errorf("failed to schedule instance: %w", err)
			}
			r.ensureControlPlaneStatus(cp, mgrCfg)

			return r.initStatusToWaitingToBecomeReady(ctx, logger, cp)
		}
	}

	markAsProvisioned(cp)
	k8sutils.SetReady(cp)

	result, err := r.patchStatus(ctx, logger, cp)
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

	if !k8sutils.ConditionsNeedsUpdate(current, updated) &&
		reflect.DeepEqual(updated.Status.Controllers, current.Status.Controllers) &&
		reflect.DeepEqual(updated.Status.FeatureGates, current.Status.FeatureGates) {
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

		// TODO: https://github.com/kong/kong-operator/issues/1749 metrics.
		WithMetricsServerOff(),
		WithAnonymousReports(r.AnonymousReportsEnabled),
		WithAnonymousReportsFixedPayloadCustomizer(payloadCustomizer),
	}

	if cp.Spec.GatewayDiscovery != nil {
		cfgOpts = append(cfgOpts,
			WithGatewayDiscoveryReadinessCheckInterval(cp.Spec.GatewayDiscovery.ReadinessCheckInterval),
			WithGatewayDiscoveryReadinessCheckTimeout(cp.Spec.GatewayDiscovery.ReadinessCheckTimeout),
		)
	}

	if cp.Spec.Cache != nil && cp.Spec.Cache.InitSyncDuration != nil {
		cfgOpts = append(cfgOpts,
			WithInitCacheSyncDuration(cp.Spec.Cache.InitSyncDuration.Duration),
		)
	}

	// If the ControlPlane is owned by a Gateway, we set the Gateway to be the only one to reconcile.
	if owner, ok := lo.Find(cp.GetOwnerReferences(), func(owner metav1.OwnerReference) bool {
		return strings.HasPrefix(owner.APIVersion, gatewayv1.GroupName) &&
			owner.Kind == "Gateway"
	}); ok {
		cfgOpts = append(cfgOpts,
			WithGatewayToReconcile(types.NamespacedName{
				Namespace: cp.Namespace,
				Name:      owner.Name,
			}),
		)
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
