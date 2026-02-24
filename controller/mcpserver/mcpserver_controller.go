package mcpserver

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// MCPServerReconciler reconciles a MCPServer object.
type MCPServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SignalManager     *SignalManager
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.SignalManager.run(ctx)

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.MCPServer{}).
		Complete(r)
}

// Reconcile reconciles the MCPServer resource.
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	var mcpServer konnectv1alpha1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &mcpServer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle pre-deletion: notify the signal manager to reset the polling offset
	// so the next poll picks up any changes caused by the deletion, then remove
	// the finalizer to allow Kubernetes to garbage-collect the object.
	if !mcpServer.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&mcpServer, mcpServerFinalizer) {
			if cpName := ownerControlPlaneName(&mcpServer); cpName != "" {
				r.SignalManager.NotifyMCPServerDeleted(mcpServer.Namespace, cpName)
			}
			controllerutil.RemoveFinalizer(&mcpServer, mcpServerFinalizer)
			if err := r.Update(ctx, &mcpServer); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
			}
		}
		return ctrl.Result{}, nil
	}

	log.Info(logger, "reconciling MCPServer", "namespace", mcpServer.Namespace, "name", mcpServer.Name)

	if err := r.ensureDeployment(ctx, &mcpServer); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureDeployment creates the MCP server Deployment for the given MCPServer if
// it does not already exist.
func (r *MCPServerReconciler) ensureDeployment(ctx context.Context, mcpServer *konnectv1alpha1.MCPServer) error {
	if mcpServer.Spec.Mirror == nil {
		return nil
	}

	cpName := ownerControlPlaneName(mcpServer)
	if cpName == "" {
		return nil
	}

	var cp konnectv1alpha1.KonnectGatewayControlPlane
	if err := r.Get(ctx, types.NamespacedName{Name: cpName, Namespace: mcpServer.Namespace}, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectGatewayControlPlane %s/%s: %w", mcpServer.Namespace, cpName, err)
	}

	authRef := cp.GetKonnectAPIAuthConfigurationRef()
	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Get(ctx, types.NamespacedName{Name: authRef.Name, Namespace: mcpServer.Namespace}, &apiAuth); err != nil {
		return fmt.Errorf("failed to get KonnectAPIAuthConfiguration %s/%s: %w", mcpServer.Namespace, authRef.Name, err)
	}

	deployment := buildDeployment(mcpServer, &cp, &apiAuth)
	if err := controllerutil.SetControllerReference(mcpServer, deployment, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on Deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
	}

	var existing appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, &existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check Deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
	}

	if err := r.Create(ctx, deployment); err != nil {
		return fmt.Errorf("failed to create Deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
	}
	return nil
}

// buildDeployment constructs the MCP server Deployment for the given MCPServer.
func buildDeployment(
	mcpServer *konnectv1alpha1.MCPServer,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) *appsv1.Deployment {
	replicas := int32(1)
	patEnvVar := buildPATEnvVar(apiAuth)
	mcpServerID := string(mcpServer.Spec.Mirror.Konnect.ID)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": mcpServer.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": mcpServer.Name},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "init",
							Image: "localhost/mcp-server-init:demo",
							Args: []string{
								"-cp-url", apiAuth.Spec.ServerURL,
								"-cp-id", cp.GetKonnectID(),
								"-mcp-server-id", mcpServerID,
								"-output-path", "/mcp-server/app.py",
								"-pat", "$(PAT)",
							},
							Env: []corev1.EnvVar{patEnvVar},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "mcp-code", MountPath: "/mcp-server"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "alpine",
							Image:   "alpine",
							Command: []string{"sh", "-c", "cat /mcp-server/app.py && echo && sleep infinity"},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "mcp-code", MountPath: "/mcp-server"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name:         "mcp-code",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
				},
			},
		},
	}
}

// buildPATEnvVar returns a PAT environment variable sourced from the
// KonnectAPIAuthConfiguration: a SecretKeyRef for secretRef auth type,
// or a direct value for token auth type.
func buildPATEnvVar(apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration) corev1.EnvVar {
	if apiAuth.Spec.Type == konnectv1alpha1.KonnectAPIAuthTypeSecretRef && apiAuth.Spec.SecretRef != nil {
		return corev1.EnvVar{
			Name: "PAT",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: apiAuth.Spec.SecretRef.Name},
					Key:                  "token",
				},
			},
		}
	}
	return corev1.EnvVar{Name: "PAT", Value: apiAuth.Spec.Token}
}

// ownerControlPlaneName returns the name of the KonnectGatewayControlPlane that
// owns the given MCPServer, or an empty string if no such owner is found.
func ownerControlPlaneName(mcpServer *konnectv1alpha1.MCPServer) string {
	for _, ref := range mcpServer.OwnerReferences {
		if ref.Kind == "KonnectGatewayControlPlane" {
			return ref.Name
		}
	}
	return ""
}
