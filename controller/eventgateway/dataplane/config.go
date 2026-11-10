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
	"slices"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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
	"github.com/kong/kong-operator/v2/internal/utils/konnectendpoint"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// konnectEventGatewayKind wires the KonnectEventGateway control plane kind
// into the shared generic DataPlane reconciler.
var konnectEventGatewayKind = shareddataplane.ControlPlaneKindConfig{
	Kind:                      "KonnectEventGateway",
	NewObject:                 func() shareddataplane.ControlPlaneObject { return &konnectv1alpha1.KonnectEventGateway{} },
	ControlPlaneRefIndexField: index.IndexFieldKegDataPlaneOnKonnectEventGateway,
	IsKonnect:                 true,
	EndpointCheck:             checkKonnectEventGatewayEndpoints,
	Conditions: shareddataplane.ControlPlaneConditions{
		ResolvedType:           string(eventgatewayv1alpha1.KonnectEventGatewayResolvedType),
		ResolvedReason:         string(eventgatewayv1alpha1.KonnectEventGatewayResolvedReason),
		ResolvedMessage:        eventgatewayv1alpha1.KonnectEventGatewayResolvedMessage,
		NotFoundReason:         string(eventgatewayv1alpha1.KonnectEventGatewayNotFoundReason),
		NotFoundMessage:        eventgatewayv1alpha1.KonnectEventGatewayNotFoundMessage,
		NotProgrammedReason:    string(eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedReason),
		NotProgrammedMessage:   eventgatewayv1alpha1.KonnectEventGatewayNotProgrammedMessage,
		EndpointMismatchReason: string(eventgatewayv1alpha1.KonnectEventGatewayEndpointMismatchReason),
	},
}

// config wires the KegDataPlane specific behavior into the shared generic
// DataPlane reconciler.
var config = shareddataplane.Config[
	*eventgatewayv1alpha1.KegDataPlane,
	*configurationv1alpha1.EventGatewayDataPlaneCertificate,
]{
	ControllerName: ControllerName,
	Kind:           "KegDataPlane",

	NewObject: func() *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{}
	},
	NewCertificateObject: func() *configurationv1alpha1.EventGatewayDataPlaneCertificate {
		return &configurationv1alpha1.EventGatewayDataPlaneCertificate{}
	},
	NewObjectList: func() client.ObjectList {
		return &eventgatewayv1alpha1.KegDataPlaneList{}
	},

	ControlPlaneRef: func(egdp *eventgatewayv1alpha1.KegDataPlane) shareddataplane.ControlPlaneRef {
		if ref := egdp.Spec.ControlPlaneRef.OnPremNamespacedRef; ref != nil {
			return shareddataplane.ControlPlaneRef{
				Kind: onPremEventGatewayKind.Kind,
				Name: ref.Name,
			}
		}
		ref := egdp.Spec.ControlPlaneRef.KonnectNamespacedRef
		if ref == nil {
			// The CRD schema requires a reference of the type, so this
			// shouldn't happen. Returning an empty ref skips control plane
			// resolution; buildContainer then refuses to build a Deployment
			// without a resolved control plane, instead of panicking.
			return shareddataplane.ControlPlaneRef{}
		}
		return shareddataplane.ControlPlaneRef{
			Kind: konnectEventGatewayKind.Kind,
			Name: ref.Name,
		}
	},
	// The OnPremEventGateway kind is added per reconciler (see
	// Reconciler.config): its Dependencies need the operator namespace.
	ControlPlanes: []shareddataplane.ControlPlaneKindConfig{konnectEventGatewayKind},

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

	Certificate: shareddataplane.CertificateConfig[
		*eventgatewayv1alpha1.KegDataPlane,
		*configurationv1alpha1.EventGatewayDataPlaneCertificate,
	]{
		LabelKey:        consts.SecretKEGDataPlaneCertificateLabel,
		Kind:            "EventGatewayDataPlaneCertificate",
		Build:           buildEventGatewayDataPlaneCertificate,
		AdaptToExisting: keepEventGatewayDataPlaneCertificateRefField,
		Ensure:          secrets.EnsureCertificate[*eventgatewayv1alpha1.KegDataPlane],
		// Requested is left nil: KEG always needs a certificate to connect to
		// its control plane, so an unresolved one always blocks the
		// Deployment.
		// Resolve is set per reconciler (see Reconciler.config).
		Checksum:           shareddataplane.CertificateChecksum,
		ChecksumAnnotation: consts.KEGDataPlaneCertificateChecksumAnnotation,
		// CleanupStale and RequeueAfter are set per reconciler (see
		// Reconciler.config): they depend on its certificate settings.
	},
	// ExtraWatches is set per reconciler (see Reconciler.config): the
	// watches of the OnPremEventGateway kind depend on whether it is enabled.

	Deployment: shareddataplane.DeploymentConfig[*eventgatewayv1alpha1.KegDataPlane]{
		ContainerName:         consts.KEGContainerName,
		RelatedImageEnvVar:    consts.RelatedImageKEGEnvVar,
		DefaultImage:          consts.DefaultKEGImage,
		ManagedByLabelValue:   consts.DataPlaneManagedByLabelValue,
		PodTemplateSpec:       podTemplateSpec,
		DeploymentLabels:      deploymentLabels,
		DeploymentAnnotations: deploymentAnnotations,
		BuildContainer:        buildContainer,
		Customize:             customizeDeployment,
		ControlPlaneGone:      releaseDeploymentFromGWAdmin,

		TerminationGracePeriodSeconds: terminationGracePeriodSeconds,
	},

	Service: shareddataplane.ServiceConfig[*eventgatewayv1alpha1.KegDataPlane]{
		Description:         "Kafka",
		NameSuffix:          "-kafka",
		DefaultPortName:     "kafka",
		DefaultPort:         DefaultKafkaPort,
		ManagedByLabelValue: consts.DataPlaneManagedByLabelValue,
		Options:             serviceOptions,

		SetStatusAddresses: setStatusAddresses,
	},

	SetStatusReplicas: setStatusReplicas,
}

