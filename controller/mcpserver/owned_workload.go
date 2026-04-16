package mcpserver

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
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

// ensureDeployment ensures that exactly one Deployment exists for the given
// MCPServer. It creates a new Deployment if none exists, updates it if needed,
// and returns the operation result.
func (r *MCPServerReconciler) ensureDeployment(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	remoteMCPServer *sdkkonnectcomp.MCPServerCPInfo,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (op.Result, *appsv1.Deployment, error) {
	desired := generateDeployment(mcpServer, *remoteMCPServer, apiAuth)

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: desired.Namespace,
		Name:      desired.Name,
	}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nil, fmt.Errorf("failed to get Deployment for MCPServer %s/%s: %w",
				mcpServer.Namespace, mcpServer.Name, err)
		}

		k8sutils.SetOwnerForObject(desired, mcpServer)
		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nil, fmt.Errorf("failed to create Deployment for MCPServer %s/%s: %w",
				mcpServer.Namespace, mcpServer.Name, err)
		}
		return op.Created, desired, nil
	}

	// Ensure the version annotation is up to date on both the Deployment and
	// its pod template.
	// TODO: ensure the whole deployment spec is up to date and enforced.
	desiredVersion := desired.Annotations[mcpServerVersionAnnotationKey]

	if existing.Annotations == nil {
		existing.Annotations = make(map[string]string)
	}
	if existing.Spec.Template.Annotations == nil {
		existing.Spec.Template.Annotations = make(map[string]string)
	}

	deployNeedsUpdate := existing.Annotations[mcpServerVersionAnnotationKey] != desiredVersion
	templateNeedsUpdate := existing.Spec.Template.Annotations[mcpServerVersionAnnotationKey] != desiredVersion

	if deployNeedsUpdate || templateNeedsUpdate {
		old := existing.DeepCopy()
		existing.Annotations[mcpServerVersionAnnotationKey] = desiredVersion
		existing.Spec.Template.Annotations[mcpServerVersionAnnotationKey] = desiredVersion
		if err := r.Patch(ctx, existing, client.MergeFrom(old)); err != nil {
			return op.Noop, nil, fmt.Errorf("failed to patch Deployment %s/%s version annotation: %w",
				existing.Namespace, existing.Name, err)
		}
		return op.Updated, existing, nil
	}

	return op.Noop, existing, nil
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

	const mcpServerVolumeName = "mcp-server-code"

	deployment := &appsv1.Deployment{
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
							Image:           "kong/mcp-server-init",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: []string{
								"-cp-url", apiAuth.Spec.ServerURL,
								"-cp-id", mcpServer.GetControlPlaneID(),
								"-mcp-server-id", mcpServer.GetKonnectID(),
								"-output-path", "/mcp-server/app.py",
								"-pat", "$(PAT)",
							},
							Env: []corev1.EnvVar{patEnvVar},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      mcpServerVolumeName,
									MountPath: "/mcp-server",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "mcp-server",
							Image:           "kong/mcp-server-runner",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "MCP_SERVER_PATH",
									Value: "/mcp-server/app.py",
								},
							},
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
									MountPath: "/mcp-server",
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

// ensureService ensures that exactly one Service exists for the given
// MCPServer. It creates a new Service if none exists and returns the
// operation result.
func (r *MCPServerReconciler) ensureService(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
) (op.Result, *corev1.Service, error) {
	desired := generateService(mcpServer)

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: desired.Namespace,
		Name:      desired.Name,
	}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nil, fmt.Errorf("failed to get Service for MCPServer %s/%s: %w",
				mcpServer.Namespace, mcpServer.Name, err)
		}

		k8sutils.SetOwnerForObject(desired, mcpServer)
		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nil, fmt.Errorf("failed to create Service for MCPServer %s/%s: %w",
				mcpServer.Namespace, mcpServer.Name, err)
		}
		return op.Created, desired, nil
	}

	// TODO: if the service already exists, we should check if its spec is up to date and update it if needed.

	return op.Noop, existing, nil
}

// generateService creates the desired Service spec for the given MCPServer.
func generateService(mcpServer *konnectv1alpha1.MCPServer) *corev1.Service {
	nn := generateWorkloadNN(mcpServer)
	labels := map[string]string{
		"app": nn.Name,
	}

	svc := &corev1.Service{
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

	return svc
}
