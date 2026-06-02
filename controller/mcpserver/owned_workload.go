package mcpserver

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

const (
	// mcpServerVersionAnnotationKey is the annotation key used to store the
	// remote MCP server version on the owned Deployment's pod template.
	mcpServerVersionAnnotationKey = "kong-operator.konghq.com/mcp-server-version"
)

// generateWorkloadNN returns the NamespacedName for resources owned by the
// given MCPServer. All owned resources share the MCPServer's own name.
func generateWorkloadNN(mcpServer *konnectv1alpha1.MCPServer) types.NamespacedName {
	return types.NamespacedName{
		Namespace: mcpServer.Namespace,
		Name:      fmt.Sprintf("mcpserver-%s", mcpServer.Name),
	}
}

// ----------------------------------------------------------------------------
// Deployment
// ----------------------------------------------------------------------------

// ensureDeployment reconciles the Deployment for the given MCPServer using
// Server-Side Apply. It returns the live Deployment after the apply so callers
// can derive ReplicaSet/Pod version statuses from it.
func (r *MCPServerReconciler) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	mcpServer *konnectv1alpha1.MCPServer,
	remoteMCPServer *sdkkonnectcomp.MCPServerCPInfo,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (*appsv1.Deployment, error) {
	if remoteMCPServer.InitContainer == nil {
		return nil, fmt.Errorf("remote MCPServer %s is missing init container info", remoteMCPServer.ID)
	}
	if remoteMCPServer.Container == nil {
		return nil, fmt.Errorf("remote MCPServer %s is missing container info", remoteMCPServer.ID)
	}

	desired := generateDeployment(mcpServer, *remoteMCPServer, apiAuth)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.typeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, "DeploymentFailed", "ApplyDeployment",
			"Failed to apply Deployment: %v", err)
		return nil, fmt.Errorf("failed to apply Deployment for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Deployment created", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, "DeploymentCreated", "CreateDeployment",
			"Deployment %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Deployment updated", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, "DeploymentUpdated", "UpdateDeployment",
			"Deployment %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}

	existing := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		return nil, fmt.Errorf("failed to get Deployment after apply for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	return existing, nil
}

// generateDeployment creates the desired Deployment spec for the given MCPServer.
func generateDeployment(
	mcpServer *konnectv1alpha1.MCPServer,
	remoteMCPServer sdkkonnectcomp.MCPServerCPInfo,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) *appsv1.Deployment {
	nn := generateWorkloadNN(mcpServer)
	selectorLabels := map[string]string{
		"app": nn.Name,
	}
	podLabels := map[string]string{
		"app":                                    nn.Name,
		consts.GatewayOperatorManagedByLabel:     consts.MCPServerManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel: mcpServer.Name,
	}

	var replicas int32 = 1

	patEnvVar := patEnvVarFromAuth(apiAuth)

	const (
		mcpServerVolumeName      = "mcp-server-code"
		mcpServerVolumeMountPath = "/mcp-server"
	)

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Annotations: map[string]string{
				mcpServerVersionAnnotationKey: remoteMCPServer.Version,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 0,
					},
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 1,
					},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
					Annotations: map[string]string{
						mcpServerVersionAnnotationKey: remoteMCPServer.Version,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "init-mcp-server",
							Image:           *remoteMCPServer.InitContainer.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: []string{
								"-cp-url", apiAuth.Spec.ServerURL,
								"-cp-id", mcpServer.GetControlPlaneID(),
								"-mcp-server-id", mcpServer.GetKonnectID(),
								"-output-path", mcpServerVolumeMountPath + "/app.py",
								"-pat", "$(PAT)",
							},
							Env: []corev1.EnvVar{patEnvVar},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      mcpServerVolumeName,
									MountPath: mcpServerVolumeMountPath,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "mcp-server",
							Image:           *remoteMCPServer.Container.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									Name:          "mcp",
									ContainerPort: consts.MCPServerDefaultPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      mcpServerVolumeName,
									MountPath: mcpServerVolumeMountPath,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1000m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: mcpServerVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	k8sresources.LabelObjectAsMCPServerManaged(deployment)
	k8sutils.SetOwnerForObject(deployment, mcpServer)

	return deployment
}

// patEnvVarFromAuth builds a PAT environment variable from the given
// KonnectAPIAuthConfiguration. For token-type auth the token value is inlined;
// for secretRef-type auth the value is sourced from the referenced Secret.
func patEnvVarFromAuth(apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration) corev1.EnvVar {
	if apiAuth.Spec.Type == konnectv1alpha1.KonnectAPIAuthTypeSecretRef && apiAuth.Spec.SecretRef != nil {
		return corev1.EnvVar{
			Name: "PAT",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: apiAuth.Spec.SecretRef.Name,
					},
					Key: "token",
				},
			},
		}
	}
	return corev1.EnvVar{
		Name:  "PAT",
		Value: apiAuth.Spec.Token,
	}
}

// ----------------------------------------------------------------------------
// Service
// ----------------------------------------------------------------------------

// ensureService reconciles the Service for the given MCPServer using
// Server-Side Apply.
func (r *MCPServerReconciler) ensureService(
	ctx context.Context,
	logger logr.Logger,
	mcpServer *konnectv1alpha1.MCPServer,
) error {
	desired := generateService(mcpServer)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.typeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeWarning, "ServiceFailed", "ApplyService",
			"Failed to apply Service: %v", err)
		return fmt.Errorf("failed to apply Service for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Service created", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, "ServiceCreated", "CreateService",
			"Service %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Service updated", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpServer, nil, corev1.EventTypeNormal, "ServiceUpdated", "UpdateService",
			"Service %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// generateService creates the desired Service spec for the given MCPServer.
func generateService(mcpServer *konnectv1alpha1.MCPServer) *corev1.Service {
	nn := generateWorkloadNN(mcpServer)
	labels := map[string]string{
		"app": nn.Name,
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "mcp",
					Protocol:   corev1.ProtocolTCP,
					Port:       consts.MCPServerDefaultPort,
					TargetPort: intstr.FromInt32(consts.MCPServerDefaultPort),
				},
			},
		},
	}

	k8sresources.LabelObjectAsMCPServerManaged(svc)
	k8sutils.SetOwnerForObject(svc, mcpServer)

	return svc
}
