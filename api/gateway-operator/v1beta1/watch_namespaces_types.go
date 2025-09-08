package v1beta1

// WatchNamespaces defines the namespaces to watch for resources
//
// +kubebuilder:validation:XValidation:message="list is required when type is 'list'", rule="self.type == 'list' ? has(self.list) : true"
// +kubebuilder:validation:XValidation:message="list must not be specified when type is not 'list'", rule="self.type != 'list' ? !has(self.list) : true"
// +apireference:kgo:include
type WatchNamespaces struct {
	// Type indicates the type of namespace watching to be done.
	// By default, all namespaces are watched.
	//
	// +required
	Type WatchNamespacesType `json:"type"`

	// List is a list of namespaces to watch for resources.
	// Only used when Type is set to List.
	//
	// +optional
	List []string `json:"list,omitempty"`
}

// WatchNamespacesType indicates the type of namespace watching to be done.
//
// +kubebuilder:validation:Enum=all;list;own
type WatchNamespacesType string

const (
	// WatchNamespacesTypeAll indicates that all namespaces should be watched
	// for resources.
	WatchNamespacesTypeAll WatchNamespacesType = "all"
	// WatchNamespacesTypeList indicates that only the namespaces listed in
	// the Namespaces field should be watched for resources.
	// All the namespaces enumerated in the list will be watched in addition to
	// the namespace of the object.
	WatchNamespacesTypeList WatchNamespacesType = "list"
	// WatchNamespacesTypeOwn indicates that only the namespace of the
	// object should be watched for resources.
	WatchNamespacesTypeOwn WatchNamespacesType = "own"
)
