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
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// config wires the KegDataPlane specific behavior into the shared generic
// DataPlane reconciler.
var config = shareddataplane.Config[
	*eventgatewayv1alpha1.KegDataPlane,
	*konnectv1alpha1.KonnectEventGateway,
	*configurationv1alpha1.EventGatewayDataPlaneCertificate,
]{
	ControllerName: ControllerName,
	Kind:           "KegDataPlane",

	NewObject: func() *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{}
	},
	NewControlPlaneObject: func() *konnectv1alpha1.KonnectEventGateway {
		return &konnectv1alpha1.KonnectEventGateway{}
	},
	NewCertificateObject: func() *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		return &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
	},
	NewObjectList: func() client.ObjectList {
		return &eventgatewayv1alpha1.KegDataPlaneList{}
	},

	ControlPlaneRefName: func(egdp *eventgatewayv1alpha1.KegDataPlane) string {
		return egdp.Spec.ControlPlaneRef.KonnectNamespacedRef.Name
	},
	ControlPlaneKind:          "KonnectEventGateway",
	ControlPlaneRefIndexField: index.IndexFieldKegDataPlaneOnKonnectEventGateway,

	Conditions: shareddataplane.Conditions{
		ReadyType:                    string(eventgatewayv1alpha1.ReadyType),
		ResourceReadyReason:          string(eventgatewayv1alpha1.ResourceReadyReason),
		DependenciesNotReadyReason:   string(eventgatewayv1alpha1.DependenciesNotReadyReason),
		DependenciesNotReadyMessage:  eventgatewayv1alpha1.DependenciesNotReadyMessage,
		WaitingToBecomeReadyReason:   string(eventgatewayv1alpha1.WaitingToBecomeReadyReason),
		WaitingToBecomeReadyMessage:  eventgatewayv1alpha1.WaitingToBecomeReadyMessage,
		UnableToProvisionReason:      string(eventgatewayv1alpha1.UnableToProvisionReason),
		CertificateProvisionedType:   string(eventgatewayv1alpha1.CertificateProvisionedType),
		CertificateProvisionedReason: string(eventgatewayv1alpha1.CertificateProvisionedReason),

		ControlPlaneResolvedType:         string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType),
		ControlPlaneResolvedReason:       string(eventgatewayv1alpha1.KonnectEventGatewayResolvedReason),
		ControlPlaneResolvedMessage:      eventgatewayv1alpha1.KonnectEventGatewayResolvedMessage,
		ControlPlaneNotFoundReason:       string(eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason),
		ControlPlaneNotFoundMessage:      eventgatewayv1alpha1.KonnectEventGatewayNotFoundMessage,
		ControlPlaneNotProgrammedReason:  string(eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason),
		ControlPlaneNotProgrammedMessage: eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedMessage,

		KonnectCertificateRegisteredType:           string(eventgatewayv1alpha1.KonnectCertificateRegisteredType),
		KonnectCertificateRegisteredReason:         string(eventgatewayv1alpha1.KonnectCertificateRegisteredReason),
		KonnectCertificateRegistrationFailedReason: string(eventgatewayv1alpha1.KonnectCertificateRegistrationFailedReason),
		KonnectCertificateNotProgrammedReason:      string(eventgatewayv1alpha1.KonnectCertificateNotProgrammedReason),

		ServiceReadyType:         string(eventgatewayv1alpha1.ServiceReadyType),
		ServiceReadyReason:       string(eventgatewayv1alpha1.ServiceReadyReason),
		ServiceReadyMessage:      eventgatewayv1alpha1.ServiceReadyMessage,
		WaitingForAddressReason:  string(eventgatewayv1alpha1.WaitingForAddressReason),
		WaitingForAddressMessage: eventgatewayv1alpha1.WaitingForAddressMessage,
	},

	CertificateLabelKey: consts.SecretKEGDataPlaneCertificateLabel,
	CertificateKind:     "EventGatewayDataPlaneCertificate",
	BuildCertificate:    buildEventGatewayDataPlaneCertificate,
	EnsureCertificate:   secrets.EnsureCertificate[*eventgatewayv1alpha1.KegDataPlane],

	Deployment: shareddataplane.DeploymentConfig[
		*eventgatewayv1alpha1.KegDataPlane,
		*konnectv1alpha1.KonnectEventGateway,
	]{
		ContainerName:       consts.KEGContainerName,
		RelatedImageEnvVar:  consts.RelatedImageKEGEnvVar,
		DefaultImage:        consts.DefaultKEGImage,
		ManagedByLabelValue: consts.DataPlaneManagedByLabelValue,
		PodTemplateSpec: func(egdp *eventgatewayv1alpha1.KegDataPlane) *corev1.PodTemplateSpec {
			if egdp.Spec.Deployment == nil {
				return nil
			}
			return egdp.Spec.Deployment.PodTemplateSpec
		},
		DeploymentLabels: func(egdp *eventgatewayv1alpha1.KegDataPlane) map[string]string {
			if egdp.Spec.Deployment == nil {
				return nil
			}
			return egdp.Spec.Deployment.Labels
		},
		DeploymentAnnotations: func(egdp *eventgatewayv1alpha1.KegDataPlane) map[string]string {
			if egdp.Spec.Deployment == nil {
				return nil
			}
			return egdp.Spec.Deployment.Annotations
		},
		Replicas:       replicas,
		BuildContainer: buildContainer,
	},

	Service: shareddataplane.ServiceConfig[*eventgatewayv1alpha1.KegDataPlane]{
		Description:         "Kafka",
		NameSuffix:          "-kafka",
		DefaultPortName:     "kafka",
		DefaultPort:         DefaultKafkaPort,
		ManagedByLabelValue: consts.DataPlaneManagedByLabelValue,
		Options:             serviceOptions,
	},

	HPAScalingSpec:     hpaScalingSpec,
	SetStatusReplicas:  setStatusReplicas,
	SetStatusAddresses: setStatusAddresses,
}

