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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

const (
	// GWAdminKind holds the kind for the GWAdmin.
	GWAdminKind = "GWAdmin"
)

// GWAdmin is the Schema for the GWAdmin API. It deploys gw-admin, the
// on-prem control plane of the data planes that do not use Konnect (e.g. the
// Event Gateway). Kong Operator pushes the configuration to gw-admin on its
// API listener, and the data planes pull it from its control plane (cp)
// listener. Both listeners use mutual TLS: Kong Operator provisions and
// rotates every certificate they need, from three certificate authorities:
//
//   - the API CA: the API listener and Kong Operator authenticate each other
//     with it. Kong Operator creates it and keeps its key in its own
//     namespace.
//   - the cp server CA: the data planes verify the cp listener with it.
//     Kong Operator creates it and keeps its key in its own namespace, so
//     that nothing else can issue a certificate the data planes trust.
//   - the cp client CA: the cp listener verifies the data plane client
//     certificates with it. Automatic or the user's, Kong Operator copies it,
//     with its key, into its own namespace, where it issues the certificates
//     of the data planes it manages from it.
//
// The certificate authorities live and die with the GWAdmin object: deleting
// it, or restoring it under a new UID, creates new ones, and the data plane
// certificates must be issued again. gw-admin doesn't reload certificates:
// every change of the mounted ones rolls its pods.
//
// The server certificates of the listeners are mounted into the gw-admin
// pods, so they live in the namespace of the GWAdmin: whoever can read its
// Secrets can impersonate gw-admin with them, until they are renewed.
//
// Only data planes Kong Operator manages are supported. Their Deployments
// carry the gateway-operator.konghq.com/gwadmin-namespace and
// gateway-operator.konghq.com/gwadmin-name labels naming the GWAdmin, and
// their Pod template records what its pods mount: the SHA-256 of the ca.crt
// of status.tls.cpServerCABundleSecretName in the
// konghq.com/gw-admin-cp-server-bundle-checksum annotation, and the SHA-256
// fingerprint of the CA that issued their client certificate in the
// konghq.com/gw-admin-cp-client-ca annotation. They count when they run in
// the namespace of the GWAdmin, or in a namespace a KongReferenceGrant in the
// namespace of the GWAdmin grants, from OnPremEventGateway to GWAdmin. A
// rotation waits for them to roll out: the cp server certificate moves to a
// new issuer once every data plane runs a server bundle trusting it, and the
// previous cp client CA is dropped once every data plane runs a client
// certificate from the new one. The
// gateway-operator.konghq.com/rotation-skip-data-planes annotation set to
// "true" stops the rotations from waiting for them, e.g. to stop trusting a
// compromised certificate authority right away.
//
// The gw-admin pods only report Ready once Kong Operator has provisioned
// them through the API listener, so the Ready condition of the GWAdmin does
// not wait for the pods to be Ready: it reports whether the certificates, the
// Services and the Deployment are in place.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gwa,categories=kong
// +kubebuilder:printcolumn:name="Ready",description="The Resource is Ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Server URL",description="The URL data planes pull the configuration from",type=string,JSONPath=`.status.cp.serverURL`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 59 && self.metadata.name.matches('^[a-z]([-a-z0-9]*[a-z0-9])?$')",message="name must be a valid DNS-1035 label of at most 59 characters, so that the names of its Services are valid"
// +kong:channels=kong-operator
type GWAdmin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of GWAdmin.
	//
	// +optional
	Spec GWAdminSpec `json:"spec,omitzero"`

	// Status defines the observed state of GWAdmin.
	//
	// +optional
	Status GWAdminStatus `json:"status,omitempty"`
}

// GWAdminList contains a list of GWAdmin.
//
// +kubebuilder:object:root=true
type GWAdminList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of GWAdmins.
	Items []GWAdmin `json:"items"`
}

