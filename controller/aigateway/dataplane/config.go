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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// config wires the AIGatewayDataPlane specific behavior into the shared
// generic DataPlane reconciler.
var config = shareddataplane.Config[
	*aigatewayv1alpha1.AIGatewayDataPlane,
	*konnectv1alpha1.KonnectAIGateway,
	*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
]{
	ControllerName: ControllerName,
	Kind:           "AIGatewayDataPlane",

	NewObject: func() *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{}
	},
	NewControlPlaneObject: func() *konnectv1alpha1.KonnectAIGateway {
		return &konnectv1alpha1.KonnectAIGateway{}
	},
	NewCertificateObject: func() *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
		return &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{}
	},
	NewObjectList: func() client.ObjectList {
		return &aigatewayv1alpha1.AIGatewayDataPlaneList{}
	},

	ControlPlaneRefName: func(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) string {
		if aigwdp.Spec.ControlPlaneRef == nil || aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef == nil {
			return ""
		}
		return aigwdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name
	},
	ControlPlaneKind:          "KonnectAIGateway",
	ControlPlaneRefIndexField: index.IndexFieldAIGatewayDataPlaneOnKonnectAIGateway,

	Conditions: shareddataplane.Conditions{
		ReadyType:                    string(aigatewayv1alpha1.ReadyType),
		ResourceReadyReason:          string(aigatewayv1alpha1.ResourceReadyReason),
		DependenciesNotReadyReason:   string(aigatewayv1alpha1.DependenciesNotReadyReason),
		DependenciesNotReadyMessage:  aigatewayv1alpha1.DependenciesNotReadyMessage,
		WaitingToBecomeReadyReason:   string(aigatewayv1alpha1.WaitingToBecomeReadyReason),
		WaitingToBecomeReadyMessage:  aigatewayv1alpha1.WaitingToBecomeReadyMessage,
		UnableToProvisionReason:      string(aigatewayv1alpha1.UnableToProvisionReason),
		CertificateProvisionedType:   string(aigatewayv1alpha1.CertificateProvisionedType),
		CertificateProvisionedReason: string(aigatewayv1alpha1.CertificateProvisionedReason),

		ControlPlaneResolvedType:         string(aigatewayv1alpha1.KonnectAIGatewayResolvedType),
		ControlPlaneResolvedReason:       string(aigatewayv1alpha1.KonnectAIGatewayResolvedReason),
		ControlPlaneResolvedMessage:      aigatewayv1alpha1.KonnectAIGatewayResolvedMessage,
		ControlPlaneNotFoundReason:       string(aigatewayv1alpha1.KonnectAIGatewayNotFoundReason),
		ControlPlaneNotFoundMessage:      aigatewayv1alpha1.KonnectAIGatewayNotFoundMessage,
		ControlPlaneNotProgrammedReason:  string(aigatewayv1alpha1.KonnectAIGatewayNotProgrammedReason),
		ControlPlaneNotProgrammedMessage: aigatewayv1alpha1.KonnectAIGatewayNotProgrammedMessage,

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

	CertificateLabelKey: consts.SecretAIGatewayDataPlaneCertificateLabel,
	CertificateKind:     "AIGatewayDataPlaneCertificate",
	BuildCertificate:    buildAIGatewayDataPlaneCertificate,
	EnsureCertificate:   secrets.EnsureCertificate[*aigatewayv1alpha1.AIGatewayDataPlane],

	Deployment: shareddataplane.DeploymentConfig[
		*aigatewayv1alpha1.AIGatewayDataPlane,
		*konnectv1alpha1.KonnectAIGateway,
	]{
		ContainerName:         consts.AIGatewayDataPlaneContainerName,
		RelatedImageEnvVar:    consts.RelatedImageAIGatewayDataPlaneEnvVar,
		DefaultImage:          consts.DefaultAIGatewayDataPlaneImage,
		ManagedByLabelValue:   consts.AIGatewayDataPlaneManagedByLabelValue,
		PodTemplateSpec:       podTemplateSpec,
		DeploymentLabels:      deploymentLabels,
		DeploymentAnnotations: deploymentAnnotations,
		Replicas:              replicas,
		BuildContainer:        buildContainer,
		LabelManaged:          k8sresources.LabelObjectAsAIGatewayDataPlaneManaged,
	},

	Service: shareddataplane.ServiceConfig[*aigatewayv1alpha1.AIGatewayDataPlane]{
		Description:         "Ingress",
		NameSuffix:          "-ingress",
		DefaultPortName:     "ingress",
		DefaultPort:         DefaultIngressPort,
		ManagedByLabelValue: consts.AIGatewayDataPlaneManagedByLabelValue,
		Options:             serviceOptions,
	},

	HPAScalingSpec:     hpaScalingSpec,
	SetStatusReplicas:  setStatusReplicas,
	SetStatusAddresses: setStatusAddresses,
}

