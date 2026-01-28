package mcprunner

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/pkg/mcprunner"
)

const (
	// ControllerName is the name used for logging and event recording in the hybrid gateway controller.
	ControllerName = "mcprunner"
)

// Reconciler reconciles a KongMCPRunner object.
type Reconciler struct {
	client.Client

	// mockKonnectClient is a client for interacting with the MCP Runner API.
	mockKonnectClient *mcprunner.Client
}

func NewMCPRunnerReconciler(mgr ctrl.Manager) *Reconciler {
	logger := ctrllog.FromContext(context.Background()).WithName(ControllerName).WithName("mockclient")

	return &Reconciler{
		Client: mgr.GetClient(),
		mockKonnectClient: mcprunner.NewClient(
			logger,
			mcprunner.WithOnRunnersFound(
				mgr.GetClient(),
				ensureMCPRunners,
			),
		),
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// TODO: handle collection of errors and routine shutdown
	go r.mockKonnectClient.Start(ctx)

	return ctrl.NewControllerManagedBy(mgr).
		For(&configurationv1alpha1.KongMCPRunner{}).
		Complete(r)
}

// Reconcile reconciles the KongMCPRunner resource.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx).WithName(ControllerName)

	var mcpRunner configurationv1alpha1.KongMCPRunner
	if err := r.Get(ctx, req.NamespacedName, &mcpRunner); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Trace(logger, "reconciling kongmcprunner", "name", mcpRunner.Name, "namespace", mcpRunner.Namespace)

	// Handle deletion
	if mcpRunner.GetDeletionTimestamp() != nil {
		log.Debug(logger, "kongmcprunner is being deleted")
		return ctrl.Result{}, nil
	}

	runner, err := r.mockKonnectClient.GetRunner(ctx, string(mcpRunner.Spec.Mirror.Konnect.ID))
	if err != nil {
		log.Error(logger, err, "failed to get runner from MCP API", "konnectID", mcpRunner.Spec.Mirror.Konnect.ID)
		return ctrl.Result{}, err
	}

	// Update status with Konnect ID and version
	oldMCPRunner := mcpRunner.DeepCopy()
	if mcpRunner.Status.Konnect == nil {
		mcpRunner.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{}
	}
	mcpRunner.Status.Konnect.ID = runner.ID
	mcpRunner.Status.Version = &runner.Version

	if err := r.Status().Patch(ctx, &mcpRunner, client.MergeFrom(oldMCPRunner)); err != nil {
		log.Error(logger, err, "failed to update KongMCPRunner status")
		return ctrl.Result{}, err
	}
	log.Info(logger, "updated KongMCPRunner status", "konnectID", runner.ID, "version", runner.Version)

	// Ensure Secret with runner code URL
	if err := r.ensureSecret(ctx, logger, &mcpRunner, runner.ID); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure Deployment with BusyBox
	if err := r.ensureDeployment(ctx, logger, &mcpRunner); err != nil {
		return ctrl.Result{}, err
	}

	log.Info(logger, "reconciliation complete for kongMCPRunner resource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) ensureSecret(ctx context.Context, logger logr.Logger, mcpRunner *configurationv1alpha1.KongMCPRunner, runnerID string) error {
	secret := &corev1.Secret{}
	secret.Name = mcpRunner.Name + "-code"
	secret.Namespace = mcpRunner.Namespace
	secret.StringData = map[string]string{
		"url": fmt.Sprintf("http://localhost:1234/runners/%s/code", runnerID),
	}

	// Set owner reference so the secret is deleted with the MCPRunner
	if err := controllerutil.SetControllerReference(mcpRunner, secret, r.Scheme()); err != nil {
		log.Error(logger, err, "failed to set owner reference on secret")
		return err
	}

	if err := r.Create(ctx, secret); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			log.Error(logger, err, "failed to create secret")
			return err
		}
		log.Info(logger, "secret already exists", "name", secret.Name)
	} else {
		log.Info(logger, "created secret with runner code URL", "name", secret.Name, "url", secret.StringData["url"])
	}

	return nil
}

func (r *Reconciler) ensureDeployment(ctx context.Context, logger logr.Logger, mcpRunner *configurationv1alpha1.KongMCPRunner) error {
	deployment := &appsv1.Deployment{}
	deployment.Name = mcpRunner.Name + "-runner"
	deployment.Namespace = mcpRunner.Namespace
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: lo.ToPtr(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": mcpRunner.Name,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"app": mcpRunner.Name,
				},
			},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "print-code-url",
						Image: "busybox:latest",
						Command: []string{
							"sh",
							"-c",
							"echo 'Runner Code URL:' && cat /secrets/url",
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "code-secret",
								MountPath: "/secrets",
								ReadOnly:  true,
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "busybox",
						Image: "busybox:latest",
						Command: []string{
							"sh",
							"-c",
							"echo 'Runner is active' && sleep 3600",
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "code-secret",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: mcpRunner.Name + "-code",
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference so the deployment is deleted with the MCPRunner
	if err := controllerutil.SetControllerReference(mcpRunner, deployment, r.Scheme()); err != nil {
		log.Error(logger, err, "failed to set owner reference on deployment")
		return err
	}

	if err := r.Create(ctx, deployment); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			log.Error(logger, err, "failed to create deployment")
			return err
		}
		log.Info(logger, "deployment already exists", "name", deployment.Name)
	} else {
		log.Info(logger, "created deployment with busybox", "name", deployment.Name)
	}

	return nil
}