func podTemplateSpec(egdp *eventgatewayv1alpha1.KegDataPlane) *corev1.PodTemplateSpec {
	if egdp.Spec.Deployment == nil || egdp.Spec.Deployment.PodTemplateSpec == nil {
		return nil
	}
	overlay := egdp.Spec.Deployment.PodTemplateSpec
	_, clientCA := overlay.Annotations[consts.GWAdminDataPlaneClientCAAnnotation]
	_, serverBundle := overlay.Annotations[consts.GWAdminDataPlaneServerBundleAnnotation]
	if !clientCA && !serverBundle {
		return overlay
	}
	// The overlay wins on annotation conflicts: pinning those the rotations of
	// a GWAdmin rely on would make them drop a certificate authority pods
	// still use, or wait forever (see customizeDeployment).
	overlay = overlay.DeepCopy()
	delete(overlay.Annotations, consts.GWAdminDataPlaneClientCAAnnotation)
	delete(overlay.Annotations, consts.GWAdminDataPlaneServerBundleAnnotation)
	return overlay
}

// terminationGracePeriodSeconds returns the termination grace period of the
// keg pods: long enough for keg to drain its connections and shut down, plus
// a margin. Each timeout is the one keg runs with: the env var of the keg
// container in the PodTemplateSpec, which wins over the operator's, else
// spec.config.runtime, else keg's default. It returns nil, keeping the
// Kubernetes default, when the default is long enough: a data plane that
// doesn't raise the timeouts isn't rolled out for it.
func terminationGracePeriodSeconds(egdp *eventgatewayv1alpha1.KegDataPlane) *int64 {
	var specDrain, specShutdown *int32
	if egdp.Spec.Config != nil && egdp.Spec.Config.Runtime != nil {
		specDrain = egdp.Spec.Config.Runtime.DrainDurationSeconds
		specShutdown = egdp.Spec.Config.Runtime.ShutdownTimeoutSeconds
	}
	var kegContainer *corev1.Container
	if pts := podTemplateSpec(egdp); pts != nil {
		kegContainer = k8sutils.GetPodContainerByName(&pts.Spec, consts.KEGContainerName)
	}

	drain := runtimeTimeoutSeconds(kegContainer, EnvRuntimeDrainDuration, specDrain, DefaultDrainDurationSeconds)
	shutdown := runtimeTimeoutSeconds(kegContainer, EnvRuntimeShutdownTimeout, specShutdown, DefaultShutdownTimeoutSeconds)

	grace := drain + shutdown + TerminationGracePeriodMarginSeconds
	if grace <= DefaultTerminationGracePeriodSeconds {
		return nil
	}
	return &grace
}

