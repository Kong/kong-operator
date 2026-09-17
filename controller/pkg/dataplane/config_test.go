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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// This test fixture wires the generic reconciler with the real AIGateway API
// types so the shared logic is exercised against production-grade types.

const (
	testCASecretName      = "test-ca"
	testCASecretNamespace = "test-ns"
	testDPName            = "my-dp"

	testDefaultIngressPort int32 = 8443

	reconcileTestNS         = testCASecretNamespace
	reconcileTestDPName     = testDPName
	reconcileTestAIGWCPName = "my-aigwcp"
)

// testReconciler is the generic Reconciler instantiated with the test types.
type testReconciler = Reconciler[
	*aigatewayv1alpha1.AIGatewayDataPlane,
	*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
]

// testControlPlaneKind mirrors the KonnectAIGateway control plane kind
// configuration of the AIGatewayDataPlane controller.
var testControlPlaneKind = ControlPlaneKindConfig{
	Kind:                      "KonnectAIGateway",
	NewObject:                 func() ControlPlaneObject { return &konnectv1alpha1.KonnectAIGateway{} },
	ControlPlaneRefIndexField: index.IndexFieldAIGatewayDataPlaneOnKonnectAIGateway,
	IsKonnect:                 true,
	Conditions: ControlPlaneConditions{
		ResolvedType:         string(aigatewayv1alpha1.KonnectAIGatewayResolvedType),
		ResolvedReason:       string(aigatewayv1alpha1.ControlPlaneResolvedReason),
		ResolvedMessage:      aigatewayv1alpha1.KonnectAIGatewayResolvedMessage,
		NotFoundReason:       string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		NotFoundMessage:      aigatewayv1alpha1.KonnectAIGatewayNotFoundMessage,
		NotProgrammedReason:  string(aigatewayv1alpha1.KonnectAIGatewayNotProgrammedReason),
		NotProgrammedMessage: aigatewayv1alpha1.KonnectAIGatewayNotProgrammedMessage,
	},
}

// testOnPremControlPlaneKind mirrors the OnPremAIGateway control plane kind
// configuration of the AIGatewayDataPlane controller: not Konnect-backed, with
// a readiness condition gate.
var testOnPremControlPlaneKind = ControlPlaneKindConfig{
	Kind:                      "OnPremAIGateway",
	NewObject:                 func() ControlPlaneObject { return &aigatewayv1alpha1.OnPremAIGateway{} },
	ControlPlaneRefIndexField: index.IndexFieldAIGatewayDataPlaneOnOnPremAIGateway,
	IsKonnect:                 false,
	ReadinessConditionType:    string(aigatewayv1alpha1.ReadyType),
	Conditions: ControlPlaneConditions{
		ResolvedType:    string(aigatewayv1alpha1.OnPremAIGatewayResolvedType),
		ResolvedReason:  string(aigatewayv1alpha1.ControlPlaneResolvedReason),
		ResolvedMessage: aigatewayv1alpha1.OnPremAIGatewayResolvedMessage,
		NotFoundReason:  string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		NotFoundMessage: aigatewayv1alpha1.OnPremAIGatewayNotFoundMessage,
		NotReadyReason:  string(aigatewayv1alpha1.OnPremAIGatewayNotReadyReason),
		NotReadyMessage: aigatewayv1alpha1.OnPremAIGatewayNotReadyMessage,
	},
}