func podTemplateSpec(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *corev1.PodTemplateSpec {
	if aigwdp.Spec.Deployment == nil {
		return nil
	}
	return aigwdp.Spec.Deployment.PodTemplateSpec
}

func deploymentLabels(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) map[string]string {
	if aigwdp.Spec.Deployment == nil {
		return nil
	}
	return aigwdp.Spec.Deployment.Labels
}

func deploymentAnnotations(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) map[string]string {
	if aigwdp.Spec.Deployment == nil {
		return nil
	}
	return aigwdp.Spec.Deployment.Annotations
}

// replicas returns the replica count to seed on the Deployment: the static
// replica count, or the HPA minReplicas when horizontal scaling is configured
// so the Deployment scales up immediately before the HPA's first evaluation.
func replicas(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *int32 {
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

// hpaScalingSpec returns the HPA scaling spec, or nil when horizontal scaling
// is not configured.
func hpaScalingSpec(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *k8sresources.HPAScalingSpec {
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

func setStatusReplicas(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, replicas, readyReplicas int32) {
	aigwdp.Status.Replicas = replicas
	aigwdp.Status.ReadyReplicas = readyReplicas
}

func setStatusAddresses(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, svcAddrs []operatorv1beta1.Address) {
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

// serviceOptions converts the user-provided ingress ServiceOptions into the
// shared representation, or returns nil when not configured.
func serviceOptions(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *shareddataplane.ServiceOptions {
	if aigwdp.Spec.Network == nil || aigwdp.Spec.Network.Services == nil || aigwdp.Spec.Network.Services.Ingress == nil {
		return nil
	}
	ingress := aigwdp.Spec.Network.Services.Ingress

	ports := make([]shareddataplane.ServicePort, 0, len(ingress.Ports))
	for _, p := range ingress.Ports {
		ports = append(ports, shareddataplane.ServicePort{
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

	return &shareddataplane.ServiceOptions{
		Type:                  ingress.Type,
		Annotations:           ingress.Annotations,
		Labels:                labels,
		ExternalTrafficPolicy: ingress.ExternalTrafficPolicy,
		TrafficDistribution:   ingress.TrafficDistribution,
		InternalTrafficPolicy: ingress.InternalTrafficPolicy,
		Ports:                 ports,
	}
}

// buildContainer builds the AI Gateway container and the additional volumes it
// requires. cp is nil when the AIGatewayDataPlane has no ControlPlaneRef
// configured.
func buildContainer(
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
	image string,
) (corev1.Container, []corev1.Volume, error) {
	envVars, err := buildAIGatewayEnvVars(aigatewaycp)
	if err != nil {
		return corev1.Container{}, nil, err
	}

	container := corev1.Container{
		Name:  consts.AIGatewayDataPlaneContainerName,
		Image: image,
		Env:   envVars,
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

// buildAIGatewayEnvVars builds the AI Gateway environment variables
// from required hardcoded values and KonnectAIGateway (controlplane) status.
// aigatewaycp is nil when the AIGatewayDataPlane has no ControlPlaneRef
// configured; in that case the Konnect-endpoint env vars are omitted and the
// user is expected to supply them manually (e.g. via PodTemplateSpec).
func buildAIGatewayEnvVars(
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
) ([]corev1.EnvVar, error) {
	envVars := append(
		RequiredHardcodedEnvVars(),
		corev1.EnvVar{Name: EnvClientCertPath, Value: KonnectCertMountPath + "tls.crt"},
		corev1.EnvVar{Name: EnvKonnectClientCertKey, Value: KonnectCertMountPath + "tls.key"},
	)

	if aigatewaycp == nil {
		return envVars, nil
	}

	if aigatewaycp.Status.Endpoints == nil {
		return nil, fmt.Errorf("KonnectAIGateway %q has no endpoints in status", aigatewaycp.Name)
	}

	cpHost := aigatewaycp.Status.Endpoints.Configuration
	tpHost := aigatewaycp.Status.Endpoints.Telemetry

	return append(
		envVars,
		corev1.EnvVar{Name: EnvKongClusterControlPlane, Value: cpHost + ":443"},
		corev1.EnvVar{Name: EnvKongClusterServerName, Value: cpHost},
		corev1.EnvVar{Name: EnvKongClusterTelemetryEndpoint, Value: tpHost + ":443"},
		corev1.EnvVar{Name: EnvKongClusterTelemetryServerName, Value: tpHost},
	), nil
}

// buildAIGatewayDataPlaneCertificate builds the desired
// AIGatewayDataPlaneCertificate for the given AIGatewayDataPlane, referencing
// the provisioned mTLS Secret and the resolved KonnectAIGateway.
func buildAIGatewayDataPlaneCertificate(
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	aigatewaycp *konnectv1alpha1.KonnectAIGateway,
	certSecretName string,
) *aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate {
	return &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: aiconfigurationv1alpha1.GroupVersion.String(),
			Kind:       "AIGatewayDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigwdp.Name,
			Namespace: aigwdp.Namespace,
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
				Title: aigwdp.Name,
			},
		},
	}
}
