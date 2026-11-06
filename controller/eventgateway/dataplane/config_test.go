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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
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

	t.Run("onpremNamespacedRef set", func(t *testing.T) {
		assert.Equal(t, shareddataplane.ControlPlaneRef{
			Kind: onPremEventGatewayKind.Kind,
			Name: "my-onprem-gateway",
		}, config.ControlPlaneRef(newOnPremEGDP()))
	})
}

func TestBuildContainerWithoutControlPlane(t *testing.T) {
	_, _, err := buildContainer(newReconcileEGDP(), shareddataplane.ResolvedControlPlane{}, "image", "cert", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no resolved control plane")
}

func TestBuildContainerImage(t *testing.T) {
	gateway, gw, bundle, ca := onPremFixture(t)
	cl := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(gw, bundle, ca).Build()
	onPrem := shareddataplane.ResolvedControlPlane{Dependencies: resolveOnPremDependencies(t, cl, gateway)}
	konnect := shareddataplane.ResolvedControlPlane{Object: &konnectv1alpha1.KonnectEventGateway{
		Status: konnectv1alpha1.KonnectEventGatewayStatus{ServerURL: "https://us.api.konghq.tech"},
	}}

	for _, tc := range []struct {
		name  string
		egdp  *eventgatewayv1alpha1.KegDataPlane
		cp    shareddataplane.ResolvedControlPlane
		image string
		want  string
	}{
		{name: "on-prem: the default image is the same", egdp: newOnPremEGDP(), cp: onPrem, image: consts.DefaultKEGImage, want: consts.DefaultKEGImage},
		{name: "on-prem: another image, e.g. RELATED_IMAGE_KEG, wins", egdp: newOnPremEGDP(), cp: onPrem, image: "custom/keg:1.0", want: "custom/keg:1.0"},
		{name: "Konnect: the default image stays", egdp: newReconcileEGDP(), cp: konnect, image: consts.DefaultKEGImage, want: consts.DefaultKEGImage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container, _, err := buildContainer(tc.egdp, tc.cp, tc.image, "cert", "")
			require.NoError(t, err)
			assert.Equal(t, tc.want, container.Image)
		})
	}
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
			name: "the longest env duration is rounded up without overflowing",
			egdp: withEnv(&eventgatewayv1alpha1.KegDataPlane{}, consts.KEGContainerName, drainEnv("2562047h47m16.854775807s")),
			want: new(int64(9223372037 + 10 + 5)),
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

func TestServiceOptions(t *testing.T) {
	kafka := &eventgatewayv1alpha1.ServiceOptions{
		Type:                  corev1.ServiceTypeLoadBalancer,
		Annotations:           map[string]string{"a": "b"},
		Labels:                map[eventgatewayv1alpha1.LabelName]eventgatewayv1alpha1.LabelValue{"team": "kafka"},
		ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
		TrafficDistribution:   new("PreferClose"),
		InternalTrafficPolicy: new(corev1.ServiceInternalTrafficPolicyCluster),
		Ports: []eventgatewayv1alpha1.ServicePort{{
			Name:       new("kafka"),
			Port:       9093,
			TargetPort: new(intstr.FromInt32(9092)),
			NodePort:   new(int32(30093)),
		}},
	}
	for _, tc := range []struct {
		name    string
		network *eventgatewayv1alpha1.NetworkOptions
		want    *shareddataplane.ServiceOptions
	}{
		{name: "no network"},
		{name: "no services", network: &eventgatewayv1alpha1.NetworkOptions{}},
		{name: "no Kafka Service", network: &eventgatewayv1alpha1.NetworkOptions{Services: &eventgatewayv1alpha1.Services{}}},
		{
			name:    "the Kafka Service",
			network: &eventgatewayv1alpha1.NetworkOptions{Services: &eventgatewayv1alpha1.Services{Kafka: kafka}},
			want: &shareddataplane.ServiceOptions{
				Type:                  corev1.ServiceTypeLoadBalancer,
				Annotations:           map[string]string{"a": "b"},
				Labels:                map[string]string{"team": "kafka"},
				ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
				TrafficDistribution:   new("PreferClose"),
				InternalTrafficPolicy: new(corev1.ServiceInternalTrafficPolicyCluster),
				Ports: []shareddataplane.ServicePort{{
					Name:       new("kafka"),
					Port:       9093,
					TargetPort: new(intstr.FromInt32(9092)),
					NodePort:   new(int32(30093)),
				}},
			},
		},
		{
			name: "a Kafka Service without ports nor labels",
			network: &eventgatewayv1alpha1.NetworkOptions{Services: &eventgatewayv1alpha1.Services{
				Kafka: &eventgatewayv1alpha1.ServiceOptions{Type: corev1.ServiceTypeClusterIP},
			}},
			want: &shareddataplane.ServiceOptions{Type: corev1.ServiceTypeClusterIP, Labels: map[string]string{}, Ports: []shareddataplane.ServicePort{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			egdp := newReconcileEGDP()
			egdp.Spec.Network = tc.network
			assert.Equal(t, tc.want, serviceOptions(egdp))
		})
	}
}

func TestSetStatusAddresses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		addrs []operatorv1beta1.Address
		want  []eventgatewayv1alpha1.Address
	}{
		{name: "no address", want: []eventgatewayv1alpha1.Address{}},
		{
			name: "addresses with and without type",
			addrs: []operatorv1beta1.Address{
				{Type: new(operatorv1beta1.IPAddressType), Value: "10.0.0.1", SourceType: operatorv1beta1.PrivateIPAddressSourceType},
				{Value: "keg.example.com", SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType},
			},
			want: []eventgatewayv1alpha1.Address{
				{
					Type:       new(eventgatewayv1alpha1.AddressType(operatorv1beta1.IPAddressType)),
					Value:      "10.0.0.1",
					SourceType: eventgatewayv1alpha1.AddressSourceType(operatorv1beta1.PrivateIPAddressSourceType),
				},
				{Value: "keg.example.com", SourceType: eventgatewayv1alpha1.AddressSourceType(operatorv1beta1.PublicLoadBalancerAddressSourceType)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			egdp := newReconcileEGDP()
			egdp.Status.Addresses = []eventgatewayv1alpha1.Address{{Value: "stale"}}
			setStatusAddresses(egdp, tc.addrs)
			assert.Equal(t, tc.want, egdp.Status.Addresses)
		})
	}
}

func TestCheckKonnectEventGatewayEndpoints(t *testing.T) {
	const (
		publicServerURL  = "https://us.api.konghq.com"
		privateServerURL = "https://us.svc.konghq.tech/api"
	)

	tests := []struct {
		name            string
		serverURL       string
		domain          *string
		wantErr         bool
		wantErrContains string
	}{
		{name: "public", serverURL: publicServerURL},
		{name: "public with a port", serverURL: "https://us.api.konghq.tech:443"},
		{name: "trailing slash only", serverURL: "https://us.api.konghq.tech/"},
		{name: "empty server URL in status", serverURL: ""},
		{name: "private", serverURL: privateServerURL, wantErr: true, wantErrContains: privateServerURL},
		{name: "private with a port", serverURL: "https://us.svc.konghq.tech:443/api", wantErr: true, wantErrContains: "us.svc.konghq.tech:443/api"},
		{name: "explicit domain", serverURL: privateServerURL, domain: new("us.svc.konghq.tech")},
		{name: "explicit empty domain", serverURL: privateServerURL, domain: new(""), wantErr: true, wantErrContains: privateServerURL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keg := &konnectv1alpha1.KonnectEventGateway{}
			keg.Status.ServerURL = tc.serverURL

			egdp := &eventgatewayv1alpha1.KegDataPlane{}
			if tc.domain != nil {
				egdp.Spec.Config = &eventgatewayv1alpha1.KegDataPlaneConfiguration{
					Konnect: &eventgatewayv1alpha1.KonnectConfig{Domain: tc.domain},
				}
			}

			err := checkKonnectEventGatewayEndpoints(egdp, keg)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErrContains)
			assert.Contains(t, err.Error(), "spec.config.konnect.domain")
		})
	}
}

// TestCheckKonnectEventGatewayEndpointsUnexpectedObject asserts the verdict fails
// closed on an object of another kind instead of calling the address usable.
func TestCheckKonnectEventGatewayEndpointsUnexpectedObject(t *testing.T) {
	err := checkKonnectEventGatewayEndpoints(
		&eventgatewayv1alpha1.KegDataPlane{},
		&eventgatewayv1alpha1.OnPremEventGateway{},
	)
	require.ErrorContains(t, err, "unexpected control plane object")

	err = checkKonnectEventGatewayEndpoints(
		&aigatewayv1alpha1.AIGatewayDataPlane{},
		&konnectv1alpha1.KonnectEventGateway{},
	)
	require.ErrorContains(t, err, "unexpected DataPlane object")
}