// testConfig mirrors the AIGatewayDataPlane controller configuration with a
// stubbed container builder.
var testConfig = Config[
	*aigatewayv1alpha1.AIGatewayDataPlane,
	*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
]{
	ControllerName: "aigw-dataplane",
	Kind:           "AIGatewayDataPlane",

	NewObject: func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{}
	},
	NewCertificateObject: func() *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
		return &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
	},
	NewObjectList: func() client.ObjectList {
		return &aigatewayv1alpha1.AIGatewayDataPlaneList{}
	},

	ControlPlaneRef: func(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) ControlPlaneRef {
		ref := aigwdp.Spec.ControlPlaneRef
		if ref == nil {
			return ControlPlaneRef{}
		}
		switch ref.Type {
		case aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef:
			if ref.KonnectNamespacedRef == nil {
				return ControlPlaneRef{}
			}
			return ControlPlaneRef{
				Kind: testControlPlaneKind.Kind,
				Name: ref.KonnectNamespacedRef.Name,
			}
		case aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef:
			if ref.OnPremNamespacedRef == nil {
				return ControlPlaneRef{}
			}
			return ControlPlaneRef{
				Kind: testOnPremControlPlaneKind.Kind,
				Name: ref.OnPremNamespacedRef.Name,
			}
		default:
			return ControlPlaneRef{}
		}
	},
	ControlPlanes: []ControlPlaneKindConfig{testControlPlaneKind, testOnPremControlPlaneKind},

	Conditions: Conditions{
		ReadyType:                    string(aigatewayv1alpha1.ReadyType),
		ResourceReadyReason:          string(aigatewayv1alpha1.ResourceReadyReason),
		DependenciesNotReadyReason:   string(aigatewayv1alpha1.DependenciesNotReadyReason),
		DependenciesNotReadyMessage:  aigatewayv1alpha1.DependenciesNotReadyMessage,
		WaitingToBecomeReadyReason:   string(aigatewayv1alpha1.WaitingToBecomeReadyReason),
		WaitingToBecomeReadyMessage:  aigatewayv1alpha1.WaitingToBecomeReadyMessage,
		UnableToProvisionReason:      string(aigatewayv1alpha1.UnableToProvisionReason),
		CertificateProvisionedType:   string(aigatewayv1alpha1.CertificateProvisionedType),
		CertificateProvisionedReason: string(aigatewayv1alpha1.CertificateProvisionedReason),

		KonnectCertificateRegisteredType:           string(aigatewayv1alpha1.KonnectCertificateRegisteredType),
		KonnectCertificateRegisteredReason:         string(aigatewayv1alpha1.KonnectCertificateRegisteredReason),
		KonnectCertificateRegistrationFailedReason: string(aigatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		KonnectCertificateNotProgrammedReason:      string(aigatewayv1alpha1.KonnectCertificateNotProgrammedReason),

		ServiceReadyType:         string(aigatewayv1alpha1.ServiceReadyType),
		ServiceReadyReason:       string(aigatewayv1alpha1.ServiceReadyReason),
		ServiceReadyMessage:      aigatewayv1alpha1.ServiceReadyMessage,
		WaitingForAddressReason:  string(aigatewayv1alpha1.WaitingForAddressReason),
		WaitingForAddressMessage: aigatewayv1alpha1.WaitingForAddressMessage,
	},

	Certificate: CertificateConfig[
		*aigatewayv1alpha1.AIGatewayDataPlane,
		*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
	]{
		LabelKey: consts.SecretAIGatewayDataPlaneCertificateLabel,
		Kind:     "AIGatewayDataPlaneCertificate",
		Build:    buildTestCertificate,
		Ensure:   secrets.EnsureCertificate[*aigatewayv1alpha1.AIGatewayDataPlane],
	},

	Deployment: DeploymentConfig[*aigatewayv1alpha1.AIGatewayDataPlane]{
		ContainerName:       consts.AIGatewayDataPlaneContainerName,
		RelatedImageEnvVar:  consts.RelatedImageAIGatewayDataPlaneEnvVar,
		DefaultImage:        consts.DefaultAIGatewayDataPlaneImage,
		ManagedByLabelValue: consts.AIGatewayDataPlaneManagedByLabelValue,
		PodTemplateSpec: func(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *corev1.PodTemplateSpec {
			if aigwdp.Spec.Deployment == nil {
				return nil
			}
			return aigwdp.Spec.Deployment.PodTemplateSpec
		},
		DeploymentLabels: func(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) map[string]string {
			if aigwdp.Spec.Deployment == nil {
				return nil
			}
			return aigwdp.Spec.Deployment.Labels
		},
		DeploymentAnnotations: func(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) map[string]string {
			if aigwdp.Spec.Deployment == nil {
				return nil
			}
			return aigwdp.Spec.Deployment.Annotations
		},
		Replicas:       testReplicas,
		BuildContainer: buildTestContainer,
		LabelManaged:   k8sresources.LabelObjectAsAIGatewayDataPlaneManaged,
	},

	Service: ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]{
		Description:         "Ingress",
		NameSuffix:          "-ingress",
		DefaultPortName:     "ingress",
		DefaultPort:         testDefaultIngressPort,
		ManagedByLabelValue: consts.AIGatewayDataPlaneManagedByLabelValue,
		Options:             testServiceOptions,

		SetStatusAddresses: testSetStatusAddresses,
	},

	HPAScalingSpec:    testHPAScalingSpec,
	SetStatusReplicas: testSetStatusReplicas,
}

