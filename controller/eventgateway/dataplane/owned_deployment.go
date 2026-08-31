/*
Copyright 2025 Kong, Inc.

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
	"strconv"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/reservedkeys"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// ensureDeployment reconciles the keg Deployment for the given DataPlane.
func (r *Reconciler) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	certSecretName string,
) error {
	image := resolveImage(egdp, consts.DefaultKEGImage)
	desired, err := buildDeployment(logger, r.TypeConverter, egdp, keg, image, certSecretName)
	if err != nil {
		return fmt.Errorf("failed to build Deployment for DataPlane %s/%s: %w",
			egdp.Namespace, egdp.Name, err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeWarning, "DeploymentFailed", "ApplyDeployment",
			"Failed to apply Deployment: %v", err)
		return fmt.Errorf("failed to apply Deployment for DataPlane %s/%s: %w",
			egdp.Namespace, egdp.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Deployment created", "name", desired.GetName())
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "DeploymentCreated", "CreateDeployment",
			"Deployment %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Deployment updated", "name", desired.GetName())
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "DeploymentUpdated", "UpdateDeployment",
			"Deployment %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// resolveImage determines the keg container image using the following priority:
//  1. User-specified image in spec.deployment.podTemplateSpec (container named "keg")
//  2. RELATED_IMAGE_KEG environment variable
//  3. defaultImage
func resolveImage(egdp *eventgatewayv1alpha1.KegDataPlane, defaultImage string) string {
	if egdp.Spec.Deployment != nil && egdp.Spec.Deployment.PodTemplateSpec != nil {
		if c := k8sutils.GetPodContainerByName(&egdp.Spec.Deployment.PodTemplateSpec.Spec, consts.KEGContainerName); c != nil && c.Image != "" {
			return c.Image
		}
	}
	if relatedImage := os.Getenv(consts.RelatedImageKEGEnvVar); relatedImage != "" {
		return relatedImage
	}
	return defaultImage
}

// buildDeployment constructs the desired keg Deployment as *unstructured.Unstructured.
// If the user has provided a PodTemplateSpec overlay in spec.deployment, it is
// merged with the operator base via SMD. The result always has spec.strategy
// removed so that SSA does not claim ownership of it, leaving the API server
// (or admission webhooks) free to apply their own default.
func buildDeployment(
	logger logr.Logger,
	tc managedfields.TypeConverter,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	image string,
	certSecretName string,
) (*unstructured.Unstructured, error) {
	base, err := generateBaseDeployment(logger, egdp, keg, image, certSecretName)
	if err != nil {
		return nil, err
	}

	var u *unstructured.Unstructured
	if egdp.Spec.Deployment == nil || egdp.Spec.Deployment.PodTemplateSpec == nil {
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
				Name:      egdp.Name,
				Namespace: egdp.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						consts.GatewayOperatorManagedByLabel:     consts.DataPlaneManagedByLabelValue,
						consts.GatewayOperatorManagedByNameLabel: egdp.Name,
					},
				},
				Template: *egdp.Spec.Deployment.PodTemplateSpec,
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

// generateBaseDeployment creates the operator-managed keg Deployment without user overlays.
func generateBaseDeployment(
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	image string,
	certSecretName string,
) (*appsv1.Deployment, error) {
	labels := map[string]string{
		"app.kubernetes.io/name":                      consts.KEGContainerName,
		consts.GatewayOperatorManagedByLabel:          consts.DataPlaneManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      egdp.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: egdp.Namespace,
	}
	selector := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.DataPlaneManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      egdp.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: egdp.Namespace,
	}

	envVars, err := buildKEGEnvVars(egdp, keg)
	if err != nil {
		return nil, err
	}

	healthPort := intstr.FromInt32(DefaultHealthPort)

	container := corev1.Container{
		Name:  consts.KEGContainerName,
		Image: image,
		Env:   envVars,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      KonnectCertVolumeName,
				MountPath: KonnectCertMountPath,
				ReadOnly:  true,
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health/probes/readiness",
					Port: healthPort,
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health/probes/liveness",
					Port: healthPort,
				},
			},
		},
	}
	container, volumes := resources.HardenContainerWithSecurityContext(container, resources.DataPlaneTypeKeg)

	volumes = append(
		volumes,
		corev1.Volume{
			Name: KonnectCertVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: certSecretName,
				},
			},
		},
	)

	var replicas *int32
	if egdp.Spec.Deployment != nil {
		var hs *eventgatewayv1alpha1.HorizontalScaling
		if egdp.Spec.Deployment.Scaling != nil {
			hs = egdp.Spec.Deployment.Scaling.HorizontalScaling
		}
		switch {
		case hs == nil:
			// No HPA: use the static replica count.
			replicas = egdp.Spec.Deployment.Replicas
		case hs.MinReplicas != nil:
			// HPA is active: seed replicas from minReplicas so the Deployment
			// scales up immediately before the HPA's first evaluation.
			replicas = hs.MinReplicas
		}
	}

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      egdp.Name,
			Namespace: egdp.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{Replicas: replicas,
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

	k8sutils.SetOwnerForObject(d, egdp)

	addAnnotationsForKegDataPlaneDeployment(logger, d, egdp)
	addLabelsForKegDataPlaneDeployment(logger, d, egdp)

	return d, nil
}

// kegDataPlaneDeploymentReservedKeys reports whether a label/annotation key is
// reserved for internal operator or Kubernetes use and must be dropped from any
// spec.deployment.labels/annotations provided by the user.
var kegDataPlaneDeploymentReservedKeys = reservedkeys.NewChecker("app.kubernetes.io/name", "deployment.kubernetes.io/revision")

// addAnnotationsForKegDataPlaneDeployment merges the user-provided
// spec.deployment.annotations (with reserved keys filtered out) into the
// Deployment's own metadata. It does not mutate the base annotations map so it
// is safe to call even when the Deployment shares maps with other objects
// (e.g. the Pod template).
func addAnnotationsForKegDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, egdp *eventgatewayv1alpha1.KegDataPlane) {
	if egdp.Spec.Deployment == nil || len(egdp.Spec.Deployment.Annotations) == 0 {
		return
	}
	specAnnotations := reservedkeys.Filter(logger, reservedkeys.MetadataTypeAnnotation, egdp.Spec.Deployment.Annotations, kegDataPlaneDeploymentReservedKeys)
	deployment.Annotations = reservedkeys.Merge(deployment.Annotations, specAnnotations)
}

// addLabelsForKegDataPlaneDeployment merges the user-provided
// spec.deployment.labels (with reserved keys filtered out) into the
// Deployment's own metadata. It does not mutate the base labels map, which is
// shared with the Pod template, so the Pod template's labels are unaffected.
func addLabelsForKegDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, egdp *eventgatewayv1alpha1.KegDataPlane) {
	if egdp.Spec.Deployment == nil || len(egdp.Spec.Deployment.Labels) == 0 {
		return
	}
	specLabels := reservedkeys.Filter(logger, reservedkeys.MetadataTypeLabel, egdp.Spec.Deployment.Labels, kegDataPlaneDeploymentReservedKeys)
	deployment.Labels = reservedkeys.Merge(deployment.Labels, specLabels)
}

// buildKEGEnvVars builds the full list of keg environment variables from
// KonnectEventGateway status and DataPlane spec.config.
func buildKEGEnvVars(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
) ([]corev1.EnvVar, error) {
	srv, err := server.NewServer[konnectv1alpha1.KonnectEventGateway](keg.Status.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Konnect server URL %q: %w", keg.Status.ServerURL, err)
	}
	region := srv.Region().String()
	domain := srv.Domain()
	healthAddr := fmt.Sprintf("0.0.0.0:%d", DefaultHealthPort)

	cfg := egdp.Spec.Config
	if cfg != nil {
		if cfg.Konnect != nil && cfg.Konnect.Domain != nil {
			domain = *cfg.Konnect.Domain
		}
		if cfg.Runtime != nil && cfg.Runtime.HealthListenerAddressPort != nil {
			healthAddr = *cfg.Runtime.HealthListenerAddressPort
		}
	}

	envVars := []corev1.EnvVar{
		{Name: EnvKonnectRegion, Value: region},
		{Name: EnvKonnectGatewayClusterID, Value: keg.Status.ID},
		{Name: EnvKonnectClientCertPath, Value: KonnectCertMountPath + "tls.crt"},
		{Name: EnvKonnectClientKeyPath, Value: KonnectCertMountPath + "tls.key"},
		{Name: EnvKonnectDomain, Value: domain},
		// Bind the health endpoint to all interfaces so Kubernetes probes can reach it.
		{Name: EnvRuntimeHealthAddr, Value: healthAddr},
	}

	if cfg == nil {
		return envVars, nil
	}

	if cfg.Konnect != nil {
		if cfg.Konnect.APIRequestTimeoutSeconds != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvKonnectAPIRequestTimeout, Value: fmt.Sprintf("%ds", *cfg.Konnect.APIRequestTimeoutSeconds)})
		}
		if cfg.Konnect.InsecureSkipVerify != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvKonnectInsecureSkipVerify, Value: strconv.FormatBool(*cfg.Konnect.InsecureSkipVerify == eventgatewayv1alpha1.TLSVerificationStateEnabled)})
		}
	}

	if cfg.ConfigPollIntervalSeconds != nil {
		envVars = append(envVars, corev1.EnvVar{Name: EnvConfigPollInterval, Value: fmt.Sprintf("%ds", *cfg.ConfigPollIntervalSeconds)})
	}

	if cfg.EnableDebugEndpoints != nil {
		envVars = append(envVars, corev1.EnvVar{Name: EnvEnableDebugEndpoints, Value: strconv.FormatBool(*cfg.EnableDebugEndpoints == eventgatewayv1alpha1.DebugEndpointsStateEnabled)})
	}

	if obs := cfg.Observability; obs != nil { //nolint:gocritic
		if obs.LogFlags != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvObsLogFlags, Value: *obs.LogFlags})
		}
		if obs.LogFormat != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvObsLogFormat, Value: *obs.LogFormat})
		}
		if obs.MetricsRollupAllowMap != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvObsMetricsRollupAllowMap, Value: *obs.MetricsRollupAllowMap})
		}
		if obs.PolicyErrorsInfoLogIntervalSeconds != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvObsPolicyErrorsInfoLogInterval, Value: fmt.Sprintf("%ds", *obs.PolicyErrorsInfoLogIntervalSeconds)})
		}
	}

	if rt := cfg.Runtime; rt != nil {
		if rt.DrainDurationSeconds != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvRuntimeDrainDuration, Value: fmt.Sprintf("%ds", *rt.DrainDurationSeconds)})
		}
		if rt.ShutdownTimeoutSeconds != nil {
			envVars = append(envVars, corev1.EnvVar{Name: EnvRuntimeShutdownTimeout, Value: fmt.Sprintf("%ds", *rt.ShutdownTimeoutSeconds)})
		}
	}

	return envVars, nil
}
