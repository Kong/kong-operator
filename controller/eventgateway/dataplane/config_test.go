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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestControlPlaneRef(t *testing.T) {
	t.Run("konnectNamespacedRef set", func(t *testing.T) {
		assert.Equal(t, shareddataplane.ControlPlaneRef{
			Kind: konnectEventGatewayKind.Kind,
			Name: reconcileTestKEGName,
		}, config.ControlPlaneRef(newReconcileEGDP()))
	})

	t.Run("konnectNamespacedRef missing: empty ref instead of a panic", func(t *testing.T) {
		egdp := newReconcileEGDP()
		egdp.Spec.ControlPlaneRef.KonnectNamespacedRef = nil
		assert.Equal(t, shareddataplane.ControlPlaneRef{}, config.ControlPlaneRef(egdp))
	})

	t.Run("onpremNamespacedRef: empty ref, it isn't supported yet", func(t *testing.T) {
		assert.Equal(t, shareddataplane.ControlPlaneRef{}, config.ControlPlaneRef(newOnPremEGDP()))
	})
}

func TestBuildContainerWithoutControlPlane(t *testing.T) {
	t.Run("konnectNamespacedRef", func(t *testing.T) {
		_, _, err := buildContainer(newReconcileEGDP(), shareddataplane.ResolvedControlPlane{}, "image", "cert", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no resolved KonnectEventGateway")
	})

	t.Run("onpremNamespacedRef", func(t *testing.T) {
		_, _, err := buildContainer(newOnPremEGDP(), shareddataplane.ResolvedControlPlane{}, "image", "cert", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "an onpremNamespacedRef is not supported by the controller yet")
	})
}

// newOnPremEGDP returns a KegDataPlane that references an OnPremEventGateway.
func newOnPremEGDP() *eventgatewayv1alpha1.KegDataPlane {
	egdp := newReconcileEGDP()
	egdp.Spec.ControlPlaneRef = eventgatewayv1alpha1.ControlPlaneRef{
		Type:                eventgatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef,
		OnPremNamespacedRef: &eventgatewayv1alpha1.OnPremNamespacedRef{Name: "my-onprem-gateway"},
	}
	return egdp
}

func TestBuildEventGatewayDataPlaneCertificate(t *testing.T) {
	egdp := newReconcileEGDP()
	cert := buildEventGatewayDataPlaneCertificate(
		egdp,
		shareddataplane.ResolvedControlPlane{Kind: konnectEventGatewayKind.Kind, IsKonnect: true, Object: newProgrammedKEG()},
		"cert-secret", "checksum",
	)

	// New certificates reference the gateway through the deprecated
	// spec.gatewayRef, which CRDs of releases without parentRef understand, so
	// that rolling the operator back leaves them intact.
	assert.Equal(t, commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestKEGName},
	}, cert.Spec.GatewayRef)
	assert.Nil(t, cert.Spec.ParentRef)

	assert.Equal(t, egdp.Namespace, cert.Namespace)
	assert.Equal(t, shareddataplane.CertEntityName(egdp.Name, "checksum"), cert.Name)
	require.NotNil(t, cert.Spec.APISpec.Certificate.SecretRef)
	assert.Equal(t, "cert-secret", cert.Spec.APISpec.Certificate.SecretRef.Name)
}

func TestKeepEventGatewayDataPlaneCertificateRefField(t *testing.T) {
	build := func() *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		return buildEventGatewayDataPlaneCertificate(
			newReconcileEGDP(),
			shareddataplane.ResolvedControlPlane{Kind: konnectEventGatewayKind.Kind, IsKonnect: true, Object: newProgrammedKEG()},
			"cert-secret", "checksum",
		)
	}
	gatewayRef := commonv1alpha1.ObjectRef{
		Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: reconcileTestKEGName},
	}
	parentRef := &commonv1alpha1.GatewayRef{
		Group: "konnect.konghq.com",
		Kind:  "KonnectEventGateway",
		Name:  reconcileTestKEGName,
	}

	t.Run("existing certificate using gatewayRef keeps gatewayRef", func(t *testing.T) {
		desired := build()
		existing := build()
		keepEventGatewayDataPlaneCertificateRefField(desired, existing)
		assert.Equal(t, gatewayRef, desired.Spec.GatewayRef)
		assert.Nil(t, desired.Spec.ParentRef)
	})

	t.Run("existing certificate using parentRef keeps parentRef", func(t *testing.T) {
		desired := build()
		existing := build()
		existing.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
		existing.Spec.ParentRef = parentRef.DeepCopy()
		keepEventGatewayDataPlaneCertificateRefField(desired, existing)
		assert.Equal(t, parentRef, desired.Spec.ParentRef)
		assert.Zero(t, desired.Spec.GatewayRef)
	})
}