func testReplicas(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *int32 {
	if aigwdp.Spec.Deployment == nil {
		return nil
	}
	var hs *aigatewayv1alpha1.HorizontalScaling
	if aigwdp.Spec.Deployment.Scaling != nil {
		hs = aigwdp.Spec.Deployment.Scaling.HorizontalScaling
	}
	switch {
	case hs == nil:
		return aigwdp.Spec.Deployment.Replicas
	case hs.MinReplicas != nil:
		return hs.MinReplicas
	}
	return nil
}

func testHPAScalingSpec(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *k8sresources.HPAScalingSpec {
	if aigwdp.Spec.Deployment == nil ||
		aigwdp.Spec.Deployment.Scaling == nil ||
		aigwdp.Spec.Deployment.Scaling.HorizontalScaling == nil {
		return nil
	}
	hs := aigwdp.Spec.Deployment.Scaling.HorizontalScaling
	return &k8sresources.HPAScalingSpec{
		MinReplicas: hs.MinReplicas,
		MaxReplicas: hs.MaxReplicas,
		Metrics:     hs.Metrics,
		Behavior:    hs.Behavior,
	}
}

func testSetStatusReplicas(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, replicas, readyReplicas int32) {
	aigwdp.Status.Replicas = replicas
	aigwdp.Status.ReadyReplicas = readyReplicas
}

func testSetStatusAddresses(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, svcAddrs []operatorv1beta1.Address) {
	addrs := make([]aigatewayv1alpha1.Address, len(svcAddrs))
	for i, a := range svcAddrs {
		addrs[i] = aigatewayv1alpha1.Address{
			Value:      a.Value,
			SourceType: aigatewayv1alpha1.AddressSourceType(a.SourceType),
		}
		if a.Type != nil {
			t := aigatewayv1alpha1.AddressType(*a.Type)
			addrs[i].Type = &t
		}
	}
	aigwdp.Status.Addresses = addrs
}

// testServiceOptions converts the AIGatewayDataPlane ingress ServiceOptions
// into the shared representation.
func testServiceOptions(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *ServiceOptions {
	if aigwdp.Spec.Network == nil || aigwdp.Spec.Network.Services == nil || aigwdp.Spec.Network.Services.Ingress == nil {
		return nil
	}
	ingress := aigwdp.Spec.Network.Services.Ingress

	ports := make([]ServicePort, 0, len(ingress.Ports))
	for _, p := range ingress.Ports {
		ports = append(ports, ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: p.TargetPort,
			NodePort:   p.NodePort,
		})
	}

	labels := make(map[string]string, len(ingress.Labels))
	for k, v := range ingress.Labels {
		labels[string(k)] = string(v)
	}

	return &ServiceOptions{
		Type:                  ingress.Type,
		Annotations:           ingress.Annotations,
		Labels:                labels,
		ExternalTrafficPolicy: ingress.ExternalTrafficPolicy,
		TrafficDistribution:   ingress.TrafficDistribution,
		InternalTrafficPolicy: ingress.InternalTrafficPolicy,
		Ports:                 ports,
	}
}

// buildTestContainer is a stub container builder that produces a minimal
// hardened container for the AI Gateway data plane. It mirrors the production
// builder's contract of failing when the control plane has no endpoints.
func buildTestContainer(
	_ *aigatewayv1alpha1.AIGatewayDataPlane,
	cp ResolvedControlPlane,
	image string,
	_ string, // certSecretName
	_ string, // adminCertSecretName
) (corev1.Container, []corev1.Volume, error) {
	aigatewaycp, _ := cp.Object.(*konnectv1alpha1.KonnectAIGateway)
	if aigatewaycp != nil && aigatewaycp.Status.Endpoints == nil {
		return corev1.Container{}, nil, fmt.Errorf("KonnectAIGateway %q has no endpoints in status", aigatewaycp.Name)
	}
	container := corev1.Container{
		Name:  consts.AIGatewayDataPlaneContainerName,
		Image: image,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      KonnectCertVolumeName,
				MountPath: KonnectCertMountPath,
				ReadOnly:  true,
			},
		},
		ReadinessProbe: k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint),
	}
	container, volumes := k8sresources.HardenContainerWithSecurityContext(container, k8sresources.DataPlaneTypeAIGateway)
	return container, volumes, nil
}

