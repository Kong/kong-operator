/*
Copyright 2026 Kong Inc.

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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
)

// DBBackedHybridControlPlane stands for a control plane role in the DB backed hybrid mode deployed on prem.
// In the DB backed hybrid mode, the control plane is responsible for accepting configuration from KO spawned KIC instances
// and storing it in the database. The control plane is also responsible for propagating the configuration to the
// data plane instances. The control plane is not responsible for serving traffic, which is the responsibility of the
// data plane instances.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dbcp,categories=kong
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Admin GUI",description="The Resource is hardened",type=string,JSONPath=`.status.adminGUIServiceStatus.address.value`
// +kong:channels=kong-operator
type DBBackedHybridControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DBBackedHybridControlPlaneSpec   `json:"spec,omitempty"`
	Status DBBackedHybridControlPlaneStatus `json:"status,omitempty"`
}

// DBBackedHybridControlPlaneSpec stands for the desired state of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneSpec struct {
	DeploymentOptions DBBackedHybridControlPlaneDeploymentOptions `json:"deployment,omitempty"`

	NetworkOptions DBBackedHybridControlPlaneNetworkOptions `json:"network,omitempty"`
}

// DBBackedHybridControlPlaneDeploymentOptions represents the deployment options for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneDeploymentOptions struct {
	DeploymentOptions `json:",inline"`

	// License represents the license information for the DBBackedHybridControlPlane.
	// +required
	License DBBackedHybridControlPlaneLicense `json:"license"`

	// Database represents the database connection options for the DBBackedHybridControlPlane.
	// +required
	Database DBBackedHybridControlPlaneDatabaseConnectionOptions `json:"database"`

	// Hardened indicates whether the operator should apply a hardened
	// security context (non-root user, read-only root filesystem, dropped
	// capabilities) and the related volumes and environment variables to
	// the DataPlane's proxy container.
	//
	// Enabling this on an existing DataPlane causes a rolling restart of
	// its Pods.
	//
	// +optional
	// +kubebuilder:default=disabled
	Hardened commonv1alpha1.HardeningState `json:"hardened,omitempty"`
}

// DBBackedHybridControlPlaneLicenseType represents the type of specifying the license for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneLicenseType string

const (
	// DBBackedHybridControlPlaneLicenseTypeSecretRef represents the license type of specifying the license for the DBBackedHybridControlPlane by a Kubernetes Secret reference.
	DBBackedHybridControlPlaneLicenseTypeSecretRef DBBackedHybridControlPlaneLicenseType = "secretRef"
)

// DBBackedHybridControlPlaneLicense represents the license information for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneLicense struct {
	// Type represents the type of specifying the license for the DBBackedHybridControlPlane.
	// Now only `secretRef` is supported, which means the license is specified by a Kubernetes Secret reference.
	// +required
	// +kubebuilder:validation:Enum=secretRef
	Type DBBackedHybridControlPlaneLicenseType `json:"type"`

	// SecretRef represents a reference to a Kubernetes Secret that contains the license for the DBBackedHybridControlPlane.
	// +optional
	SecretRef *DBBackedHybridControlPlaneLicenseSecretRef `json:"secretRef,omitempty"`
}

// DBBackedHybridControlPlaneLicenseSecretRef represents a reference to a Kubernetes Secret that contains the license for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneLicenseSecretRef struct {
	// Name represents the name of the Kubernetes Secret that contains the license for the DBBackedHybridControlPlane.
	// +required
	Name string `json:"name"`
	// Key represents the key in the Kubernetes Secret that contains the license for the DBBackedHybridControlPlane.
	// +required
	Key string `json:"key"`
}

// DBBackedHybridControlPlaneDatabaseConnectionOptions represents the database connection options for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneDatabaseConnectionOptions struct {
	// Host represents the host of the database for the DBBackedHybridControlPlane.
	// Filled to `KONG_PG_HOST` environment variable.
	// +kubebuilder:validation:MinLength=1
	// +required
	Host string `json:"host"`
	// Port represents the port of the database for the DBBackedHybridControlPlane.
	// Filled to `KONG_PG_PORT` environment variable.
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=5432
	Port int32 `json:"port"`
	// DatabaseUser represents the username of the database for the DBBackedHybridControlPlane.
	// Filled to `KONG_PG_USER` environment variable.
	// +required
	Username string `json:"username"`
	// Password represents the password of the database for the DBBackedHybridControlPlane.
	// Filled to `KONG_PG_PASSWORD` environment variable as a Kubernetes Secret reference.
	// +required
	Password DBBackedHybridControlPlaneDatabasePassword `json:"password"`
	// DatabaseName represents the name of the database for the DBBackedHybridControlPlane.
	// Filled to `KONG_DATABASE` environment variable.
	// +required
	// +kubebuilder:default="kong"
	DatabaseName string `json:"databaseName"`
	// TLS represents the TLS configuration for the database connection of the DBBackedHybridControlPlane.
	// +optional
	TLS *DBBackedHybridControlPlaneDatabaseTLS `json:"tls,omitempty"`
}

// DatabaseTLSRequiredState represents the required state of TLS for the database connection.
type DatabaseTLSRequiredState string

const (
	// DatabaseTLSRequiredStateRequired indicates that TLS is required for the database connection.
	// Kong will fail to connect to the database if TLS is not available.
	DatabaseTLSRequiredStateRequired DatabaseTLSRequiredState = "required"
	// DatabaseTLSRequiredStateNotRequired indicates that TLS is not required for the database connection.
	// Kong will fallback to non-TLS connection if TLS is not available.
	DatabaseTLSRequiredStateNotRequired DatabaseTLSRequiredState = "notRequired"
)

// DatabaseTLSVerifyState represents the verification state of TLS for the database connection.
type DatabaseTLSVerifyState string

const (
	// DatabaseTLSVerifyEnabled indicates that TLS certificate verification is enabled.
	DatabaseTLSVerifyEnabled DatabaseTLSVerifyState = "enabled"
	// DatabaseTLSVerifyDisabled indicates that TLS certificate verification is disabled.
	DatabaseTLSVerifyDisabled DatabaseTLSVerifyState = "disabled"
)

// DatabaseTLSVersion represents the TLS version to be used for the database connection.
type DatabaseTLSVersion string

const (
	// DatabaseTLSVersionTLSv1_1 indicates that TLS version 1.1 should be used for the database connection.
	DatabaseTLSVersionTLSv1_1 DatabaseTLSVersion = "tlsv1_1"
	// DatabaseTLSVersionTLSv1_2 indicates that TLS version 1.2 should be used for the database connection.
	DatabaseTLSVersionTLSv1_2 DatabaseTLSVersion = "tlsv1_2"
	// DatabaseTLSVersionTLSv1_3 indicates that TLS version 1.3 should be used for the database connection.
	DatabaseTLSVersionTLSv1_3 DatabaseTLSVersion = "tlsv1_3"
	// DatabaseTLSVersionAny indicates that client would negotiate the highest TLS version with the server which cannot be lower than TLS v1.1.
	DatabaseTLSVersionAny DatabaseTLSVersion = "any"
)

// DBBackedHybridControlPlaneDatabaseTLS represents the TLS configuration for the database connection of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneDatabaseTLS struct {
	// Required indicates whether TLS is required for the database connection.
	// If set to "required", Kong gateway fails to connect to the database if TLS is not available.
	// Filled to `KONG_PG_SSL_REQUIRED` environment variable.
	// +required
	// +kubebuilder:validation:Enum=required;notRequired
	Required DatabaseTLSRequiredState `json:"required"`
	// Verify indicates whether the TLS certificate should be verified.
	// Filled to `KONG_PG_SSL_VERIFY` environment variable.
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	Verify DatabaseTLSVerifyState `json:"verify"`
	// Version represents the TLS version to be used for the database connection.
	// Filled to `KONG_PG_SSL_VERSION` environment variable.
	// +optional
	// +kubebuilder:default="tlsv1_2"
	// +kubebuilder:validation:Enum=tlsv1_1;tlsv1_2;tlsv1_3;any
	Version DatabaseTLSVersion `json:"version"`
	// TrustedCertificates represents the list of trusted TLS certificates for the database connection.
	// The secrets referenced here should contain the trusted TLS certificates for the database connection.
	// These secrets are mounted to the pod and the mounting path is filled in `KONG_LUA_SSL_TRUSTED_CERTIFICATES` environment variable.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	TrustedCertificates []corev1.SecretReference `json:"trustedCertificates,omitempty"`
	// VerifyDepth represents the maximum depth for verifying the TLS certificate chain
	// to prevent DoS attack on tracking the signing chain in certificate verification.
	// Filled to `KONG_LUA_SSL_VERIFY_DEPTH` environment variable.
	// +optional
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	VerifyDepth int `json:"verifyDepth"`
	// ClientCertificate represents the reference to the Kubernetes Secret containing the client TLS certificate for the database connection.
	// These secrets are mounted to the pod and the mounting path is filled in `KONG_PG_SSL_CERT` and `KONG_PG_SSL_KEY` environment variables.
	ClientCertificate *corev1.SecretReference `json:"clientCertificate,omitempty"`
}

// DBBackedHybridControlPlaneDatabasePassword represents the password for the database of the DBBackedHybridControlPlane.
// It is typically sourced from a Kubernetes Secret.
type DBBackedHybridControlPlaneDatabasePassword struct {
	// Type indicates the source type of the database password.
	// +required
	// +kubebuilder:validation:Enum=secretRef
	Type DBBackedHybridControlPlaneDatabasePasswordType `json:"type"`

	// SecretRef represents the reference to the Kubernetes Secret containing the database password.
	// It is required if the Type is set to "Secret".
	// +optional
	SecretRef *DBBackedHybridControlPlaneDatabasePasswordSecretRef `json:"secretRef,omitempty"`
}

// DBBackedHybridControlPlaneDatabasePasswordType represents the sourcetype of the database password for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneDatabasePasswordType string

const (
	// DBBackedHybridControlPlaneDatabasePasswordTypeSecretRef indicates that the database password is sourced from a Kubernetes Secret.
	DBBackedHybridControlPlaneDatabasePasswordTypeSecretRef DBBackedHybridControlPlaneDatabasePasswordType = "secretRef"
)

// DBBackedHybridControlPlaneDatabasePasswordSecretRef represents a reference to a Kubernetes Secret containing the database password for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneDatabasePasswordSecretRef struct {
	// Name represents the name of the Kubernetes Secret containing the database password.
	// +required
	Name string `json:"name"`

	// Key represents the key within the Kubernetes Secret that holds the database password.
	// +required
	Key string `json:"key"`
}

// DBBackedHybridControlPlaneNetworkOptions represents the network-related options for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneNetworkOptions struct {
	ServiceOptions DBBackedHybridControlPlaneServiceOptions `json:"services,omitempty"`
}

// DBBackedHybridControlPlaneServiceOptions represents the service-related options for the DBBackedHybridControlPlane.
// A DB backed hybrid control plane has the following services exposed:
// - Admin API (receiving administrative requests and Kong configuration)
// - Cluster service (dispatching Kong configuration to dataplanes)
// - Cluster telemetry service (collecting telemetry data from the dataplanes)
// - Admin GUI (Kong Manager, providing the web interface for administrative tasks)
// - Status service (providing the health and status information of the control plane)
// Currently only the Admin GUI service options are configurable. Other services have fixed configurations.
type DBBackedHybridControlPlaneServiceOptions struct {
	// AdminUI represents the service options for the Admin GUI of the DBBackedHybridControlPlane.
	// +optional
	AdminUI *DBBackedHybridControlPlaneServiceAdminGUIOptions `json:"adminGUI,omitempty"`
}

// AdminGUIState represents the enabled or disabled state of the Admin GUI service.
type AdminGUIState string

const (
	// AdminGUIStateEnabled indicates that the Admin GUI service is enabled.
	AdminGUIStateEnabled AdminGUIState = "enabled"
	// AdminGUIStateDisabled indicates that the Admin GUI service is disabled.
	AdminGUIStateDisabled AdminGUIState = "disabled"
)

// DBBackedHybridControlPlaneServiceAdminGUIOptions represents the service options for the
// Admin GUI ("Kong manager") of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneServiceAdminGUIOptions struct {
	// Enabled represents whether the Admin GUI is enabled or disabled for the DBBackedHybridControlPlane.
	// +optional
	// +kubebuilder:default=enabled
	Enabled AdminGUIState `json:"enabled,omitempty"`

	// Type represents the type of the service for the Admin GUI of the DBBackedHybridControlPlane.
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer;ExternalName
	// +optional
	// +kubebuilder:default=LoadBalancer
	Type corev1.ServiceType `json:"type,omitempty"`
	// +optional
	// +kubebuilder:default=8445
	Port int32 `json:"port,omitempty"`
}

// DBBackedHybridControlPlaneStatus represents the observed state of the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneStatus struct {
	// Conditions represent the latest available observations of a DBBackedHybridControlPlane's current state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Selector contains a unique DataPlane identifier used as a deterministic
	// label selector that is used throughout its dependent resources.
	// This is used e.g. as a label selector for DataPlane's Services, Deployments and PodDisruptionBudgets.
	//
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:MinLength=8
	Selector string `json:"selector,omitempty"`

	// ReadyReplicas indicates how many replicas have reported to be ready.
	ReadyReplicas int32 `json:"readyReplicas"`

	// Replicas indicates how many replicas have been set for the DataPlane.
	Replicas int32 `json:"replicas"`

	// AdminGUIServiceStatus indicates the status of the Admin GUI (Kong Manager) service for the DBBackedHybridControlPlane.
	// +optional
	AdminGUIServiceStatus *DBBackedHybridControlPlaneAdminGUIServiceStatus `json:"adminGUIServiceStatus,omitempty"`
}

// DBBackedHybridControlPlaneAdminGUIServiceStatus represents the status of the Admin GUI (Kong Manager) service for the DBBackedHybridControlPlane.
type DBBackedHybridControlPlaneAdminGUIServiceStatus struct {
	// Type represents the type of the service for the Admin GUI of the DBBackedHybridControlPlane.
	Type corev1.ServiceType `json:"type,omitempty"`

	// Name indicates the name of the service for the Admin GUI of the DBBackedHybridControlPlane.
	Name string `json:"name,omitempty"`

	// Address represents the address of the service for the Admin GUI of the DBBackedHybridControlPlane.
	Address Address `json:"address,omitempty"`
}

// DBBackedHybridControlPlaneList contains a list of DBBackedHybridControlPlane.
// +kubebuilder:object:root=true
type DBBackedHybridControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []DBBackedHybridControlPlane `json:"items"`
}

// The following methods implement the `ConditionsAwareObject` interface.

// GetConditions retrieves the DBBackedHybridControlPlane Status Conditions.
func (c *DBBackedHybridControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the DBBackedHybridControlPlane Status Conditions.
func (c *DBBackedHybridControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}