// runtimeTimeoutSeconds returns, in whole seconds rounded up, the keg timeout
// set by the env var envName of the user keg container, else by the spec
// field, else def. An env var whose value can't be read here, from valueFrom
// or not a duration, is skipped.
func runtimeTimeoutSeconds(kegContainer *corev1.Container, envName string, spec *int32, def int64) int64 {
	if kegContainer != nil {
		if i := slices.IndexFunc(kegContainer.Env, func(e corev1.EnvVar) bool { return e.Name == envName }); i >= 0 {
			if e := kegContainer.Env[i]; e.ValueFrom == nil {
				if d, err := time.ParseDuration(e.Value); err == nil && d >= 0 {
					s := d / time.Second
					if d%time.Second != 0 {
						s++
					}
					return int64(s)
				}
			}
		}
	}
	if spec != nil {
		return int64(*spec)
	}
	return def
}

func deploymentLabels(egdp *eventgatewayv1alpha1.KegDataPlane) map[string]string {
	if egdp.Spec.Deployment == nil {
		return nil
	}
	return egdp.Spec.Deployment.Labels
}

func deploymentAnnotations(egdp *eventgatewayv1alpha1.KegDataPlane) map[string]string {
	if egdp.Spec.Deployment == nil {
		return nil
	}
	return egdp.Spec.Deployment.Annotations
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

// konnectEventGatewayFromResolved returns the KonnectEventGateway carried by
// the resolved control plane, or nil when the KegDataPlane has no control
// plane resolved or references another kind of control plane.
func konnectEventGatewayFromResolved(cp shareddataplane.ResolvedControlPlane) *konnectv1alpha1.KonnectEventGateway {
	keg, _ := cp.Object.(*konnectv1alpha1.KonnectEventGateway)
	return keg
}

// buildContainer builds the keg container and the additional volumes it
// requires.
func buildContainer(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	cp shareddataplane.ResolvedControlPlane,
	image string,
	_ string, // certSecretName: KEG always resolves a certificate Secret before the Deployment is built.
	_ string, // adminCertSecretName: KEG never provisions an admin certificate.
) (corev1.Container, []corev1.Volume, error) {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      KonnectCertVolumeName,
			MountPath: KonnectCertMountPath,
			ReadOnly:  true,
		},
	}
	var extraVolumes []corev1.Volume
	var envVars []corev1.EnvVar
	if deps := onPremDependenciesOf(cp); deps != nil {
		envVars = append(onPremEnvVars(deps), commonKEGEnvVars(egdp)...)
		volume, mount := onPremVolume(egdp, deps)
		extraVolumes = append(extraVolumes, volume)
		volumeMounts = append(volumeMounts, mount)
	} else {
		keg := konnectEventGatewayFromResolved(cp)
		if keg == nil {
			// keg needs the region and cluster ID of its control plane to
			// start, so there is no Deployment to build without one. The CRD
			// schema requires a control plane reference, so this shouldn't
			// happen.
			return corev1.Container{}, nil, fmt.Errorf("KegDataPlane %s/%s has no resolved control plane", egdp.Namespace, egdp.Name)
		}
		var err error
		envVars, err = buildKEGEnvVars(egdp, keg)
		if err != nil {
			return corev1.Container{}, nil, err
		}
	}

	healthPort := intstr.FromInt32(DefaultHealthPort)

	container := corev1.Container{
		Name:         consts.KEGContainerName,
		Image:        image,
		Env:          envVars,
		VolumeMounts: volumeMounts,
		ReadinessProbe: &corev1.Probe{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health/probes/readiness",
				Port: healthPort,
			},
		},
		LivenessProbe: &corev1.Probe{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health/probes/liveness",
				Port: healthPort,
			},
		},
	}
	container, volumes := k8sresources.HardenContainerWithSecurityContext(container, k8sresources.DataPlaneTypeKeg)
	return container, append(volumes, extraVolumes...), nil
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
	if cfg := egdp.Spec.Config; cfg != nil && cfg.Konnect != nil && cfg.Konnect.Domain != nil {
		domain = *cfg.Konnect.Domain
	}

	envVars := []corev1.EnvVar{
		{Name: EnvKonnectRegion, Value: region},
		{Name: EnvKonnectGatewayClusterID, Value: keg.Status.ID},
		{Name: EnvKonnectClientCertPath, Value: KonnectCertMountPath + "tls.crt"},
		{Name: EnvKonnectClientKeyPath, Value: KonnectCertMountPath + "tls.key"},
		{Name: EnvKonnectDomain, Value: domain},
	}
	return append(envVars, commonKEGEnvVars(egdp)...), nil
}