// certEntityName derives the certificate CR name from the DataPlane name and
// the certificate content checksum: rotating the certificate produces a new CR
// name instead of mutating the previous one in place. Mirrors the wrapper
// implementations under test.
func certEntityName(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, certChecksum string) string {
	const checksumPrefixLen = 10
	suffix := certChecksum
	if len(suffix) > checksumPrefixLen {
		suffix = suffix[:checksumPrefixLen]
	}
	name := aigwdp.Name
	if maxNameLen := 253 - 1 - checksumPrefixLen; len(name) > maxNameLen {
		name = name[:maxNameLen]
	}
	return fmt.Sprintf("%s-%s", name, suffix)
}

// certificateChecksum hashes the certificate Secret's tls.crt/tls.key pair.
func certificateChecksum(secret *corev1.Secret) string {
	h := sha256.New()
	h.Write(secret.Data[corev1.TLSCertKey])
	h.Write(secret.Data[corev1.TLSPrivateKeyKey])
	return hex.EncodeToString(h.Sum(nil))
}

// buildTestCertificate builds the desired AIGatewayDataPlaneCertificate for
// the given AIGatewayDataPlane.
func buildTestCertificate(
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	cp ResolvedControlPlane,
	certSecretName string,
	certChecksum string,
) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	aigatewaycp, ok := cp.Object.(*konnectv1alpha1.KonnectAIGateway)
	if !ok {
		panic("buildTestCertificate expects a resolved KonnectAIGateway control plane")
	}
	name := certEntityName(aigwdp, certChecksum)
	return &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
		Kind:       "AIGatewayDataPlaneCertificate",
		Name:       name,
		Namespace:  aigwdp.Namespace,
		Labels: map[string]string{
			consts.GatewayOperatorManagedByLabel:          consts.AIGatewayDataPlaneManagedByLabelValue,
			consts.GatewayOperatorManagedByNameLabel:      aigwdp.Name,
			consts.GatewayOperatorManagedByNamespaceLabel: aigwdp.Namespace,
		},
		Spec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateSpec{
			AIGatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: aigatewaycp.Name,
				},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateAPISpec{
				Cert: aiconfigurationv1alpha1.SensitiveDataSource{
					Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
						Name: certSecretName,
						Key:  corev1.TLSCertKey,
					},
				},
				Title: name,
			},
		},
	}
}

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

// makeAIGWDP builds an AIGatewayDataPlane with an explicit UID so that
// ListSecretsForOwner can match OwnerReferences by UID.
func makeAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		Namespace: testCASecretNamespace,
		Name:      testDPName,
		UID:       types.UID("aigwdp-uid"),
	}
}

