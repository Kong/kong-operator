package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-ingress-controller/v3/pkg/manager"
	managercfg "github.com/kong/kubernetes-ingress-controller/v3/pkg/manager/config"
	"github.com/kong/kubernetes-ingress-controller/v3/pkg/manager/multiinstance"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller"
	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/pkg/consts"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// Reconciler reconciles a ControlPlane object
type Reconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	ClusterCAKeyConfig       secrets.KeyConfig
	DevelopmentMode          bool

	RestConfig       *rest.Config
	InstancesManager *multiinstance.Manager
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&operatorv1beta1.ControlPlane{}).Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "controlplane", r.DevelopmentMode)

	log.Trace(logger, "reconciling ControlPlane resource")
	cp := new(operatorv1beta1.ControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	mgrID, err := manager.NewID(cp.Name)
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
		controllerutil.RemoveFinalizer(cp, string(ControlPlaneFinalizerCPInstanceTeardown))
		if err := r.Client.Update(ctx, cp); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
		}

		// cleanup completed
		log.Debug(logger, "resource cleanup completed, controlplane deleted")
		return ctrl.Result{}, nil
	}

	// ensure the controlplane has a finalizer to delete owned cluster wide resources on delete.
	finalizerSet := controllerutil.AddFinalizer(cp, string(ControlPlaneFinalizerCPInstanceTeardown))
	if finalizerSet {
		log.Trace(logger, "setting finalizers")
		if err := r.Client.Update(ctx, cp); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's finalizers : %w", err)
		}
		// Requeue to ensure that we do not miss next reconciliation request in case
		// AddFinalizer calls returned true but the update resulted in a noop.
		return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
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
	dataplane, err := gatewayutils.GetDataPlaneForControlPlane(ctx, r.Client, cp)
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
	defaultArgs := controlplane.DefaultsArgs{
		Namespace:                   cp.Namespace,
		ControlPlaneName:            cp.Name,
		DataPlaneIngressServiceName: dataplaneIngressServiceName,
		DataPlaneAdminServiceName:   dataplaneAdminServiceName,
		AnonymousReportsEnabled:     controlplane.DeduceAnonymousReportsEnabled(r.DevelopmentMode, &cp.Spec.ControlPlaneOptions),
	}
	for _, owner := range cp.OwnerReferences {
		if strings.HasPrefix(owner.APIVersion, gatewayv1.GroupName) && owner.Kind == "Gateway" {
			defaultArgs.OwnedByGateway = owner.Name
			continue
		}
	}
	changed := controlplane.SetDefaults(
		&cp.Spec.ControlPlaneOptions,
		defaultArgs)
	if changed {
		log.Debug(logger, "updating ControlPlane resource after defaults are set since resource has changed")
		err := r.Client.Update(ctx, cp)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane resource, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane: %w", err)
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	// TODO(czeslavo): Make sure we reschedule the instance if the spec has changed.

	log.Trace(logger, "validating ControlPlane's DataPlane status")
	dataplaneIsSet := r.ensureDataPlaneStatus(cp, dataplane)
	if dataplaneIsSet {
		log.Trace(logger, "DataPlane is set, deployment for ControlPlane will be provisioned")
	} else {
		log.Debug(logger, "DataPlane not set, deployment for ControlPlane will remain dormant")
	}

	// TODO: before creating a manager, verify that the Admin API service has endpoints OR
	// handle more gracefully:
	//

	log.Trace(logger, "checking readiness of ControlPlane instance")
	if err := r.InstancesManager.IsInstanceReady(mgrID); err != nil {
		log.Trace(logger, "control plane instance not ready yet", "error", err)

		if errors.As(err, &multiinstance.InstanceNotFoundError{}) {
			log.Debug(logger, "control plane instance not found, creating new instance")

			var caSecret corev1.Secret
			if err := r.Get(ctx, types.NamespacedName{
				Namespace: r.ClusterCASecretNamespace,
				Name:      r.ClusterCASecretName,
			}, &caSecret); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to get CA secret: %w", err)
			}

			log.Trace(logger, "creating mTLS certificate")
			clientCert, clientKey, err := r.generateClientCert(client.ObjectKeyFromObject(cp), caSecret)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate client certificate: %w", err)
			}

			mgrCfg, err := manager.NewConfig(
				WithRestConfig(r.RestConfig),
				WithKongAdminService(types.NamespacedName{
					Name:      dataplaneAdminServiceName,
					Namespace: cp.Namespace,
				}),
				WithKongAdminServicePortName(consts.DataPlaneAdminServicePortName),
				// Don't retry when Kong Admin API is not available to not block the reconciliation.
				WithKongAdminInitializationRetryAttempts(1),
				WithGatewayToReconcile(types.NamespacedName{
					Namespace: cp.Namespace,
					Name:      defaultArgs.OwnedByGateway,
				}),
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
				WithMetricsServerOff(),
			)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create manager config: %w", err)
			}

			// TODO: set POD_NAME and POD_NAMESPACE (or rather change it in the manager to not use env vars)

			log.Debug(logger, "creating new instance", "manager_id", mgrID, "manager_config", mgrCfg)
			mgr, err := manager.NewManager(ctx, mgrID, logger, mgrCfg)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create manager: %w", err)
			}

			if err := r.InstancesManager.ScheduleInstance(mgr); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to schedule instance: %w", err)
			}
		}

		k8sutils.SetCondition(
			k8sutils.NewCondition(consts.ReadyType, metav1.ConditionFalse, consts.WaitingToBecomeReadyReason, consts.WaitingToBecomeReadyMessage),
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

		// Give the instance some time to start up.
		return ctrl.Result{RequeueAfter: time.Second}, nil
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
func (r *Reconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *operatorv1beta1.ControlPlane) (ctrl.Result, error) {
	current := &operatorv1beta1.ControlPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		log.Debug(logger, "patching ControlPlane status", "status", updated.Status)
		if err := r.Client.Status().Patch(ctx, updated, client.MergeFrom(current)); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying")
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's status : %w", err)
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) generateClientCert(
	cp types.NamespacedName,
	caSecret corev1.Secret,
) ([]byte, []byte, error) {
	priv, privPem, signatureAlgorithm, err := secrets.CreatePrivateKey(r.ClusterCAKeyConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create private key: %w", err)
	}

	subject := fmt.Sprintf("%s.%s", cp.Name, cp.Namespace)
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   subject,
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SignatureAlgorithm: signatureAlgorithm,
		DNSNames:           []string{subject},
	}

	caCertBlock, _ := pem.Decode(caSecret.Data["tls.crt"])
	if caCertBlock == nil {
		return nil, nil, errors.New("failed to decode CA certificate")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}
	caKeyBlock, _ := pem.Decode(caSecret.Data["tls.key"])
	if caKeyBlock == nil {
		return nil, nil, errors.New("failed to decode CA key")
	}

	caSigner, signatureAlgorithm, err := secrets.ParsePrivateKey(caKeyBlock)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse CA key: %w", err)
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate request: %w", err)
	}

	policy := &config.Signing{
		Default: &config.SigningProfile{
			Usage: []string{
				string(certificatesv1.UsageKeyEncipherment),
				string(certificatesv1.UsageDigitalSignature),
				string(certificatesv1.UsageClientAuth),
			},
			Expiry: time.Hour * 24 * 365,
		},
	}
	cfs, err := local.NewSigner(caSigner, caCert, signatureAlgorithm, policy)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create signer: %w", err)
	}

	cert, err := cfs.Sign(signer.SignRequest{
		Request: string(pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE REQUEST",
			Bytes: der,
		})),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sign certificate: %w", err)
	}

	return cert, pem.EncodeToMemory(privPem), nil
}