// checkKonnectEventGatewayEndpoints reports whether the data plane can reach the
// KonnectEventGateway: the Event Gateway builds the host itself, from the region
// and the domain, and cannot use a path. A domain without a path set on the
// KegDataPlane lifts the check for that reason, as issue #6107 asks.
func checkKonnectEventGatewayEndpoints(dp shareddataplane.Object, cp shareddataplane.ControlPlaneObject) error {
	keg, ok := cp.(*konnectv1alpha1.KonnectEventGateway)
	if !ok {
		return fmt.Errorf("unexpected control plane object %T", cp)
	}
	egdp, ok := dp.(*eventgatewayv1alpha1.KegDataPlane)
	if !ok {
		return fmt.Errorf("unexpected DataPlane object %T", dp)
	}
	cfg := egdp.Spec.Config
	if !konnectendpoint.CarriesPath(keg.Status.ServerURL) ||
		(cfg != nil && cfg.Konnect != nil && cfg.Konnect.Domain != nil &&
			*cfg.Konnect.Domain != "" && !konnectendpoint.CarriesPath(*cfg.Konnect.Domain)) {
		return nil
	}
	return fmt.Errorf(
		"cannot use the private server URL %q: the Event Gateway composes a public Konnect host from the region and the domain and cannot use a path; set spec.config.konnect.domain to a domain without a path to let it connect over the public internet",
		keg.Status.ServerURL,
	)
}

// commonKEGEnvVars builds the keg environment variables that don't depend on
// the kind of the control plane, from DataPlane spec.config.
func commonKEGEnvVars(egdp *eventgatewayv1alpha1.KegDataPlane) []corev1.EnvVar {
	healthAddr := fmt.Sprintf("0.0.0.0:%d", DefaultHealthPort)
	cfg := egdp.Spec.Config
	if cfg != nil && cfg.Runtime != nil && cfg.Runtime.HealthListenerAddressPort != nil {
		healthAddr = *cfg.Runtime.HealthListenerAddressPort
	}

	envVars := []corev1.EnvVar{
		// Bind the health endpoint to all interfaces so Kubernetes probes can reach it.
		{Name: EnvRuntimeHealthAddr, Value: healthAddr},
	}

	if cfg == nil {
		return envVars
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

	return envVars
}

// buildEventGatewayDataPlaneCertificate builds the desired
// EventGatewayDataPlaneCertificate for the given KegDataPlane, referencing the
// mTLS Secret and the resolved KonnectEventGateway. Its name is derived from
// certChecksum, so a certificate rotation registers a new entity rather than
// mutating the previous one in place.
func buildEventGatewayDataPlaneCertificate(
	egdp *eventgatewayv1alpha1.KegDataPlane,
	cp shareddataplane.ResolvedControlPlane,
	certSecretName string,
	certChecksum string,
) *configurationv1alpha1.EventGatewayDataPlaneCertificate {
	// certName is also used as the Konnect certificate name so that every
	// rotation registers a distinct Konnect entity (see
	// shareddataplane.CertEntityName).
	certName := shareddataplane.CertEntityName(egdp.Name, certChecksum)
	if deps := onPremDependenciesOf(cp); deps != nil {
		return buildOnPremCertificate(egdp, deps.gateway, certSecretName, certName)
	}
	keg := konnectEventGatewayFromResolved(cp)
	return &configurationv1alpha1.EventGatewayDataPlaneCertificate{
		APIVersion: configurationv1alpha1.GroupVersion.String(),
		Kind:       "EventGatewayDataPlaneCertificate",
		Name:       certName,
		Namespace:  egdp.Namespace,
		Labels:     selectorLabelsForKegDataPlane(egdp),
		Spec: configurationv1alpha1.EventGatewayDataPlaneCertificateSpec{
			// The deprecated gatewayRef is used on purpose: CRDs of releases
			// without parentRef drop it, which would leave the certificate
			// with no reference after a rollback. See
			// keepEventGatewayDataPlaneCertificateRefField.
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
				Name: certName,
			},
		},
	}
}