// GWAdminSpec defines the desired state of GWAdmin.
type GWAdminSpec struct {
	// Deployment configures the gw-admin Deployment.
	//
	// +optional
	Deployment *GWAdminDeploymentOptions `json:"deployment,omitempty"`

	// Network configures the Services in front of gw-admin.
	//
	// +optional
	Network *GWAdminNetworkOptions `json:"network,omitempty"`

	// TLS configures the certificates of gw-admin. By default Kong Operator
	// creates a certificate authority for this GWAdmin and issues and rotates
	// every certificate from it, so nothing has to be configured.
	//
	// +optional
	TLS *GWAdminTLS `json:"tls,omitempty"`
}

// GWAdminDeploymentOptions specifies options for the gw-admin Deployment.
//
// +kubebuilder:validation:XValidation:message="Using both replicas and scaling fields is not allowed.",rule="!(has(self.scaling) && has(self.replicas))"
type GWAdminDeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This field and the scaling field are mutually exclusive.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas,omitempty"`

	// Scaling defines the scaling options for the Deployment.
	//
	// +optional
	Scaling *GWAdminScaling `json:"scaling,omitempty"`

	// Annotations are custom annotations that are propagated to the gw-admin
	// Deployment metadata by the operator.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are custom labels that are propagated to the gw-admin Deployment
	// metadata by the operator.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Labels map[string]string `json:"labels,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	// It's being applied on top of the generated Deployment using
	// [StrategicMergePatch](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch#StrategicMergePatch).
	// The gw-admin container is named "gw-admin": set its image here to
	// override the default one.
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// GWAdminScaling defines the scaling options for the gw-admin Deployment.
type GWAdminScaling struct {
	// HorizontalScaling defines horizontal scaling options for the Deployment.
	//
	// +optional
	HorizontalScaling *commonv1alpha1.HorizontalScaling `json:"horizontal,omitempty"`
}

// GWAdminNetworkOptions defines the network options of gw-admin.
type GWAdminNetworkOptions struct {
	// CP configures the Service the data planes pull the configuration from.
	// It is always a ClusterIP Service: the data planes run in the cluster.
	// The Service Kong Operator pushes the configuration through is not
	// configurable.
	//
	// +optional
	CP *GWAdminServiceOptions `json:"cp,omitempty"`
}

// GWAdminServiceOptions contains Service configuration for gw-admin.
//
// +kubebuilder:validation:XValidation:message="Every port must be named",rule="!has(self.ports) || self.ports.all(p, has(p.name))"
type GWAdminServiceOptions struct {
	// Annotations is an unstructured key value map stored with the Service resource.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are propagated to the Service.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Labels map[string]string `json:"labels,omitempty"`

	// TrafficDistribution offers a way to express preferences for how traffic is
	// distributed to Service endpoints.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#traffic-distribution
	//
	// +optional
	// +kubebuilder:validation:Enum=PreferSameZone;PreferSameNode
	TrafficDistribution *string `json:"trafficDistribution,omitempty"`

	// InternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on the ClusterIP.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#internal-traffic-policy
	//
	// +optional
	// +kubebuilder:validation:Enum=Cluster;Local
	InternalTrafficPolicy *corev1.ServiceInternalTrafficPolicy `json:"internalTrafficPolicy,omitempty"`

	// Ports defines the list of ports that are exposed by the Service.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	Ports []GWAdminServicePort `json:"ports,omitempty"`
}

