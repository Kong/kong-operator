package mcpserver

import (
	"context"
	"fmt"
	"maps"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	konnectcontroller "github.com/kong/kong-operator/v2/controller/konnect"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/reservedkeys"
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
func generateWorkloadNN(mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) types.NamespacedName {
	nn := generateHashedName(mcpDataPlane.Namespace, "mcpserver", mcpDataPlane.Name)
	return types.NamespacedName{
		Namespace: nn.Namespace,
		Name:      nn.Name, // bounded to <=63 chars
	}
}

type mcpServerMetadata struct {
	ID                 string
	ContainerImage     string
	InitContainerImage string
	Version            string
	ControlPlaneID     string
	MCPServerID        string
}

// derefImage returns the container's image, or "" if the container spec or
// its image is nil.
func derefImage(c *sdkkonnectcomp.ContainerSpec) string {
	if c == nil || c.Image == nil {
		return ""
	}
	return *c.Image
}

func (r *MCPServerDataPlaneReconciler) ensureTokenSecret(
	ctx context.Context,
	logger logr.Logger,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (*corev1.Secret, error) {
	var (
		secret = corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			Type: corev1.SecretTypeOpaque,
		}
	)

	switch apiAuth.Spec.Type {
	case konnectv1alpha1.KonnectAPIAuthTypeToken:
		nn := generateWorkloadNN(mcpDataPlane)
		secret.ObjectMeta = metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		}
		if r.SecretLabelSelector != "" {
			secret.Labels = map[string]string{
				r.SecretLabelSelector: "true",
			}
		}
		secret.Data = map[string][]byte{
			konnectcontroller.SecretTokenKey: []byte(apiAuth.Spec.Token),
		}
		k8sresources.LabelObjectAsMCPServerManaged(&secret)
		k8sutils.SetOwnerForObject(&secret, mcpDataPlane)

		result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, &secret, controllerpkgssa.FieldManager)
		if err != nil {
			r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeWarning, "TokenSecretFailed", "ApplyTokenSecret",
				"Failed to apply Token Secret: %v", err)
			return nil, fmt.Errorf("failed to apply Token Secret for MCPServer %s/%s: %w",
				mcpDataPlane.Namespace, mcpDataPlane.Name, err)
		}
		switch result {
		case op.Created:
			log.Debug(logger, "Token Secret created", "name", secret.GetName())
			r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "TokenSecretCreated", "CreateTokenSecret",
				"Token Secret %s created", secret.GetName())
		case op.Updated:
			log.Debug(logger, "Token Secret updated", "name", secret.GetName())
			r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "TokenSecretUpdated", "UpdateTokenSecret",
				"Token Secret %s updated", secret.GetName())
		case op.Noop, op.Deleted:
		}

		// Only the name is used downstream (patEnvVarFromAuth), and it's
		// already known from generateWorkloadNN. Avoid a Get through the
		// (possibly lagging) manager cache right after the apply.
		return &secret, nil

	case konnectv1alpha1.KonnectAPIAuthTypeSecretRef:
		if apiAuth.Spec.SecretRef == nil {
			return nil, fmt.Errorf("KonnectAPIAuthConfiguration %s/%s has auth type secretRef but no spec.secretRef",
				apiAuth.Namespace, apiAuth.Name)
		}
		secretRef := apiAuth.Spec.SecretRef.DeepCopy()
		ns := secretRef.Namespace
		if ns == "" {
			// default to the same namespace as the KonnectAPIAuthConfiguration
			ns = apiAuth.Namespace
		}
		if ns != mcpDataPlane.Namespace {
			// A SecretKeyRef only resolves inside the Pod's own namespace, so
			// a cross-namespace secretRef would leave the Deployment mounting
			// a Secret that doesn't exist there.
			return nil, fmt.Errorf(
				"KonnectAPIAuthConfiguration %s/%s references Secret %s/%s in a different namespace than MCPServerDataPlane %s/%s: a Pod can only mount Secrets from its own namespace",
				apiAuth.Namespace, apiAuth.Name, ns, secretRef.Name,
				mcpDataPlane.Namespace, mcpDataPlane.Name,
			)
		}
		secret.ObjectMeta = metav1.ObjectMeta{
			Name:      secretRef.Name,
			Namespace: ns,
		}

		// If auth switched from token to secretRef, delete the Secret this
		// controller generated earlier so the plaintext PAT doesn't linger.
		if generatedNN := generateWorkloadNN(mcpDataPlane); generatedNN.Name != secretRef.Name {
			var stale corev1.Secret
			switch err := r.Get(ctx, generatedNN, &stale); {
			case err == nil:
				if err := r.Delete(ctx, &stale); err != nil && !apierrors.IsNotFound(err) {
					return nil, fmt.Errorf("failed to delete stale Token Secret %s for MCPServer %s/%s: %w",
						generatedNN, mcpDataPlane.Namespace, mcpDataPlane.Name, err)
				}
			case !apierrors.IsNotFound(err):
				return nil, fmt.Errorf("failed to get stale Token Secret %s for MCPServer %s/%s: %w",
					generatedNN, mcpDataPlane.Namespace, mcpDataPlane.Name, err)
			}
		}
		return &secret, nil
	default:
		return nil, fmt.Errorf("unsupported KonnectAPIAuthConfiguration type: %s", apiAuth.Spec.Type)
	}
}

