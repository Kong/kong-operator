package dataplane

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

const (
	confWatchTestImage = "kong/kong-gateway:3.17.0.0"
	// confWatchTestLabelSelector mirrors the operator's default
	// --configmap-label-selector.
	confWatchTestLabelSelector = "konghq.com/configmap"
)

func confWatchTestDataPlane() *operatorv1beta1.DataPlane {
	return &operatorv1beta1.DataPlane{
		Name:      "test-dataplane",
		Namespace: "default",
		UID:       types.UID("test-uid"),
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: confWatchTestImage,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func confWatchTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, operatorv1beta1.AddToScheme(s))
	return s
}

func buildWithConfWatch(
	t *testing.T,
	dataplane *operatorv1beta1.DataPlane,
	enabled bool,
) *appsv1.Deployment {
	t.Helper()

	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(confWatchTestScheme(t)).
		WithObjects(dataplane).
		Build()

	deployment, _, err := NewDeploymentBuilder(logr.Discard(), cl).
		WithDefaultImage(confWatchTestImage).
		WithClusterCertificate("test-cert").
		WithIPFamily(ipfamily.IPv4).
		WithConfWatch(enabled).
		WithConfigMapLabelSelector(confWatchTestLabelSelector).
		BuildAndDeploy(t.Context(), dataplane, false, false)
	require.NoError(t, err)
	require.NotNil(t, deployment)

	return deployment
}

func TestApplyKongConfWatchForDataPlane(t *testing.T) {
	t.Run("moves the listen settings out of the environment", func(t *testing.T) {
		dataplane := confWatchTestDataPlane()
		deployment := buildWithConfWatch(t, dataplane, true)

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)

		// An environment variable always wins over the watched file, so these have
		// to be gone from the container or every reload would be a no-op.
		assert.Empty(t, k8sutils.EnvValueByName(container.Env, "KONG_PROXY_LISTEN"))
		assert.Empty(t, k8sutils.EnvValueByName(container.Env, "KONG_PORT_MAPS"))

		// The operator-internal listens stay environment variables.
		assert.NotEmpty(t, k8sutils.EnvValueByName(container.Env, "KONG_ADMIN_LISTEN"))
		assert.NotEmpty(t, k8sutils.EnvValueByName(container.Env, "KONG_STATUS_LISTEN"))

		assert.Equal(t, consts.KongConfWatchPath,
			k8sutils.EnvValueByName(container.Env, consts.KongConfWatchPathEnvVarName))
		assert.Equal(t, consts.DataPlaneDefaultWorkerShutdownTimeout,
			k8sutils.EnvValueByName(container.Env, "KONG_NGINX_MAIN_WORKER_SHUTDOWN_TIMEOUT"))
	})

	t.Run("mounts the ConfigMap as a directory, never with SubPath", func(t *testing.T) {
		dataplane := confWatchTestDataPlane()
		deployment := buildWithConfWatch(t, dataplane, true)

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)

		var mount *corev1.VolumeMount
		for i, m := range container.VolumeMounts {
			if m.Name == consts.KongConfWatchVolume {
				mount = &container.VolumeMounts[i]
			}
		}
		require.NotNil(t, mount, "the kong.conf volume is not mounted")

		// Kubernetes does not update a file mounted with SubPath, so the watcher
		// would never see a change and no reload would ever happen.
		assert.Empty(t, mount.SubPath)
		assert.Equal(t, consts.KongConfWatchVolumeMountPath, mount.MountPath)
		assert.True(t, mount.ReadOnly)

		var volume *corev1.Volume
		for i, v := range deployment.Spec.Template.Spec.Volumes {
			if v.Name == consts.KongConfWatchVolume {
				volume = &deployment.Spec.Template.Spec.Volumes[i]
			}
		}
		require.NotNil(t, volume)
		require.NotNil(t, volume.ConfigMap)
		assert.Equal(t,
			k8sresources.KongConfWatchConfigMapName(dataplane.Name),
			volume.ConfigMap.Name,
		)
	})

	t.Run("leaves the environment alone when disabled", func(t *testing.T) {
		dataplane := confWatchTestDataPlane()
		deployment := buildWithConfWatch(t, dataplane, false)

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)

		assert.NotEmpty(t, k8sutils.EnvValueByName(container.Env, "KONG_PROXY_LISTEN"))
		assert.Empty(t, k8sutils.EnvValueByName(container.Env, consts.KongConfWatchPathEnvVarName))
	})

	t.Run("an image that predates the feature keeps the environment", func(t *testing.T) {
		dataplane := confWatchTestDataPlane()
		dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Image = "kong/kong-gateway:3.16.0.0"

		deployment := buildWithConfWatch(t, dataplane, true)

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)

		assert.NotEmpty(t, k8sutils.EnvValueByName(container.Env, "KONG_PROXY_LISTEN"))
		assert.Empty(t, k8sutils.EnvValueByName(container.Env, consts.KongConfWatchPathEnvVarName))
	})
}