// caSecret builds a Secret containing a self-signed RSA CA certificate.
func caSecret() *corev1.Secret {
	return certificate.MustGenerateCASecret(
		testCASecretNamespace,
		testCASecretName,
		"Kong Test CA",
	)
}

// testKonnectAIGateway returns a minimal KonnectAIGateway with the standard
// test Konnect Configuration/Telemetry endpoints.
func testKonnectAIGateway() *konnectv1alpha1.KonnectAIGateway {
	aigwcp := &konnectv1alpha1.KonnectAIGateway{}
	aigwcp.Status.Endpoints = &konnectv1alpha1.KonnectAIGatewayEndpoints{
		Configuration: "cp.example.com",
		Telemetry:     "tp.example.com",
	}
	return aigwcp
}

// findEnv finds an env-var by name in a slice, returning (value, found).
func findEnv(envs []corev1.EnvVar, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// mustEnv asserts an env-var exists and returns its value (first match).
func mustEnv(t *testing.T, envs []corev1.EnvVar, name string) string {
	t.Helper()
	v, ok := findEnv(envs, name)
	require.True(t, ok, "env var %q not found", name)
	return v
}

// newReconcileAIGWDP builds the standard AIGatewayDataPlane used across tests.
func newReconcileAIGWDP() *aigatewayv1alpha1.AIGatewayDataPlane {
	return &aigatewayv1alpha1.AIGatewayDataPlane{
		Namespace:  reconcileTestNS,
		Name:       reconcileTestDPName,
		UID:        types.UID("aigwdp-uid"),
		APIVersion: "aigateway.konghq.com/v1alpha1",
		Kind:       "AIGatewayDataPlane",
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			ControlPlaneRef: &aigatewayv1alpha1.ControlPlaneRef{
				Type: aigatewayv1alpha1.ControlPlaneRefTypeKonnectNamespacedRef,
				KonnectNamespacedRef: &aigatewayv1alpha1.NamespacedRef{
					Name: reconcileTestAIGWCPName,
				},
			},
		},
	}
}

// newTestReconciler builds a Reconciler wired to cl and recorder.
// The fake client is wrapped with an interceptor that populates TypeMeta on
// AIGatewayDataPlane objects after Get, because the fake client does not set it.
func newTestReconciler(cl client.WithWatch, recorder *events.FakeRecorder) *testReconciler {
	wrapped := interceptor.NewClient(cl, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			if aigwdp, ok := obj.(*aigatewayv1alpha1.AIGatewayDataPlane); ok {
				gvks, _, _ := c.Scheme().ObjectKinds(aigwdp)
				if len(gvks) > 0 {
					aigwdp.TypeMeta = metav1.TypeMeta{
						APIVersion: gvks[0].GroupVersion().String(),
						Kind:       gvks[0].Kind,
					}
				}
			}
			return nil
		},
	})
	return &testReconciler{
		Client:                   wrapped,
		TypeConverter:            managedfields.NewDeducedTypeConverter(),
		EventRecorder:            recorder,
		ClusterCASecretName:      testCASecretName,
		ClusterCASecretNamespace: testCASecretNamespace,
		CertTTL:                  consts.DefaultCertTTL,
		Config:                   testConfig,
	}
}

// deploymentConfigWithDefaultImage returns a copy of the test DeploymentConfig
// with the default image overridden.
func deploymentConfigWithDefaultImage(
	image string,
) DeploymentConfig[*aigatewayv1alpha1.AIGatewayDataPlane] {
	cfg := testConfig.Deployment
	cfg.DefaultImage = image
	return cfg
}

// infoCountSink is a minimal logr.LogSink that counts Info() calls.
type infoCountSink struct{ count *int }

func (s infoCountSink) Init(logr.RuntimeInfo)             {}
func (s infoCountSink) Enabled(int) bool                  { return true }
func (s infoCountSink) Info(_ int, _ string, _ ...any)    { *s.count++ }
func (s infoCountSink) Error(_ error, _ string, _ ...any) {}
func (s infoCountSink) WithValues(_ ...any) logr.LogSink  { return s }
func (s infoCountSink) WithName(_ string) logr.LogSink    { return s }