// replicas returns the replica count to seed on the Deployment: the static
// replica count, or the HPA minReplicas when horizontal scaling is configured
// so the Deployment scales up immediately before the HPA's first evaluation.
func replicas(egdp *eventgatewayv1alpha1.KegDataPlane) *int32 {
	if egdp.Spec.Deployment == nil {
		return nil
	}
	var hs *eventgatewayv1alpha1.HorizontalScaling
	if egdp.Spec.Deployment.Scaling != nil {
		hs = egdp.Spec.Deployment.Scaling.HorizontalScaling
	}
	switch {
	case hs == nil:
		return egdp.Spec.Deployment.Replicas
	case hs.MinReplicas != nil:
		return hs.MinReplicas
	}
	return nil
}

// hpaScalingSpec returns the HPA scaling spec, or nil when horizontal scaling
// is not configured.
func hpaScalingSpec(egdp *eventgatewayv1alpha1.KegDataPlane) *k8sresources.HPAScalingSpec {
	if egdp.Spec.Deployment == nil ||
		egdp.Spec.Deployment.Scaling == nil ||
		egdp.Spec.Deployment.Scaling.HorizontalScaling == nil {
		return nil
	}
	hs := egdp.Spec.Deployment.Scaling.HorizontalScaling
	return &k8sresources.HPAScalingSpec{
		MinReplicas: hs.MinReplicas,
		MaxReplicas: hs.MaxReplicas,
		Metrics:     hs.Metrics,
		Behavior:    hs.Behavior,
	}
}

func setStatusReplicas(egdp *eventgatewayv1alpha1.KegDataPlane, replicas, readyReplicas int32) {
	egdp.Status.Replicas = replicas
	egdp.Status.ReadyReplicas = readyReplicas
}

func setStatusAddresses(egdp *eventgatewayv1alpha1.KegDataPlane, svcAddrs []operatorv1beta1.Address) {
	addrs := make([]eventgatewayv1alpha1.Address, len(svcAddrs))
	for i, a := range svcAddrs {
		addrs[i] = eventgatewayv1alpha1.Address{
			Value:      a.Value,
			SourceType: eventgatewayv1alpha1.AddressSourceType(a.SourceType),
		}
		if a.Type != nil {
			t := eventgatewayv1alpha1.AddressType(*a.Type)
			addrs[i].Type = &t
		}
	}
	egdp.Status.Addresses = addrs
}

// serviceOptions converts the user-provided Kafka ServiceOptions into the
// shared representation, or returns nil when not configured.
func serviceOptions(egdp *eventgatewayv1alpha1.KegDataPlane) *shareddataplane.ServiceOptions {
	if egdp.Spec.Network == nil || egdp.Spec.Network.Services == nil || egdp.Spec.Network.Services.Kafka == nil {
		return nil
	}
	kafka := egdp.Spec.Network.Services.Kafka

	ports := make([]shareddataplane.ServicePort, 0, len(kafka.Ports))
	for _, p := range kafka.Ports {
		ports = append(ports, shareddataplane.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: p.TargetPort,
			NodePort:   p.NodePort,
		})
	}

	labels := make(map[string]string, len(kafka.Labels))
	for k, v := range kafka.Labels {
		labels[string(k)] = string(v)
	}

	return &shareddataplane.ServiceOptions{
		Type:                  kafka.Type,
		Annotations:           kafka.Annotations,
		Labels:                labels,
		ExternalTrafficPolicy: kafka.ExternalTrafficPolicy,
		TrafficDistribution:   kafka.TrafficDistribution,
		InternalTrafficPolicy: kafka.InternalTrafficPolicy,
		Ports:                 ports,
	}
}

// buildContainer builds the keg container and the additional volumes it
// requires.
func buildContainer(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	image string,
) (corev1.Container, []corev1.Volume, error) {
	envVars, err := buildKEGEnvVars(egdp, keg)
	if err != nil {
		return corev1.Container{}, nil, err
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
	container, volumes := k8sresources.HardenContainerWithSecurityContext(container, k8sresources.DataPlaneTypeKeg)
	return container, volumes, nil
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

// buildEventGatewayDataPlaneCertificate builds the desired
// EventGatewayDataPlaneCertificate for the given KegDataPlane, referencing the
// provisioned mTLS Secret and the resolved KonnectEventGateway.
func buildEventGatewayDataPlaneCertificate(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	keg *konnectv1alpha1.KonnectEventGateway,
	certSecretName string,
) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
	return &configurationv1alpha1.EventGatewayDataPlaneCertificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: configurationv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayDataPlaneCertificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      egdp.Name,
			Namespace: egdp.Namespace,
		},
		Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: keg.Name,
				},
			},
			APISpec: configurationv1alpha1.EventGatewayDataPlaneCertificateAPISpec{
				Certificate: configurationv1alpha1.SensitiveDataSource{
					Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
						Name: certSecretName,
						Key:  corev1.TLSCertKey,
					},
				},
			},
		},
	}
}
