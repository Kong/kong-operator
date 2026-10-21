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

package gwadmin

import (
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/internal/gwadmin/published"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

const (
	apiTLSVolumeName = "api-tls"
	cpTLSVolumeName  = "cp-tls"
)

func apiServiceName(gw *operatorv1alpha1.GWAdmin) string { return gw.Name + "-api" }
func cpServiceName(gw *operatorv1alpha1.GWAdmin) string  { return gw.Name + "-cp" }

// deploymentConfig is the gw-admin Deployment, built by the shared DataPlane
// Deployment builder.
var deploymentConfig = shareddataplane.DeploymentConfig[*operatorv1alpha1.GWAdmin]{
	ContainerName:       consts.GWAdminContainerName,
	RelatedImageEnvVar:  consts.RelatedImageGWAdminEnvVar,
	DefaultImage:        consts.DefaultGWAdminImage,
	ManagedByLabelValue: consts.GWAdminManagedByLabelValue,
	PodTemplateSpec: func(gw *operatorv1alpha1.GWAdmin) *corev1.PodTemplateSpec {
		if gw.Spec.Deployment == nil {
			return nil
		}
		return gw.Spec.Deployment.PodTemplateSpec
	},
	DeploymentLabels: func(gw *operatorv1alpha1.GWAdmin) map[string]string {
		if gw.Spec.Deployment == nil {
			return nil
		}
		return gw.Spec.Deployment.Labels
	},
	DeploymentAnnotations: func(gw *operatorv1alpha1.GWAdmin) map[string]string {
		if gw.Spec.Deployment == nil {
			return nil
		}
		return gw.Spec.Deployment.Annotations
	},
	BuildContainer: buildContainer,
	// The TLS wiring must survive a pod template overlay.
	ReassertEnvVars: []string{
		consts.GWAdminAPITLSCertFileEnvVar,
		consts.GWAdminAPITLSKeyFileEnvVar,
		consts.GWAdminAPITLSClientCAFileEnvVar,
		consts.GWAdminCPTLSCertFileEnvVar,
		consts.GWAdminCPTLSKeyFileEnvVar,
		consts.GWAdminCPTLSClientCAFileEnvVar,
	},
}

// buildDeployment builds the desired gw-admin Deployment, recording the
// checksums of the mounted TLS material and of the trust bundles, so that
// rotating any of it rolls it, and so that Kong Operator can tell what the
// pods run.
func buildDeployment(
	logger logr.Logger,
	tc managedfields.TypeConverter,
	gw *operatorv1alpha1.GWAdmin,
	m *tlsMaterial,
) (*unstructured.Unstructured, error) {
	image := shareddataplane.ResolveImage(gw, deploymentConfig)
	d, err := shareddataplane.BuildDeployment(logger, tc, gw, shareddataplane.ResolvedControlPlane{}, image,
		"", "", m.checksum(), deploymentConfig, consts.GWAdminTLSChecksumAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to build Deployment for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	// Set after the user's Pod template overlay, so that it can't pin it.
	if err := unstructured.SetNestedField(d.Object, m.trustChecksum(),
		"spec", "template", "metadata", "annotations", consts.GWAdminTrustChecksumAnnotation); err != nil {
		return nil, fmt.Errorf("failed to set the trust checksum of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	// gw-admin never talks to the Kubernetes API: its pods get no token,
	// unless the user's Pod template overlay asks for one, e.g. for a sidecar.
	tokenPath := []string{"spec", "template", "spec", "automountServiceAccountToken"}
	if _, set, _ := unstructured.NestedFieldNoCopy(d.Object, tokenPath...); !set {
		if err := unstructured.SetNestedField(d.Object, false, tokenPath...); err != nil {
			return nil, fmt.Errorf("failed to disable the service account token of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
		}
	}
	return d, nil
}

// buildContainer builds the gw-admin container and the volumes holding its
// TLS material.
func buildContainer(
	gw *operatorv1alpha1.GWAdmin,
	_ shareddataplane.ResolvedControlPlane,
	image string,
	_ string, // certSecretName: gw-admin mounts its own TLS volumes.
	_ string, // adminCertSecretName: gw-admin mounts its own TLS volumes.
) (corev1.Container, []corev1.Volume, error) {
	ops := intstr.FromString(consts.GWAdminOpsPortName)
	container := corev1.Container{
		Name:  consts.GWAdminContainerName,
		Image: image,
		Args:  []string{"server"},
		Env: []corev1.EnvVar{
			{Name: consts.GWAdminAPITLSCertFileEnvVar, Value: consts.GWAdminAPITLSMountPath + "/" + corev1.TLSCertKey},
			{Name: consts.GWAdminAPITLSKeyFileEnvVar, Value: consts.GWAdminAPITLSMountPath + "/" + corev1.TLSPrivateKeyKey},
			{Name: consts.GWAdminAPITLSClientCAFileEnvVar, Value: consts.GWAdminAPITLSMountPath + "/" + caBundleKey},
			{Name: consts.GWAdminCPTLSCertFileEnvVar, Value: consts.GWAdminCPTLSMountPath + "/" + corev1.TLSCertKey},
			{Name: consts.GWAdminCPTLSKeyFileEnvVar, Value: consts.GWAdminCPTLSMountPath + "/" + corev1.TLSPrivateKeyKey},
			{Name: consts.GWAdminCPTLSClientCAFileEnvVar, Value: consts.GWAdminCPTLSMountPath + "/" + caBundleKey},
		},
		Ports: []corev1.ContainerPort{
			{Name: consts.GWAdminAPIPortName, ContainerPort: consts.GWAdminAPIPort, Protocol: corev1.ProtocolTCP},
			{Name: consts.GWAdminCPPortName, ContainerPort: consts.GWAdminCPPort, Protocol: corev1.ProtocolTCP},
			{Name: consts.GWAdminOpsPortName, ContainerPort: consts.GWAdminOpsPort, Protocol: corev1.ProtocolTCP},
		},
		LivenessProbe: &corev1.Probe{
			HTTPGet: &corev1.HTTPGetAction{Path: consts.GWAdminLivenessPath, Port: ops},
		},
		ReadinessProbe: &corev1.Probe{
			HTTPGet: &corev1.HTTPGetAction{Path: consts.GWAdminReadinessPath, Port: ops},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: apiTLSVolumeName, MountPath: consts.GWAdminAPITLSMountPath, ReadOnly: true},
			{Name: cpTLSVolumeName, MountPath: consts.GWAdminCPTLSMountPath, ReadOnly: true},
		},
	}
	container, volumes := k8sresources.HardenContainerWithSecurityContext(container, k8sresources.DataPlaneTypeGWAdmin)
	volumes = append(volumes,
		// Each listener only trusts the client certificates of its own trust
		// domain: data plane client certificates are never accepted by the
		// API listener.
		tlsVolume(apiTLSVolumeName, apiTLSSecretName(gw), apiCABundleSecretName(gw)),
		tlsVolume(cpTLSVolumeName, cpTLSSecretName(gw), cpClientCABundleSecretName(gw)),
	)
	return container, volumes, nil
}

// tlsVolume projects the server certificate of a listener and the trust
// bundle it verifies its clients with into one directory.
func tlsVolume(name, certSecretName, bundleSecretName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{
				{Secret: &corev1.SecretProjection{
					Name: certSecretName,
					Items: []corev1.KeyToPath{
						{Key: corev1.TLSCertKey, Path: corev1.TLSCertKey},
						{Key: corev1.TLSPrivateKeyKey, Path: corev1.TLSPrivateKeyKey},
					},
				}},
				{Secret: &corev1.SecretProjection{
					Name:  bundleSecretName,
					Items: []corev1.KeyToPath{{Key: caBundleKey, Path: caBundleKey}},
				}},
			},
		},
	}
}

func apiServiceConfig() shareddataplane.ServiceConfig[*operatorv1alpha1.GWAdmin] {
	return shareddataplane.ServiceConfig[*operatorv1alpha1.GWAdmin]{
		Description:         "API",
		NameSuffix:          "-api",
		DefaultPortName:     consts.GWAdminAPIPortName,
		DefaultPort:         consts.GWAdminAPIPort,
		ManagedByLabelValue: consts.GWAdminManagedByLabelValue,
	}
}

func cpServiceConfig() shareddataplane.ServiceConfig[*operatorv1alpha1.GWAdmin] {
	return shareddataplane.ServiceConfig[*operatorv1alpha1.GWAdmin]{
		Description:         "cp",
		NameSuffix:          "-cp",
		DefaultPortName:     consts.GWAdminCPPortName,
		DefaultPort:         consts.GWAdminCPPort,
		ManagedByLabelValue: consts.GWAdminManagedByLabelValue,
		Options:             cpServiceOptions,
	}
}

// generateAPIService builds the Service Kong Operator pushes the
// configuration through. It publishes the pods before they are Ready: they
// only turn Ready once provisioned through it.
func generateAPIService(gw *operatorv1alpha1.GWAdmin) *corev1.Service {
	svc := shareddataplane.GenerateBaseService(gw, apiServiceConfig())
	// The controllers pushing to gw-admin find the pods of the API Service by
	// these labels, which its EndpointSlices carry too.
	svc.Labels = published.APIServiceLabels(gw.Name)
	svc.Spec.PublishNotReadyAddresses = true
	return svc
}

// buildCPService builds the Service the data planes pull the configuration
// from, with the user's options applied.
func buildCPService(tc managedfields.TypeConverter, gw *operatorv1alpha1.GWAdmin) (client.Object, error) {
	svc, err := shareddataplane.BuildService(tc, gw, cpServiceConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to build cp Service for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	return svc, nil
}

// cpServiceOptions converts the user's cp Service options into the shared
// representation, or returns nil when not configured.
func cpServiceOptions(gw *operatorv1alpha1.GWAdmin) *shareddataplane.ServiceOptions {
	if gw.Spec.Network == nil || gw.Spec.Network.CP == nil {
		return nil
	}
	o := gw.Spec.Network.CP
	ports := make([]shareddataplane.ServicePort, 0, len(o.Ports))
	for _, p := range o.Ports {
		// Kubernetes would default the target port to the Service port, on
		// which gw-admin doesn't listen: target the cp listener instead.
		targetPort := p.TargetPort
		if targetPort == nil {
			targetPort = new(intstr.FromString(consts.GWAdminCPPortName))
		}
		ports = append(ports, shareddataplane.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: targetPort,
		})
	}
	return &shareddataplane.ServiceOptions{
		// Always ClusterIP: the data planes run in the cluster.
		Type:                  corev1.ServiceTypeClusterIP,
		Annotations:           o.Annotations,
		Labels:                o.Labels,
		TrafficDistribution:   o.TrafficDistribution,
		InternalTrafficPolicy: o.InternalTrafficPolicy,
		Ports:                 ports,
	}
}

// cpServicePort returns the port of the cp Service: the user's port named
// like the default one, or the default port.
func cpServicePort(gw *operatorv1alpha1.GWAdmin) int32 {
	if gw.Spec.Network != nil && gw.Spec.Network.CP != nil {
		for _, p := range gw.Spec.Network.CP.Ports {
			if p.Name != nil && *p.Name == consts.GWAdminCPPortName {
				return p.Port
			}
		}
	}
	return consts.GWAdminCPPort
}
