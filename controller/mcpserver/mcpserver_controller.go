package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
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
	SdkFactory        sdkops.SDKFactory
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

	kongSvcHost, err := r.ensureKongEntities(ctx, mcpServer, &cp, &apiAuth)
	if err != nil {
		return fmt.Errorf("failed to ensure Kong entities for MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
	}

	if kongSvcHost != "" {
		svcName := strings.TrimSuffix(kongSvcHost, ".svc.cluster.local")
		if err := r.ensureService(ctx, mcpServer, svcName); err != nil {
			return fmt.Errorf("failed to ensure Service for MCPServer %s/%s: %w", mcpServer.Namespace, mcpServer.Name, err)
		}
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

// ensureService creates the MCP server Service for the given MCPServer if it
// does not already exist.
func (r *MCPServerReconciler) ensureService(ctx context.Context, mcpServer *konnectv1alpha1.MCPServer, serviceName string) error {
	svc := buildService(mcpServer, serviceName)
	if err := controllerutil.SetControllerReference(mcpServer, svc, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on Service %s/%s: %w", svc.Namespace, svc.Name, err)
	}

	var existing corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check Service %s/%s: %w", svc.Namespace, svc.Name, err)
	}

	if err := r.Create(ctx, svc); err != nil {
		return fmt.Errorf("failed to create Service %s/%s: %w", svc.Namespace, svc.Name, err)
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

// buildService constructs the MCP server Service for the given MCPServer.
func buildService(mcpServer *konnectv1alpha1.MCPServer, serviceName string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: mcpServer.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": mcpServer.Name},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt32(8080),
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

// ensureKongEntities fetches the Kong entities (KongService and KongRoute) for the
// MCPServer from Konnect and creates the corresponding Kubernetes CRs if they do not
// already exist.
func (r *MCPServerReconciler) ensureKongEntities(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (string, error) {
	token, err := tokenFromKonnectAPIAuth(ctx, r.Client, apiAuth)
	if err != nil {
		return "", fmt.Errorf("failed to get token from KonnectAPIAuthConfiguration: %w", err)
	}

	srv, err := server.NewServer[konnectv1alpha1.KonnectGatewayControlPlane](apiAuth.Spec.ServerURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse server URL %q: %w", apiAuth.Spec.ServerURL, err)
	}

	konnectClient := r.SdkFactory.NewKonnectSDK(srv, sdkops.SDKToken(token))

	resp, err := konnectClient.GetMCPServersSDK().GetMcpServerKongEntities(ctx,
		sdkkonnectops.GetMcpServerKongEntitiesRequest{
			ControlPlaneID: cp.GetKonnectID(),
			McpServerID:    string(mcpServer.Spec.Mirror.Konnect.ID),
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get Kong entities for MCP server %s: %w", mcpServer.Spec.Mirror.Konnect.ID, err)
	}
	if resp.StatusCode != http.StatusOK || resp.KongEntitiesResponse == nil {
		return "", nil
	}

	// Build a map from Konnect service ID to Kubernetes object name so that
	// routes can reference the correct KongService by name.
	var firstHost string
	serviceIDToName := make(map[string]string, len(resp.KongEntitiesResponse.Services))
	for _, svc := range resp.KongEntitiesResponse.Services {
		if err := r.ensureKongService(ctx, mcpServer, cp, svc); err != nil {
			return "", fmt.Errorf("failed to ensure KongService %q: %w", svc.Name, err)
		}
		if svc.ID != nil {
			serviceIDToName[*svc.ID] = svc.Name
		}
		if firstHost == "" {
			firstHost = svc.Host
		}
	}

	for _, route := range resp.KongEntitiesResponse.Routes {
		if err := r.ensureKongRoute(ctx, mcpServer, cp, route, serviceIDToName); err != nil {
			return "", fmt.Errorf("failed to ensure KongRoute %q: %w", route.Name, err)
		}
	}

	return firstHost, nil
}

// ensureKongService creates a KongService CR for the given SDK service if one does
// not already exist.
func (r *MCPServerReconciler) ensureKongService(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	svc sdkkonnectcomp.KongService,
) error {
	name := svc.Name
	path := svc.Path
	kongService := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name,
			Namespace: mcpServer.Namespace,
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Host:     svc.Host,
				Port:     svc.Port,
				Path:     &path,
				Protocol: sdkkonnectcomp.Protocol(svc.Protocol),
				Name:     &name,
			},
		},
	}
	if err := controllerutil.SetControllerReference(mcpServer, kongService, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on KongService %s/%s: %w", kongService.Namespace, kongService.Name, err)
	}

	var existing configurationv1alpha1.KongService
	if err := r.Get(ctx, types.NamespacedName{Name: kongService.Name, Namespace: kongService.Namespace}, &existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check KongService %s/%s: %w", kongService.Namespace, kongService.Name, err)
	}

	if err := r.Create(ctx, kongService); err != nil {
		return fmt.Errorf("failed to create KongService %s/%s: %w", kongService.Namespace, kongService.Name, err)
	}
	return nil
}

// ensureKongRoute creates a KongRoute CR for the given SDK route if one does not
// already exist. serviceIDToName maps Konnect service IDs to their Kubernetes
// KongService object names, used to build the ServiceRef.
func (r *MCPServerReconciler) ensureKongRoute(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	route sdkkonnectcomp.KongRoute,
	serviceIDToName map[string]string,
) error {
	name := route.Name
	kongRoute := &configurationv1alpha1.KongRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      route.Name,
			Namespace: mcpServer.Namespace,
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Name:    &name,
				Methods: route.Methods,
				Paths:   route.Paths,
			},
		},
	}

	if route.Service != nil && route.Service.ID != nil {
		if svcName, ok := serviceIDToName[*route.Service.ID]; ok {
			kongRoute.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: svcName,
				},
			}
		}
	} else {
		kongRoute.Spec.ControlPlaneRef = &commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: cp.Name,
			},
		}
	}

	if err := controllerutil.SetControllerReference(mcpServer, kongRoute, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference on KongRoute %s/%s: %w", kongRoute.Namespace, kongRoute.Name, err)
	}

	var existing configurationv1alpha1.KongRoute
	if err := r.Get(ctx, types.NamespacedName{Name: kongRoute.Name, Namespace: kongRoute.Namespace}, &existing); err == nil {
		return nil // already exists
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check KongRoute %s/%s: %w", kongRoute.Namespace, kongRoute.Name, err)
	}

	if err := r.Create(ctx, kongRoute); err != nil {
		return fmt.Errorf("failed to create KongRoute %s/%s: %w", kongRoute.Namespace, kongRoute.Name, err)
	}
	return nil
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
