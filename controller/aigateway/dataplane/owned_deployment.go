/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// ensureDeployment reconciles the AI Gateway Deployment for the given AIGatewayDataPlane.
func (r *Reconciler) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
	certSecretName string,
) error {
	image := resolveImage(aigwdp, consts.DefaultAIGatewayDataPlaneImage)
	desired, err := buildDeployment(r.TypeConverter, aigwdp, aigatewaycp, image, certSecretName)
	if err != nil {
		return fmt.Errorf("failed to build Deployment for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeWarning, "DeploymentFailed", "ApplyDeployment",
			"Failed to apply Deployment: %v", err)
		return fmt.Errorf("failed to apply Deployment for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Deployment created", "name", desired.GetName())
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "DeploymentCreated", "CreateDeployment",
			"Deployment %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Deployment updated", "name", desired.GetName())
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "DeploymentUpdated", "UpdateDeployment",
			"Deployment %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// resolveImage determines the AI Gateway container image using the following priority:
//  1. User-specified image in spec.deployment.podTemplateSpec (container named "aigw")
//  2. RELATED_IMAGE_AIGW environment variable
//  3. defaultImage
func resolveImage(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, defaultImage string) string {
	if aigwdp.Spec.Deployment != nil && aigwdp.Spec.Deployment.PodTemplateSpec != nil {
		if c := k8sutils.GetPodContainerByName(&aigwdp.Spec.Deployment.PodTemplateSpec.Spec, consts.AIGatewayDataPlaneContainerName); c != nil && c.Image != "" {
			return c.Image
		}
	}
	if relatedImage := os.Getenv(consts.RelatedImageAIGatewayDataPlaneEnvVar); relatedImage != "" {
		return relatedImage
	}
	return defaultImage
}

// buildDeployment constructs the desired AI Gateway Deployment as *unstructured.Unstructured.
// If the user has provided a PodTemplateSpec overlay in spec.deployment, it is
// merged with the operator base via SMD. The result always has spec.strategy
// removed so that SSA does not claim ownership of it, leaving the API server
// (or admission webhooks) free to apply their own default.
func buildDeployment(
	tc managedfields.TypeConverter,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
	image string,
	certSecretName string,
) (*unstructured.Unstructured, error) {
	base, err := generateBaseDeployment(aigwdp, aigatewaycp, image, certSecretName)
	if err != nil {
		return nil, err
	}

	var u *unstructured.Unstructured
	if aigwdp.Spec.Deployment == nil || aigwdp.Spec.Deployment.PodTemplateSpec == nil {
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(base)
		if err != nil {
			return nil, fmt.Errorf("failed to convert Deployment to unstructured: %w", err)
		}
		u = &unstructured.Unstructured{Object: raw}
	} else {
		// Wrap the user PodTemplateSpec overlay into a Deployment skeleton.
		userDeployment := &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      aigwdp.Name,
				Namespace: aigwdp.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						consts.GatewayOperatorManagedByLabel:     consts.AIGatewayDataPlaneManagedByLabelValue,
						consts.GatewayOperatorManagedByNameLabel: aigwdp.Name,
					},
				},
				Template: *aigwdp.Spec.Deployment.PodTemplateSpec,
			},
		}

		u, err = controllerpkgssa.MergeObjects(tc, base, userDeployment)
		if err != nil {
			return nil, err
		}
	}

	// Remove spec.strategy so we don't claim SSA ownership of it.
	// The zero-value DeploymentStrategy{} serializes to "strategy: {}" which
	// diverges from the server-defaulted rolling-update value every reconcile.
	unstructured.RemoveNestedField(u.Object, "spec", "strategy")
	return u, nil
}

// generateBaseDeployment creates the operator-managed AI Gateway Deployment without user overlays.
func generateBaseDeployment(
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
	image string,
	certSecretName string,
) (*appsv1.Deployment, error) {
	labels := map[string]string{
		"app.kubernetes.io/name":                      consts.AIGatewayDataPlaneContainerName,
		consts.GatewayOperatorManagedByLabel:          consts.AIGatewayDataPlaneManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      aigwdp.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: aigwdp.Namespace,
	}
	selector := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.AIGatewayDataPlaneManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      aigwdp.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: aigwdp.Namespace,
	}

	envVars, err := buildAIGatewayEnvVars(aigatewaycp)
	if err != nil {
		return nil, err
	}

	const tmpVolumeName = "tmp"

	container := corev1.Container{
		Name:  consts.AIGatewayDataPlaneContainerName,
		Image: image,
		Env:   envVars,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"NET_RAW"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      KonnectCertVolumeName,
				MountPath: KonnectCertMountPath,
				ReadOnly:  true,
			},
			{
				Name:      tmpVolumeName,
				MountPath: "/tmp",
			},
		},
		ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
	}

	tmpSizeLimit := resource.MustParse("1Gi")
	volumes := []corev1.Volume{
		{
			Name: KonnectCertVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: certSecretName,
				},
			},
		},
		{
			Name: tmpVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					SizeLimit: &tmpSizeLimit,
				},
			},
		},
	}

	var replicas *int32
	if aigwdp.Spec.Deployment != nil {
		replicas = aigwdp.Spec.Deployment.Replicas
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigwdp.Name,
			Namespace: aigwdp.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes:    volumes,
				},
			},
		},
	}

	k8sutils.SetOwnerForObject(d, aigwdp)
	k8sresources.LabelObjectAsAIGatewayDataPlaneManaged(d)
	k8sresources.LabelObjectAsAIGatewayDataPlaneManaged(&d.Spec.Template)
	return d, nil
}

// buildAIGatewayEnvVars builds the AI Gateway environment variables
// from required hardcoded values and KonnectAIGateway (controlplane) status.
func buildAIGatewayEnvVars(
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
) ([]corev1.EnvVar, error) {
	if aigatewaycp.Status.Endpoints == nil {
		return nil, fmt.Errorf("KonnectAIGateway %q has no endpoints in status", aigatewaycp.Name)
	}

	cpHost := aigatewaycp.Status.Endpoints.Configuration
	tpHost := aigatewaycp.Status.Endpoints.Telemetry

	return append(
		RequiredHardcodedEnvVars(),
		corev1.EnvVar{Name: EnvKongClusterControlPlane, Value: cpHost + ":443"},
		corev1.EnvVar{Name: EnvKongClusterServerName, Value: cpHost},
		corev1.EnvVar{Name: EnvKongClusterTelemetryEndpoint, Value: tpHost + ":443"},
		corev1.EnvVar{Name: EnvKongClusterTelemetryServerName, Value: tpHost},
		corev1.EnvVar{Name: EnvClientCertPath, Value: KonnectCertMountPath + "tls.crt"},
		corev1.EnvVar{Name: EnvKonnectClientCertKey, Value: KonnectCertMountPath + "tls.key"},
	), nil
}