func TestTerminationGracePeriodSeconds(t *testing.T) {
	withRuntime := func(rt *eventgatewayv1alpha1.RuntimeOptions) *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{Runtime: rt}},
		}
	}
	withEnv := func(egdp *eventgatewayv1alpha1.KegDataPlane, container string, env ...corev1.EnvVar) *eventgatewayv1alpha1.KegDataPlane {
		egdp.Spec.Deployment = &eventgatewayv1alpha1.DeploymentOptions{
			PodTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: container, Env: env}}},
			},
		}
		return egdp
	}
	drainEnv := func(v string) corev1.EnvVar { return corev1.EnvVar{Name: EnvRuntimeDrainDuration, Value: v} }
	shutdownEnv := func(v string) corev1.EnvVar { return corev1.EnvVar{Name: EnvRuntimeShutdownTimeout, Value: v} }
	for _, tc := range []struct {
		name string
		egdp *eventgatewayv1alpha1.KegDataPlane
		want *int64
	}{
		{name: "no configuration keeps the Kubernetes default", egdp: &eventgatewayv1alpha1.KegDataPlane{}},
		{name: "no runtime options keep the Kubernetes default", egdp: withRuntime(nil)},
		{name: "keg's defaults fit in the Kubernetes default", egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{})},
		{
			name: "a raised drain that still fits keeps the Kubernetes default",
			egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(15))}),
		},
		{
			name: "a drain past the default: drain, keg's default shutdown and the margin",
			egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(60))}),
			want: new(int64(60 + 10 + 5)),
		},
		{
			name: "a shutdown past the default: keg's default drain, shutdown and the margin",
			egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{ShutdownTimeoutSeconds: new(int32(40))}),
			want: new(int64(5 + 40 + 5)),
		},
		{
			name: "both set",
			egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(20)), ShutdownTimeoutSeconds: new(int32(20))}),
			want: new(int64(20 + 20 + 5)),
		},
		{
			name: "a drain of 0, the shutdown alone past the default",
			egdp: withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(0)), ShutdownTimeoutSeconds: new(int32(30))}),
			want: new(int64(0 + 30 + 5)),
		},
		{
			name: "a drain set by the keg container env, without runtime options",
			egdp: withEnv(&eventgatewayv1alpha1.KegDataPlane{}, consts.KEGContainerName, drainEnv("60s")),
			want: new(int64(60 + 10 + 5)),
		},
		{
			name: "the keg container env wins over the runtime options",
			egdp: withEnv(
				withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(60)), ShutdownTimeoutSeconds: new(int32(40))}),
				consts.KEGContainerName, drainEnv("2m"), shutdownEnv("5s"),
			),
			want: new(int64(120 + 5 + 5)),
		},
		{
			name: "an env lowering the runtime options under the default keeps the Kubernetes default",
			egdp: withEnv(
				withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(60))}),
				consts.KEGContainerName, drainEnv("1s"),
			),
		},
		{
			name: "env durations are rounded up to the second",
			egdp: withEnv(&eventgatewayv1alpha1.KegDataPlane{}, consts.KEGContainerName, drainEnv("1h30m"), shutdownEnv("1500ms")),
			want: new(int64(5400 + 2 + 5)),
		},
		{
			name: "an env that isn't a duration falls back to the runtime options",
			egdp: withEnv(
				withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(60))}),
				consts.KEGContainerName, drainEnv("a minute"),
			),
			want: new(int64(60 + 10 + 5)),
		},
		{
			name: "an env from valueFrom falls back to keg's default",
			egdp: withEnv(&eventgatewayv1alpha1.KegDataPlane{}, consts.KEGContainerName, corev1.EnvVar{
				Name: EnvRuntimeDrainDuration,
				ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					Name: "keg", Key: "drain",
				}},
			}),
		},
		{
			name: "the env of another container is ignored",
			egdp: withEnv(&eventgatewayv1alpha1.KegDataPlane{}, "sidecar", drainEnv("60s")),
		},
		{
			name: "a negative env duration falls back to the runtime options",
			egdp: withEnv(
				withRuntime(&eventgatewayv1alpha1.RuntimeOptions{DrainDurationSeconds: new(int32(60))}),
				consts.KEGContainerName, drainEnv("-5s"),
			),
			want: new(int64(60 + 10 + 5)),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, terminationGracePeriodSeconds(tc.egdp))
		})
	}
}