// GWAdminServicePort contains information on a Service port.
type GWAdminServicePort struct {
	// The name of this port within the Service. It must be a DNS-1123 label.
	// The port named "cp" replaces the default cp port. Required: every port
	// must be named.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name *string `json:"name,omitempty"`

	// The port that will be exposed by this Service.
	//
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// Number or name of the port to access on the pods targeted by the Service.
	// Defaults to the cp listener of gw-admin.
	//
	// +optional
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`
}

// GWAdminTLS configures the certificates of gw-admin.
type GWAdminTLS struct {
	// ClientCA is the certificate authority the cp listener verifies the data
	// plane client certificates with. Only data plane client certificates are
	// issued from it: the server certificate of the cp listener comes from a
	// certificate authority of its own, so that whoever issues data plane
	// certificates can't impersonate gw-admin.
	//
	// With Automatic provisioning (the default) Kong Operator creates a
	// certificate authority for this GWAdmin, valid for a year, and renews it
	// once two thirds of that have elapsed.
	//
	// With Manual provisioning the referenced Secret holds the certificate
	// authority in tls.crt and its key in tls.key, e.g. the Secret of a
	// cert-manager Certificate with isCA set to true. When tls.crt holds a
	// chain, the first certificate is the signer, and it is the one gw-admin
	// trusts, even when it is an intermediate. Kong Operator never modifies
	// that Secret, and warns when less than a sixth of its validity is left.
	//
	// Either way, Kong Operator publishes the certificate authority, with its
	// key, in a Secret of its own namespace once the gw-admin pods trust it,
	// and issues the data plane certificates from that copy, never from the
	// user's Secret, so that they never come from a certificate authority the
	// pods don't trust yet. Its key never lands in the namespace of the
	// GWAdmin. The previous one stays trusted until every data plane rolled
	// out with a certificate from the new one.
	//
	// A rotation waits for the gw-admin Deployment to roll out, which needs
	// the new pods to turn Ready, i.e. to be provisioned by Kong Operator,
	// and for the data plane Deployments to roll out: it stalls, safely,
	// while they don't.
	//
	// +optional
	ClientCA *GWAdminCertificateSecret `json:"clientCA,omitempty"`

	// CP configures the server certificate of the listener the data planes
	// connect to.
	//
	// +optional
	CP *GWAdminServerCertificate `json:"cp,omitempty"`
}

// GWAdminCertificateProvisioning is the method used to provision a certificate.
//
// +kubebuilder:validation:Enum=Manual;Automatic
type GWAdminCertificateProvisioning string

const (
	// GWAdminCertificateProvisioningManual indicates the certificate is
	// supplied by the user through a Secret.
	GWAdminCertificateProvisioningManual GWAdminCertificateProvisioning = "Manual"
	// GWAdminCertificateProvisioningAutomatic indicates the certificate is
	// generated and managed by the operator.
	GWAdminCertificateProvisioningAutomatic GWAdminCertificateProvisioning = "Automatic"
)

// GWAdminCertificateSecret configures how a certificate is provisioned.
//
// +kubebuilder:validation:XValidation:rule="self.provisioning == 'Manual' ? has(self.secretRef) : !has(self.secretRef)",message="secretRef must be set when provisioning is Manual and only then"
type GWAdminCertificateSecret struct {
	// Provisioning is the method used to provision the certificate.
	//
	// +optional
	// +kubebuilder:default=Automatic
	Provisioning GWAdminCertificateProvisioning `json:"provisioning,omitempty"`

	// SecretRef references the Secret holding the certificate. Only used, and
	// required, when Provisioning is Manual. The Secret must carry the
	// operator's secret label selector (default "konghq.com/secret: \"true\"")
	// or the operator cannot see it; with cert-manager set it through
	// spec.secretTemplate.labels of the Certificate.
	//
	// +optional
	SecretRef *GWAdminSecretRef `json:"secretRef,omitempty"`
}

// GWAdminServerCertificate configures the server certificate of a listener.
//
// +kubebuilder:validation:XValidation:rule="self.provisioning == 'Manual' ? has(self.secretRef) : !has(self.secretRef)",message="secretRef must be set when provisioning is Manual and only then"
type GWAdminServerCertificate struct {
	// Provisioning is the method used to provision the certificate.
	//
	// With Automatic provisioning (the default) Kong Operator issues the
	// certificate from the cp server CA, for the in-cluster names of the
	// Service.
	//
	// With Manual provisioning the referenced Secret holds the certificate in
	// tls.crt and its key in tls.key, e.g. the Secret of a cert-manager
	// Certificate. The issuers the data planes verify it with are taken from
	// the chain in tls.crt and from ca.crt, and added to
	// status.tls.cpServerCABundleSecretName: the Secret must carry at least
	// one of them. Kong Operator copies the certificate into a Secret it owns
	// once every data plane rolled out with a bundle trusting one of its
	// issuers, so a renewal from the same issuer is copied right away, and
	// warns when less than a sixth of its validity is left.
	//
	// +optional
	// +kubebuilder:default=Automatic
	Provisioning GWAdminCertificateProvisioning `json:"provisioning,omitempty"`

	// SecretRef references the Secret holding the certificate. Only used, and
	// required, when Provisioning is Manual. The Secret must carry the
	// operator's secret label selector (default "konghq.com/secret: \"true\"")
	// or the operator cannot see it.
	//
	// +optional
	SecretRef *GWAdminSecretRef `json:"secretRef,omitempty"`
}

// GWAdminSecretRef references a Secret in the namespace of the GWAdmin.
type GWAdminSecretRef struct {
	// Name is the name of the referenced Secret.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}

// GWAdminStatus defines the observed state of GWAdmin.
type GWAdminStatus struct {
	// Conditions describe the status of the GWAdmin.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Ready", status: "Unknown", reason: "Pending", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// API describes the Service Kong Operator pushes the configuration through.
	//
	// +optional
	API *GWAdminAPIStatus `json:"api,omitempty"`

	// CP describes the endpoint the data planes pull the configuration from.
	//
	// +optional
	CP *GWAdminCPStatus `json:"cp,omitempty"`

	// TLS lists the Secrets holding the trust material of gw-admin.
	//
	// +optional
	TLS *GWAdminTLSStatus `json:"tls,omitempty"`
}

// GWAdminAPIStatus describes the Service Kong Operator pushes the
// configuration through.
type GWAdminAPIStatus struct {
	// ServiceName is the name of the Service.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	ServiceName string `json:"serviceName,omitempty"`

	// PortName is the name of the Service port.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	PortName string `json:"portName,omitempty"`

	// Port is the Service port.
	//
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// GWAdminCPStatus describes the endpoint the data planes pull the
// configuration from.
type GWAdminCPStatus struct {
	// ServerURL is the in-cluster URL of the cp listener.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https' && url(self).getHostname() != ''",message="serverURL must be an https URL with a host"
	ServerURL string `json:"serverURL,omitempty"`
}

// GWAdminTLSStatus lists the Secrets holding the trust material of gw-admin
// that its peers need.
type GWAdminTLSStatus struct {
	// CPServerCABundleSecretName is the name of the Secret whose ca.crt holds
	// the certificate authorities the data planes verify the cp listener
	// with. It holds more than one certificate while the cp server
	// certificate moves to a new issuer, which waits for every data plane to
	// roll out with it.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	CPServerCABundleSecretName string `json:"cpServerCABundleSecretName,omitempty"`

	// APICABundleSecretName is the name of the Secret whose ca.crt holds the
	// certificate authorities of the API listener: Kong Operator verifies the
	// API listener with it, and the API listener verifies the client
	// certificate of Kong Operator with it. It holds more than one
	// certificate while a certificate authority rotation is in progress.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	APICABundleSecretName string `json:"apiCABundleSecretName,omitempty"`
}

// GetConditions retrieves the GWAdmin Status Conditions.
func (g *GWAdmin) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

// SetConditions sets the GWAdmin Status Conditions.
func (g *GWAdmin) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

// GetHorizontalScaling returns the GWAdmin horizontal scaling options, or nil
// when horizontal scaling is not configured.
func (g *GWAdmin) GetHorizontalScaling() *commonv1alpha1.HorizontalScaling {
	if g.Spec.Deployment == nil || g.Spec.Deployment.Scaling == nil {
		return nil
	}
	return g.Spec.Deployment.Scaling.HorizontalScaling
}