// ----------------------------------------------------------------------------
// Deployment
// ----------------------------------------------------------------------------

// ensureDeployment reconciles the Deployment for the given MCPServer using
// Server-Side Apply. It returns the live Deployment after the apply so callers
// can derive ReplicaSet/Pod version statuses from it.
func (r *MCPServerDataPlaneReconciler) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	mcpMetadata mcpServerMetadata,
	apiURL string,
	tokenSecret *corev1.Secret,
) (*appsv1.Deployment, error) {
	if mcpMetadata.InitContainerImage == "" {
		return nil, fmt.Errorf("remote MCPServer %s is missing init container info", mcpMetadata.ID)
	}
	if mcpMetadata.ContainerImage == "" {
		return nil, fmt.Errorf("remote MCPServer %s is missing container info", mcpMetadata.ID)
	}

	desired := generateDeployment(logger, mcpDataPlane, mcpMetadata, tokenSecret, apiURL)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeWarning, "DeploymentFailed", "ApplyDeployment",
			"Failed to apply Deployment: %v", err)
		return nil, fmt.Errorf("failed to apply Deployment for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Deployment created", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "DeploymentCreated", "CreateDeployment",
			"Deployment %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Deployment updated", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "DeploymentUpdated", "UpdateDeployment",
			"Deployment %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}

	existing := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), existing); err != nil {
		return nil, fmt.Errorf("failed to get Deployment after apply for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}
	return existing, nil
}

// generateDeployment creates the desired Deployment spec for the given MCPServer.
func generateDeployment(
	logger logr.Logger,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	mcpMetadata mcpServerMetadata,
	tokenSecret *corev1.Secret,
	apiURL string,
) *appsv1.Deployment {
	nn := generateWorkloadNN(mcpDataPlane)
	selectorLabels := map[string]string{
		"app": nn.Name,
	}
	podLabels := map[string]string{
		"app":                                    nn.Name,
		consts.GatewayOperatorManagedByLabel:     consts.MCPServerManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel: mcpDataPlane.Name,
	}

	var replicas int32 = 1
	if deploy := mcpDataPlane.Spec.Deployment; deploy != nil {
		if deploy.Replicas != nil {
			replicas = *deploy.Replicas
		}

		// TODO: handle other scaling types (e.g. HPA) in the future when API adds them.
	}

	patEnvVar := patEnvVarFromAuth(tokenSecret)

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
				mcpServerVersionAnnotationKey: mcpMetadata.Version,
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
						mcpServerVersionAnnotationKey: mcpMetadata.Version,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            consts.MCPServerDataPlaneInitContainerName,
							Image:           mcpMetadata.InitContainerImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args: []string{
								"-cp-url", apiURL,
								"-cp-id", mcpMetadata.ControlPlaneID,
								"-mcp-server-id", mcpMetadata.MCPServerID,
								"-output-path", mcpServerVolumeMountPath + "/app.py",
								"-pat", "$(PAT)",
							},
							Env: []corev1.EnvVar{
								patEnvVar,
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
							Name:            consts.MCPServerDataPlaneContainerName,
							Image:           mcpMetadata.ContainerImage,
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
	k8sutils.SetOwnerForObject(deployment, mcpDataPlane)

	addAnnotationsForMCPServerDataPlaneDeployment(logger, deployment, mcpDataPlane)
	addLabelsForMCPServerDataPlaneDeployment(logger, deployment, mcpDataPlane)
	addPodTemplateMetadataForMCPServerDataPlane(logger, deployment, mcpDataPlane)
	addContainerResourcesForMCPServerDataPlane(deployment, mcpDataPlane)

	return deployment
}

// addContainerResourcesForMCPServerDataPlane overrides the Resources of
// containers (init or regular) in the generated Deployment's Pod template with
// the ones provided via spec.deployment.podTemplateSpec.spec.containers,
// matched by container name. Overrides for names that don't match any
// container the operator manages are ignored.
func addContainerResourcesForMCPServerDataPlane(
	deployment *appsv1.Deployment,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
) {
	if mcpDataPlane.Spec.Deployment == nil {
		return
	}

	podSpec := &deployment.Spec.Template.Spec
	for _, override := range mcpDataPlane.Spec.Deployment.PodTemplateSpec.Spec.Containers {
		if container := k8sutils.GetPodContainerByName(podSpec, override.Name); container != nil {
			maps.Copy(container.Resources.Requests, override.Resources.Requests)
			maps.Copy(container.Resources.Limits, override.Resources.Limits)
			continue
		}
		if initContainer := k8sutils.GetInitContainerByName(podSpec, override.Name); initContainer != nil {
			maps.Copy(initContainer.Resources.Requests, override.Resources.Requests)
			maps.Copy(initContainer.Resources.Limits, override.Resources.Limits)
		}
	}
}

// mcpServerDataPlaneDeploymentReservedKeys reports whether a label/annotation key is
// reserved for internal operator or Kubernetes use and must be dropped from any
// spec.deployment.labels/annotations or spec.deployment.podTemplateSpec.metadata
// labels/annotations provided by the user.
var mcpServerDataPlaneDeploymentReservedKeys = reservedkeys.NewChecker(
	"app",
	"pod-template-hash",
	"deployment.kubernetes.io/revision",
	mcpServerVersionAnnotationKey,
)

// addAnnotationsForMCPServerDataPlaneDeployment merges the user-provided
// spec.deployment.annotations (with reserved keys filtered out) into the
// Deployment's own metadata. It does not mutate the base annotations map so it
// is safe to call even when the Deployment shares maps with other objects
// (e.g. the Pod template).
func addAnnotationsForMCPServerDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) {
	if mcpDataPlane.Spec.Deployment == nil || len(mcpDataPlane.Spec.Deployment.Annotations) == 0 {
		return
	}
	specAnnotations := reservedkeys.Filter(logger, reservedkeys.MetadataTypeAnnotation, mcpDataPlane.Spec.Deployment.Annotations, mcpServerDataPlaneDeploymentReservedKeys)
	deployment.Annotations = reservedkeys.Merge(deployment.Annotations, specAnnotations)
}

// addLabelsForMCPServerDataPlaneDeployment merges the user-provided
// spec.deployment.labels (with reserved keys filtered out) into the
// Deployment's own metadata. It does not mutate the base labels map, which is
// shared with the Pod template, so the Pod template's labels are unaffected.
func addLabelsForMCPServerDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) {
	if mcpDataPlane.Spec.Deployment == nil || len(mcpDataPlane.Spec.Deployment.Labels) == 0 {
		return
	}
	specLabels := reservedkeys.Filter(logger, reservedkeys.MetadataTypeLabel, mcpDataPlane.Spec.Deployment.Labels, mcpServerDataPlaneDeploymentReservedKeys)
	deployment.Labels = reservedkeys.Merge(deployment.Labels, specLabels)
}

