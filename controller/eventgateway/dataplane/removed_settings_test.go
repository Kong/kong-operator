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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// TestRemovedKEGSettings checks the settings keg 1.3.0 removed are passed to
// an older keg only, and reported as ignored otherwise.
func TestRemovedKEGSettings(t *testing.T) {
	egdp := &eventgatewayv1alpha1.KegDataPlane{
		Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
			Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
				ConfigPollIntervalSeconds: new(int32(30)),
				EnableDebugEndpoints:      new(eventgatewayv1alpha1.DebugEndpointsStateEnabled),
			},
		},
	}
	for _, tc := range []struct {
		image    string
		passed   bool
		untagged bool
	}{
		{image: "kong/kong-event-gateway:1.1.0", passed: true},
		{image: "kong/kong-event-gateway:1.2.0", passed: true},
		{image: "kong/kong-event-gateway:1.2.7-debian", passed: true},
		{image: "registry.example.com:5000/kong/kong-event-gateway:1.2.0", passed: true},
		{image: "kong/kong-event-gateway:1.3.0"},
		{image: "kong/kong-event-gateway:1.3.0-rc.1"},
		{image: "kong/kong-event-gateway:1.4.2"},
		{image: "kong/kong-event-gateway:main", untagged: true},
		{image: "kong/kong-event-gateway@sha256:7829e796664079b6c1e6187aa58d003791e4090d9c196af5fab172ec43239a44", untagged: true},
		{image: "kong/kong-event-gateway", untagged: true},
	} {
		t.Run(tc.image, func(t *testing.T) {
			envVars := commonKEGEnvVars(egdp, tc.image)
			has := func(name string) bool {
				for _, e := range envVars {
					if e.Name == name {
						return true
					}
				}
				return false
			}
			ignored := ignoredKEGSettings(egdp, tc.image)
			if tc.passed {
				assert.True(t, has(EnvConfigPollInterval))
				assert.True(t, has(EnvEnableDebugEndpoints))
				assert.Empty(t, ignored)
				return
			}
			assert.False(t, has(EnvConfigPollInterval), "keg refuses to start with it")
			assert.False(t, has(EnvEnableDebugEndpoints), "keg refuses to start with it")
			require.Len(t, ignored, 2)
			assert.Contains(t, ignored[0], "spec.config.configPollIntervalSeconds is ignored")
			assert.Contains(t, ignored[1], "spec.config.enableDebugEndpoints is ignored")
			if tc.untagged {
				assert.Contains(t, ignored[0], "the image "+tc.image+" has no version tag")
			} else {
				assert.Contains(t, ignored[0], "the keg of the image "+tc.image+" is 1.3.0 or later")
			}
		})
	}

	t.Run("nothing set, nothing ignored", func(t *testing.T) {
		assert.Empty(t, ignoredKEGSettings(&eventgatewayv1alpha1.KegDataPlane{}, "kong/kong-event-gateway:1.3.0"))
		assert.Empty(t, ignoredKEGSettings(&eventgatewayv1alpha1.KegDataPlane{
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{}},
		}, "kong/kong-event-gateway:1.3.0"))
	})

}

// TestBuildContainerRemovedKEGSettings checks the container gets the settings
// keg 1.3.0 removed only for an older keg, from the image the Deployment
// runs, on both kinds of control plane: the default one, or one the user
// sets in the pod template of the KegDataPlane.
func TestBuildContainerRemovedKEGSettings(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	onPrem := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	konnect := shareddataplane.ResolvedControlPlane{Object: &konnectv1alpha1.KonnectEventGateway{
		Status: konnectv1alpha1.KonnectEventGatewayStatus{ServerURL: "https://us.api.konghq.tech"},
	}}
	withSettings := func(egdp *eventgatewayv1alpha1.KegDataPlane, image string) *eventgatewayv1alpha1.KegDataPlane {
		egdp.Spec.Config = &eventgatewayv1alpha1.KegDataPlaneConfiguration{
			ConfigPollIntervalSeconds: new(int32(30)),
			EnableDebugEndpoints:      new(eventgatewayv1alpha1.DebugEndpointsStateEnabled),
		}
		if image != "" {
			egdp.Spec.Deployment = &eventgatewayv1alpha1.DeploymentOptions{PodTemplateSpec: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: consts.KEGContainerName, Image: image}}},
			}}
		}
		return egdp
	}
	for _, tc := range []struct {
		name   string
		egdp   *eventgatewayv1alpha1.KegDataPlane
		cp     shareddataplane.ResolvedControlPlane
		passed bool
	}{
		{name: "on-prem, default keg", egdp: withSettings(newOnPremEGDP(), ""), cp: onPrem},
		{name: "on-prem, keg 1.2 in the pod template", egdp: withSettings(newOnPremEGDP(), "kong/kong-event-gateway:1.2.0"), cp: onPrem, passed: true},
		{name: "Konnect, default keg", egdp: withSettings(newReconcileEGDP(), ""), cp: konnect},
		{name: "Konnect, keg 1.2 in the pod template", egdp: withSettings(newReconcileEGDP(), "kong/kong-event-gateway:1.2.0"), cp: konnect, passed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container, _, err := buildContainer(tc.egdp, tc.cp, shareddataplane.ResolveImage(tc.egdp, config.Deployment), "cert", "")
			require.NoError(t, err)
			names := make([]string, 0, len(container.Env))
			for _, e := range container.Env {
				names = append(names, e.Name)
			}
			if tc.passed {
				assert.Contains(t, names, EnvConfigPollInterval)
				assert.Contains(t, names, EnvEnableDebugEndpoints)
			} else {
				assert.NotContains(t, names, EnvConfigPollInterval, "keg refuses to start with it")
				assert.NotContains(t, names, EnvEnableDebugEndpoints, "keg refuses to start with it")
			}
		})
	}
}

// TestKEGConfigReportsIgnoredSettings checks the KegDataPlane controller
// reports the settings the default keg ignores (see
// shareddataplane.DeploymentConfig.IgnoredSettings).
func TestKEGConfigReportsIgnoredSettings(t *testing.T) {
	require.NotNil(t, config.Deployment.IgnoredSettings)
	egdp := newReconcileEGDP()
	egdp.Spec.Config = &eventgatewayv1alpha1.KegDataPlaneConfiguration{
		ConfigPollIntervalSeconds: new(int32(30)),
		EnableDebugEndpoints:      new(eventgatewayv1alpha1.DebugEndpointsStateEnabled),
	}
	assert.Len(t, config.Deployment.IgnoredSettings(egdp, consts.DefaultKEGImage), 2)
}