// -----------------------------------------------------------------
// validateConfig
// -----------------------------------------------------------------

// validAdminAPI returns a fully populated, valid AdminAPIConfig.
func validAdminAPI() *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane] {
	return &AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]{
		Enabled:             func(*aigatewayv1alpha1.AIGatewayDataPlane, ResolvedControlPlane) bool { return true },
		ServiceNameSuffix:   "-admin",
		ServicePortName:     "admin",
		ServicePort:         8444,
		ManagedByLabelValue: consts.AIGatewayDataPlaneManagedByLabelValue,
		CertificateLabelKey: consts.SecretAIGatewayDataPlaneAdminCertificateLabel,
	}
}

func Test_validateConfig(t *testing.T) {
	// validService returns the primary Service config, fully valid.
	validService := func() ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane] {
		return ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]{
			Description:        "Ingress",
			SetStatusAddresses: testSetStatusAddresses,
		}
	}
	// serviceWithout returns the valid primary Service config with the given
	// modification applied to it.
	serviceWithout := func(mod func(*ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane])) ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane] {
		s := validService()
		mod(&s)
		return s
	}
	// adminAPIWith returns the valid AdminAPI config with the given
	// modification applied to it.
	adminAPIWith := func(mod func(*AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane])) *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane] {
		a := validAdminAPI()
		mod(a)
		return a
	}

	tests := []struct {
		name     string
		service  ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]
		adminAPI *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]
		wantErr  bool
	}{
		{
			name:    "primary Service with SetStatusAddresses: valid",
			service: validService(),
		},
		{
			name: "primary Service without SetStatusAddresses: invalid (the DataPlane status would never be populated)",
			service: serviceWithout(func(s *ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				s.SetStatusAddresses = nil
			}),
			wantErr: true,
		},
		{
			name: "primary Service with Enabled: invalid (the status-feeding Service must never become deletable)",
			service: serviceWithout(func(s *ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				s.Enabled = func(*aigatewayv1alpha1.AIGatewayDataPlane, ResolvedControlPlane) bool { return true }
			}),
			wantErr: true,
		},
		{
			name:     "fully populated AdminAPI: valid",
			service:  validService(),
			adminAPI: validAdminAPI(),
		},
		{
			name:    "AdminAPI with nil Enabled: invalid",
			service: validService(),
			adminAPI: adminAPIWith(func(a *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				a.Enabled = nil
			}),
			wantErr: true,
		},
		{
			name:    "AdminAPI with empty ServiceNameSuffix: invalid",
			service: validService(),
			adminAPI: adminAPIWith(func(a *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				a.ServiceNameSuffix = ""
			}),
			wantErr: true,
		},
		{
			name:    "AdminAPI with non-positive ServicePort: invalid",
			service: validService(),
			adminAPI: adminAPIWith(func(a *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				a.ServicePort = 0
			}),
			wantErr: true,
		},
		{
			name:    "AdminAPI with empty CertificateLabelKey: invalid",
			service: validService(),
			adminAPI: adminAPIWith(func(a *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				a.CertificateLabelKey = ""
			}),
			wantErr: true,
		},
		{
			name:    "AdminAPI CertificateLabelKey equal to Certificate.LabelKey: invalid",
			service: validService(),
			adminAPI: adminAPIWith(func(a *AdminAPIConfig[*aigatewayv1alpha1.AIGatewayDataPlane]) {
				a.CertificateLabelKey = consts.SecretAIGatewayDataPlaneCertificateLabel
			}),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &testReconciler{
				Config: func() Config[*aigatewayv1alpha1.AIGatewayDataPlane, *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate] {
					cfg := testConfig
					cfg.Service = tc.service
					cfg.AdminAPI = tc.adminAPI
					return cfg
				}(),
			}
			err := r.validateConfig()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
