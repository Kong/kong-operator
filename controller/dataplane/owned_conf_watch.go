package dataplane

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/config"
	"github.com/kong/kong-operator/v2/internal/versions"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// listKongConfWatchConfigMapsForDataPlane lists the ConfigMaps holding the
// watched kong.conf owned by the given DataPlane.
func listKongConfWatchConfigMapsForDataPlane(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
) ([]corev1.ConfigMap, error) {
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	matchingLabels[consts.DataPlaneConfigMapTypeLabel] = consts.DataPlaneConfigMapTypeKongConfWatch

	return k8sutils.ListConfigMapsForOwner(
		ctx,
		cl,
		dataplane.UID,
		client.InNamespace(dataplane.Namespace),
		matchingLabels,
	)
}

// ensureKongConfWatchConfigMapForDataPlane ensures the ConfigMap holding the
// kong.conf watched by the DataPlane's Kong Gateway container exists and holds
// the given contents.
//
// The ConfigMap is updated in place: its name is stable, and only its contents
// change. That is what keeps a listener change out of the Pod template and so
// avoids a rollout.
func ensureKongConfWatchConfigMapForDataPlane(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	contents string,
) (op.Result, *corev1.ConfigMap, error) {
	dpNn := client.ObjectKeyFromObject(dataplane)

	cms, err := listKongConfWatchConfigMapsForDataPlane(ctx, cl, dataplane)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing kong.conf ConfigMaps for DataPlane %s: %w", dpNn, err)
	}

	generated := k8sresources.GenerateKongConfWatchConfigMapForDataPlane(dataplane, contents)

	// More than one is only possible if something outside the operator created
	// one. Delete everything that does not carry the name we generate, so the
	// Pod template keeps pointing at a ConfigMap that exists.
	var existing *corev1.ConfigMap
	for i := range cms {
		cm := &cms[i]
		if cm.Name == generated.Name {
			existing = cm
			continue
		}
		if err := cl.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
			return op.Noop, nil, fmt.Errorf("failed deleting stale kong.conf ConfigMap %s for DataPlane %s: %w",
				client.ObjectKeyFromObject(cm), dpNn, err)
		}
	}

	if existing == nil {
		if err := cl.Create(ctx, generated); err != nil {
			return op.Noop, nil, fmt.Errorf("failed creating kong.conf ConfigMap for DataPlane %s: %w", dpNn, err)
		}
		return op.Created, generated, nil
	}

	old := existing.DeepCopy()

	var updated bool
	updated, existing.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existing.ObjectMeta, generated.ObjectMeta)

	if !maps.Equal(existing.Data, generated.Data) {
		existing.Data = generated.Data
		updated = true
	}

	res, patched, err := patch.ApplyPatchIfNotEmpty(ctx, cl, logger, existing, old, updated)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed updating kong.conf ConfigMap for DataPlane %s: %w", dpNn, err)
	}

	return res, patched, nil
}

// deleteKongConfWatchConfigMapsForDataPlane removes the ConfigMaps holding the
// watched kong.conf owned by the given DataPlane.
//
// It runs when the feature is not in use, so that turning it off converges
// instead of leaving an orphaned ConfigMap behind.
func deleteKongConfWatchConfigMapsForDataPlane(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
) error {
	cms, err := listKongConfWatchConfigMapsForDataPlane(ctx, cl, dataplane)
	if err != nil {
		return fmt.Errorf("failed listing kong.conf ConfigMaps for DataPlane %s: %w",
			client.ObjectKeyFromObject(dataplane), err)
	}

	for i := range cms {
		if err := cl.Delete(ctx, &cms[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed deleting kong.conf ConfigMap %s for DataPlane %s: %w",
				client.ObjectKeyFromObject(&cms[i]), client.ObjectKeyFromObject(dataplane), err)
		}
	}

	return nil
}

// applyKongConfWatchForDataPlane moves the listen-related Kong settings out of
// the proxy container's environment and into a ConfigMap that the Kong Gateway
// container watches, and points the container at it with KONG_CONF_WATCH_PATH.
//
// This is what makes a listener change non-disruptive. The settings no longer
// live in the Pod template, so adding, changing or removing a Gateway listener
// leaves the Pod template byte-identical: Kubernetes creates no new ReplicaSet
// and replaces no Pod. The in-container watcher notices the ConfigMap change and
// runs `kong reload`, which rotates the Nginx workers while the master keeps its
// PID and existing connections drain on the old workers.
//
// It is a no-op, and cleans up after itself, when the DataPlane's image cannot
// watch a kong.conf.
func applyKongConfWatchForDataPlane(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	deployment *k8sresources.Deployment,
) error {
	container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return errors.New("DataPlane proxy container not found, cannot configure the watched kong.conf")
	}

	if !versions.IsDataPlaneImageKongConfWatchCapable(container.Image) {
		log.Debug(logger, "DataPlane image predates the watched kong.conf support, keeping listen settings in the environment",
			"image", container.Image,
			"minimum_version", versions.MinimumDataPlaneKongConfWatchVersion,
		)
		return deleteKongConfWatchConfigMapsForDataPlane(ctx, cl, dataplane)
	}

	managedEnv, remainingEnv := config.ExtractConfWatchEnvVars(container.Env)
	contents := config.RenderKongConf(managedEnv)

	_, cm, err := ensureKongConfWatchConfigMapForDataPlane(ctx, cl, logger, dataplane, contents)
	if err != nil {
		return err
	}
	if cm == nil {
		return fmt.Errorf("no kong.conf ConfigMap for DataPlane %s", client.ObjectKeyFromObject(dataplane))
	}

	// Drop the managed settings from the container. An environment variable
	// always wins over the watched file, so leaving them would make every
	// reload a no-op while the watcher logged a WARN for each key.
	container.Env = remainingEnv

	deployment.
		WithVolume(k8sresources.KongConfWatchVolume(cm.Name)).
		WithVolumeMount(k8sresources.KongConfWatchVolumeMount(), consts.DataPlaneProxyContainerName).
		WithEnvVar(corev1.EnvVar{
			Name:  consts.KongConfWatchPathEnvVarName,
			Value: consts.KongConfWatchPath,
		}, consts.DataPlaneProxyContainerName)

	return nil
}