// addPodTemplateMetadataForMCPServerDataPlane merges the user-provided
// spec.deployment.podTemplateSpec.metadata labels/annotations (with reserved
// keys filtered out) into the Deployment's Pod template. reservedkeys.Merge
// never mutates the base maps, so the Deployment's own labels/annotations and
// the selector labels are unaffected.
func addPodTemplateMetadataForMCPServerDataPlane(logger logr.Logger, deployment *appsv1.Deployment, mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) {
	if mcpDataPlane.Spec.Deployment == nil {
		return
	}
	meta := mcpDataPlane.Spec.Deployment.PodTemplateSpec.Metadata
	podMeta := &deployment.Spec.Template.ObjectMeta
	specLabels := reservedkeys.Filter(logger, reservedkeys.MetadataTypeLabel, meta.Labels, mcpServerDataPlaneDeploymentReservedKeys)
	podMeta.Labels = reservedkeys.Merge(podMeta.Labels, specLabels)
	specAnnotations := reservedkeys.Filter(logger, reservedkeys.MetadataTypeAnnotation, meta.Annotations, mcpServerDataPlaneDeploymentReservedKeys)
	podMeta.Annotations = reservedkeys.Merge(podMeta.Annotations, specAnnotations)
}

// patEnvVarFromAuth builds a PAT environment variable sourced from the given
// token Secret, as returned by ensureTokenSecret for both the token and
// secretRef KonnectAPIAuthConfiguration types.
func patEnvVarFromAuth(tokenSecret *corev1.Secret) corev1.EnvVar {
	return corev1.EnvVar{
		Name: "PAT",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: tokenSecret.Name,
				},
				Key: konnectcontroller.SecretTokenKey,
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Service
// ----------------------------------------------------------------------------

// ensureService reconciles the Service for the given MCPServer using
// Server-Side Apply.
func (r *MCPServerDataPlaneReconciler) ensureService(
	ctx context.Context,
	logger logr.Logger,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
) error {
	desired := generateService(mcpDataPlane)

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeWarning, "ServiceFailed", "ApplyService",
			"Failed to apply Service: %v", err)
		return fmt.Errorf("failed to apply Service for MCPServer %s/%s: %w",
			mcpDataPlane.Namespace, mcpDataPlane.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Service created", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "ServiceCreated", "CreateService",
			"Service %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Service updated", "name", desired.GetName())
		r.eventRecorder.Eventf(mcpDataPlane, nil, corev1.EventTypeNormal, "ServiceUpdated", "UpdateService",
			"Service %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// generateService creates the desired Service spec for the given MCPServer.
func generateService(mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) *corev1.Service {
	nn := generateWorkloadNN(mcpDataPlane)
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
	k8sutils.SetOwnerForObject(svc, mcpDataPlane)

	return svc
}
