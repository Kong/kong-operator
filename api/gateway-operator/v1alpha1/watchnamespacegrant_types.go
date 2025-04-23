package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&WatchNamespaceGrant{}, &WatchNamespaceGrantList{})
}

// WatchNamespaceGrant is a grant that allows a trusted namespace to watch
// resources in the namespace this grant exists in.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +apireference:kgo:include
// +kong:channels=gateway-operator
type WatchNamespaceGrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the WatchNamespaceGrant.
	Spec WatchNamespaceGrantSpec `json:"spec,omitempty"`

	// Status is not specified for WatchNamespaceGrant but it may be added in the future.
}

// WatchNamespaceGrantSpec defines the desired state of an WatchNamespaceGrant.
//
// +apireference:kgo:include
type WatchNamespaceGrantSpec struct {
	// From describes the trusted namespaces and kinds that can reference the
	// namespace this grant exists in.
	//
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	From []WatchNamespaceGrantFrom `json:"from"`
}

// WatchNamespaceGrantFrom describes trusted namespaces.
type WatchNamespaceGrantFrom struct {
	// Group is the group of the referent.
	//
	// +kubebuilder:validation:Enum=gateway-operator.konghq.com
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// Kind is the kind of the referent.
	//
	// +kubebuilder:validation:Enum=ControlPlane
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// Namespace is the namespace of the referent.
	//
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}

// WatchNamespaceGrantList contains a list of WatchNamespaceGrants.
//
// +kubebuilder:object:root=true
// +apireference:kgo:include
type WatchNamespaceGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of WatchNamespaceGrants.
	Items []WatchNamespaceGrant `json:"items"`
}
