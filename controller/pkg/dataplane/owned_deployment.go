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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/reservedkeys"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureDeployment reconciles the DataPlane Deployment for the given DataPlane.
func (r *Reconciler[T, CP, Cert]) ensureDeployment(
	ctx context.Context,
	logger logr.Logger,
	dp T,
	cp CP,
	certSecretName string,
) error {
	image := ResolveImage(dp, r.Config.Deployment)
	desired, err := BuildDeployment(logger, r.TypeConverter, dp, cp, image, certSecretName, r.Config)
	if err != nil {
		return fmt.Errorf("failed to build Deployment for %s %s/%s: %w",
			r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeWarning, "DeploymentFailed", "ApplyDeployment",
			"Failed to apply Deployment: %v", err)
		return fmt.Errorf("failed to apply Deployment for %s %s/%s: %w",
			r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Deployment created", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "DeploymentCreated", "CreateDeployment",
			"Deployment %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Deployment updated", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "DeploymentUpdated", "UpdateDeployment",
			"Deployment %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// ResolveImage determines the DataPlane container image using the following priority:
//  1. User-specified image in the pod template overlay (the DataPlane container)
//  2. The related-image environment variable
//  3. The configured default image
func ResolveImage[T Object, CP ControlPlaneObject](dp T, cfg DeploymentConfig[T, CP]) string {
	if pts := cfg.PodTemplateSpec(dp); pts != nil {
		if c := k8sutils.GetPodContainerByName(&pts.Spec, cfg.ContainerName); c != nil && c.Image != "" {
			return c.Image
		}
	}
	if relatedImage := os.Getenv(cfg.RelatedImageEnvVar); relatedImage != "" {
		return relatedImage
	}
	return cfg.DefaultImage
}

// BuildDeployment constructs the desired DataPlane Deployment as
// *unstructured.Unstructured. If the user has provided a PodTemplateSpec
// overlay, it is merged with the operator base via SMD. The result always has
// spec.strategy removed so that SSA does not claim ownership of it, leaving
// the API server (or admission webhooks) free to apply their own default.
func BuildDeployment[T Object, CP ControlPlaneObject, Cert CertificateObject](
	logger logr.Logger,
	tc managedfields.TypeConverter,
	dp T,
	cp CP,
	image string,
	certSecretName string,
	cfg Config[T, CP, Cert],
) (*unstructured.Unstructured, error) {
	base, err := GenerateBaseDeployment(logger, dp, cp, image, certSecretName, cfg)
	if err != nil {
		return nil, err
	}

	var u *unstructured.Unstructured
	if pts := cfg.Deployment.PodTemplateSpec(dp); pts == nil {
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
				Name:      dp.GetName(),
				Namespace: dp.GetNamespace(),
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: SelectorLabels(dp, cfg.Deployment.ManagedByLabelValue),
				},
				Template: *pts,
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

// SelectorLabels returns the labels uniquely identifying the pods of the
// given DataPlane.
func SelectorLabels[T Object](dp T, managedByLabelValue string) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          managedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      dp.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: dp.GetNamespace(),
	}
}

// GenerateBaseDeployment creates the operator-managed DataPlane Deployment
// without user overlays.
func GenerateBaseDeployment[T Object, CP ControlPlaneObject, Cert CertificateObject](
	logger logr.Logger,
	dp T,
	cp CP,
	image string,
	certSecretName string,
	cfg Config[T, CP, Cert],
) (*appsv1.Deployment, error) {
	labels := SelectorLabels(dp, cfg.Deployment.ManagedByLabelValue)
	labels["app.kubernetes.io/name"] = cfg.Deployment.ContainerName

	selector := SelectorLabels(dp, cfg.Deployment.ManagedByLabelValue)

	container, volumes, err := cfg.Deployment.BuildContainer(dp, cp, image)
	if err != nil {
		return nil, err
	}

	volumes = append(
		volumes,
		corev1.Volume{
			Name: KonnectCertVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: certSecretName,
				},
			},
		})

	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dp.GetName(),
			Namespace: dp.GetNamespace(),
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cfg.Deployment.Replicas(dp),
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

	k8sutils.SetOwnerForObject(d, dp)
	if cfg.Deployment.LabelManaged != nil {
		cfg.Deployment.LabelManaged(d)
		cfg.Deployment.LabelManaged(&d.Spec.Template)
	}

	addAnnotationsForDataPlaneDeployment(logger, d, cfg.Deployment.DeploymentAnnotations(dp))
	addLabelsForDataPlaneDeployment(logger, d, cfg.Deployment.DeploymentLabels(dp))

	return d, nil
}

// dataPlaneDeploymentReservedKeys reports whether a label/annotation key is
// reserved for internal operator or Kubernetes use and must be dropped from
// any user-provided Deployment labels/annotations.
var dataPlaneDeploymentReservedKeys = reservedkeys.NewChecker("app.kubernetes.io/name", "deployment.kubernetes.io/revision")

// addAnnotationsForDataPlaneDeployment merges the user-provided Deployment
// annotations (with reserved keys filtered out) into the Deployment's own
// metadata. It does not mutate the base annotations map so it is safe to call
// even when the Deployment shares maps with other objects (e.g. the Pod
// template).
func addAnnotationsForDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}
	specAnnotations := reservedkeys.Filter(logger, reservedkeys.MetadataTypeAnnotation, annotations, dataPlaneDeploymentReservedKeys)
	deployment.Annotations = reservedkeys.Merge(deployment.Annotations, specAnnotations)
}

// addLabelsForDataPlaneDeployment merges the user-provided Deployment labels
// (with reserved keys filtered out) into the Deployment's own metadata. It
// does not mutate the base labels map, which is shared with the Pod template,
// so the Pod template's labels are unaffected.
func addLabelsForDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	specLabels := reservedkeys.Filter(logger, reservedkeys.MetadataTypeLabel, labels, dataPlaneDeploymentReservedKeys)
	deployment.Labels = reservedkeys.Merge(deployment.Labels, specLabels)
}
