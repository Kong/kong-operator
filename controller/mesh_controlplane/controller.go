package controlplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"

	"github.com/kong/gateway-operator/controller"
	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// Reconciler reconciles a ControlPlane object
type Reconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	DevelopmentMode          bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// watch ControlPlane objects
		For(&operatorv1alpha1.MeshControlPlane{}).
		// watch for changes in Secrets created by the controlplane controller
		Owns(&corev1.Secret{}).
		// watch for changes in ServiceAccounts created by the controlplane controller
		Owns(&corev1.ServiceAccount{}).
		// watch for changes in Deployments created by the controlplane controller
		Owns(&appsv1.Deployment{}).
		// watch for changes in Services created by the controlplane controller
		Owns(&corev1.Service{}).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "controlplane", r.DevelopmentMode)

	log.Trace(logger, "reconciling MeshControlPlane resource", req)
	cp := new(operatorv1alpha1.MeshControlPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	res, err := r.ensureWebhookResources(ctx, logger, cp)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure webhook resources: %w", err)
	} else if res != op.Noop {
		return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
	}

	// log.Trace(logger, "looking for existing Deployments for ControlPlane resource", cp)
	// res, controlplaneDeployment, err := r.ensureDeployment(ctx, logger, deploymentParams)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }
	// log.Trace(logger, "checking readiness of ControlPlane deployments", cp)
	return ctrl.Result{}, nil
}

// validateControlPlane validates the control plane.
func validateControlPlane(controlPlane *operatorv1beta1.ControlPlane, devMode bool) error {
	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !devMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsControlPlaneImageVersionSupported)
	}
	_, err := controlplane.GenerateImage(&controlPlane.Spec.ControlPlaneOptions, versionValidationOptions...)
	return err
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *Reconciler) patchStatus(ctx context.Context, logger logr.Logger, updated *operatorv1beta1.ControlPlane) (ctrl.Result, error) {
	current := &operatorv1beta1.ControlPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		log.Debug(logger, "patching ControlPlane status", updated, "status", updated.Status)
		if err := r.Client.Status().Patch(ctx, updated, client.MergeFrom(current)); err != nil {
			if k8serrors.IsConflict(err) {
				log.Debug(logger, "conflict found when updating ControlPlane, retrying", current)
				return ctrl.Result{Requeue: true, RequeueAfter: controller.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed updating ControlPlane's status : %w", err)
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureWebhookResources(
	ctx context.Context, logger logr.Logger, cp *operatorv1alpha1.MeshControlPlane,
) (op.Result, error) {
	log.Trace(logger, "ensuring admission webhook service", cp)
	res, admissionWebhookService, err := r.ensureAdmissionWebhookService(ctx, logger, r.Client, cp)
	if err != nil {
		return res, fmt.Errorf("failed to ensure admission webhook service: %w", err)
	}
	if res != op.Noop {
		log.Debug(logger, "admission webhook service has been created/updated", cp)
		return res, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "ensuring admission webhook configuration", cp)
	res, err = r.ensureValidatingWebhookConfiguration(ctx, cp, admissionWebhookService)
	if err != nil {
		return res, err
	}
	if res != op.Noop {
		log.Debug(logger, "ValidatingWebhookConfiguration has been created/updated", cp)
	}
	return res, nil
}

func isAdmissionWebhookEnabled(ctx context.Context, cl client.Client, logger logr.Logger, cp *operatorv1beta1.ControlPlane) bool {
	if cp.Spec.Deployment.PodTemplateSpec == nil {
		return false
	}

	container := k8sutils.GetPodContainerByName(&cp.Spec.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		return false
	}
	admissionWebhookListen, ok, err := k8sutils.GetEnvValueFromContainer(ctx, container, cp.Namespace, "CONTROLLER_ADMISSION_WEBHOOK_LISTEN", cl)
	if err != nil {
		log.Debug(logger, "unable to get CONTROLLER_ADMISSION_WEBHOOK_LISTEN env var", cp, "error", err)
		return false
	}
	if !ok {
		return false
	}
	// We don't validate the value of the env var here, just that it is set.
	return len(admissionWebhookListen) > 0 && admissionWebhookListen != "off"
}