// keepEventGatewayDataPlaneCertificateRefField makes the desired certificate
// reference its KonnectEventGateway through the same field as the existing
// one. The CRD never allows replacing parentRef with gatewayRef, so a
// certificate that already uses parentRef (e.g. created by a newer operator
// before a rollback, or migrated by a user) must keep it; otherwise every
// apply would be rejected.
func keepEventGatewayDataPlaneCertificateRefField(
	desired, existing *configurationv1alpha1.EventGatewayDataPlaneCertificate,
) {
	if existing.Spec.ParentRef == nil || desired.Spec.ParentRef != nil {
		return
	}
	desired.Spec.ParentRef = &commonv1alpha1.GatewayRef{
		Group: konnectv1alpha1.GroupVersion.Group,
		Kind:  "KonnectEventGateway",
		Name:  desired.Spec.GatewayRef.NamespacedRef.Name,
	}
	desired.Spec.GatewayRef = commonv1alpha1.ObjectRef{}
}

// selectorLabelsForKegDataPlane returns the labels identifying the
// operator-managed resources of the given KegDataPlane.
func selectorLabelsForKegDataPlane(egdp *eventgatewayv1alpha1.KegDataPlane) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.KEGDataPlaneManagedByLabelValue,
		consts.GatewayOperatorManagedByNameLabel:      egdp.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: egdp.Namespace,
	}
}

// cleanupStaleCertificates removes the certificate resources left over from
// an earlier rotation or a switch of provisioning mode: stale
// EventGatewayDataPlaneCertificates (all but the one named after the current
// checksum), when a manually-referenced Secret is in use, the
// operator-provisioned Automatic Secret, and, for an OnPremEventGateway, the
// client certificate Secrets the current one, Automatic or Manual, replaced,
// and the copy of the cp server CA bundle of the GWAdmin once it is no
// longer needed. The shared
// reconciler only calls this once the rollout onto the current certificate
// completed, so no running replica is left depending on what this removes.
func (r *Reconciler) cleanupStaleCertificates(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
	cp shareddataplane.ResolvedControlPlane,
	certChecksum string,
) error {
	if err := shareddataplane.CleanupStaleKonnectCertificates(ctx, cl, logger, egdp,
		&configurationv1alpha1.EventGatewayDataPlaneCertificateList{}, "EventGatewayDataPlaneCertificate",
		shareddataplane.CertEntityName(egdp.Name, certChecksum),
	); err != nil {
		return err
	}
	if err := cleanupServerBundleCopy(ctx, cl, logger, egdp, cp); err != nil {
		return err
	}
	// The client certificate Secrets of a data plane of an OnPremEventGateway
	// are kept while a certificate references them, also after a switch to
	// Manual: gw-admin keeps trusting a certificate until its
	// EventGatewayDataPlaneCertificate is gone.
	if egdp.Spec.ControlPlaneRef.OnPremNamespacedRef != nil {
		return r.cleanupStaleOnPremCertificates(ctx, cl, logger, egdp, certChecksum)
	}
	if isManualProvisioning(egdp) {
		return shareddataplane.CleanupStaleAutomaticCertificateSecret(ctx, cl, logger, egdp, consts.SecretKEGDataPlaneCertificateLabel)
	}
	return nil
}