// TestConfWatchListenerChangeDoesNotChangePodTemplate is the regression test for
// the property the whole feature exists to provide: changing a listener must not
// change the Pod template.
//
// Kubernetes creates a new ReplicaSet, and so replaces Pods, only when
// .spec.template changes. As long as the listen settings live in a ConfigMap
// that is referenced by a stable name, the operator can keep updating the
// Deployment object while the Pods stay exactly where they are, and the
// in-container watcher reloads Kong instead.
func TestConfWatchListenerChangeDoesNotChangePodTemplate(t *testing.T) {
	scheme := confWatchTestScheme(t)

	dataplane := confWatchTestDataPlane()
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dataplane).
		Build()

	build := func(dp *operatorv1beta1.DataPlane) *appsv1.Deployment {
		t.Helper()
		deployment, _, err := NewDeploymentBuilder(logr.Discard(), cl).
			WithDefaultImage(confWatchTestImage).
			WithClusterCertificate("test-cert").
			WithIPFamily(ipfamily.IPv4).
			WithConfWatch(true).
			WithConfigMapLabelSelector(confWatchTestLabelSelector).
			BuildAndDeploy(t.Context(), dp, true, false)
		require.NoError(t, err)
		require.NotNil(t, deployment)
		return deployment
	}

	// Build twice before the comparison. The first call creates the Deployment,
	// and the operator only applies Pod template defaults once one exists, so
	// comparing a create against an update would report differences that a
	// running cluster never sees. The listener change we care about always
	// happens against an existing Deployment.
	build(dataplane)
	before := build(dataplane).DeepCopy()

	confMapNN := types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      k8sresources.KongConfWatchConfigMapName(dataplane.Name),
	}
	var cmBefore corev1.ConfigMap
	require.NoError(t, cl.Get(t.Context(), confMapNN, &cmBefore))
	require.Contains(t, cmBefore.Data[consts.KongConfWatchFileName], "stream_listen = off")

	// Add a stream listener, the way the Gateway controller does when a TCP
	// listener is added to a Gateway.
	dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "KONG_STREAM_LISTEN", Value: "0.0.0.0:8899 reuseport"},
	}

	after := build(dataplane)

	// The ConfigMap picked the new listener up...
	var cmAfter corev1.ConfigMap
	require.NoError(t, cl.Get(t.Context(), confMapNN, &cmAfter))
	assert.Contains(t, cmAfter.Data[consts.KongConfWatchFileName], "stream_listen = 0.0.0.0:8899 reuseport")
	assert.NotEqual(t, cmBefore.Data, cmAfter.Data, "the ConfigMap contents should have changed")

	// ...and the ConfigMap kept its name, so the Pod template still points at it.
	assert.Equal(t, cmBefore.Name, cmAfter.Name)

	// The actual assertion: nothing in the Pod template moved, so Kubernetes
	// creates no new ReplicaSet and replaces no Pod.
	assert.Equal(t, before.Spec.Template, after.Spec.Template,
		"a listener change must not alter the Pod template, otherwise the Pods roll")
}

func TestKongConfWatchConfigMapCleanup(t *testing.T) {
	scheme := confWatchTestScheme(t)

	dataplane := confWatchTestDataPlane()
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dataplane).
		Build()

	build := func(enabled bool) {
		t.Helper()
		_, _, err := NewDeploymentBuilder(logr.Discard(), cl).
			WithDefaultImage(confWatchTestImage).
			WithClusterCertificate("test-cert").
			WithIPFamily(ipfamily.IPv4).
			WithConfWatch(enabled).
			WithConfigMapLabelSelector(confWatchTestLabelSelector).
			BuildAndDeploy(t.Context(), dataplane, true, false)
		require.NoError(t, err)
	}

	confMapNN := types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      k8sresources.KongConfWatchConfigMapName(dataplane.Name),
	}

	build(true)
	var cm corev1.ConfigMap
	require.NoError(t, cl.Get(t.Context(), confMapNN, &cm))

	// Turning the feature off has to converge, not leave an orphan behind.
	build(false)
	err := cl.Get(t.Context(), confMapNN, &cm)
	assert.Error(t, err, "the kong.conf ConfigMap should have been removed")
}

// TestConfWatchConfigMapCarriesCacheLabel guards a failure found on a real
// cluster: the manager's cache only ingests ConfigMaps carrying the operator's
// --configmap-label-selector label. Without it the operator cannot read back the
// ConfigMap it just created, so every reconciliation tries to create it again,
// fails with AlreadyExists and the DataPlane never converges.
func TestConfWatchConfigMapCarriesCacheLabel(t *testing.T) {
	dataplane := confWatchTestDataPlane()
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(confWatchTestScheme(t)).
		WithObjects(dataplane).
		Build()

	_, _, err := NewDeploymentBuilder(logr.Discard(), cl).
		WithDefaultImage(confWatchTestImage).
		WithClusterCertificate("test-cert").
		WithIPFamily(ipfamily.IPv4).
		WithConfWatch(true).
		WithConfigMapLabelSelector(confWatchTestLabelSelector).
		BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)

	var cm corev1.ConfigMap
	require.NoError(t, cl.Get(t.Context(), types.NamespacedName{
		Namespace: dataplane.Namespace,
		Name:      k8sresources.KongConfWatchConfigMapName(dataplane.Name),
	}, &cm))

	assert.Equal(t, "true", cm.Labels[confWatchTestLabelSelector],
		"the ConfigMap must carry the operator's configmap-label-selector label, or the manager's cache will not see it")
}
