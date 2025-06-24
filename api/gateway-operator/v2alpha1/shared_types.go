package v2alpha1

import corev1 "k8s.io/api/core/v1"

// NamespacedName is a resource identified by name and optional namespace.
//
// +apireference:kgo:include
type NamespacedName struct {
	// Name is the name of the resource.
	//
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the resource.
	//
	// +optional
	Namespace string `json:"namespace"`
}

// ServiceOptions is used to includes options to customize the ingress service,
// such as the annotations.
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="Cannot set ExternalTrafficPolicy for ClusterIP service.", rule="has(self.type) && self.type == 'ClusterIP' ? !has(self.externalTrafficPolicy) : true"
type ServiceOptions struct {
	// Type determines how the Service is exposed.
	// Defaults to `LoadBalancer`.
	//
	// `ClusterIP` allocates a cluster-internal IP address for load-balancing
	// to endpoints.
	//
	// `NodePort` exposes the Service on each Node's IP at a static port (the NodePort).
	// To make the node port available, Kubernetes sets up a cluster IP address,
	// the same as if you had requested a Service of type: ClusterIP.
	//
	// `LoadBalancer` builds on NodePort and creates an external load-balancer
	// (if supported in the current cloud) which routes to the same endpoints
	// as the clusterIP.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	//
	// +optional
	// +kubebuilder:default=LoadBalancer
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	Type corev1.ServiceType `json:"type,omitempty" protobuf:"bytes,4,opt,name=type,casttype=ServiceType"`

	// Name defines the name of the service.
	// If Name is empty, the controller will generate a service name from the owning object.
	//
	// +optional
	Name *string `json:"name,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	//
	// More info: http://kubernetes.io/docs/user-guide/annotations
	//
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`

	// ExternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on one of the Service's "externally-facing" addresses (NodePorts,
	// ExternalIPs, and LoadBalancer IPs). If set to "Local", the proxy will configure
	// the service in a way that assumes that external load balancers will take care
	// of balancing the service traffic between nodes, and so each node will deliver
	// traffic only to the node-local endpoints of the service, without masquerading
	// the client source IP. (Traffic mistakenly sent to a node with no endpoints will
	// be dropped.) The default value, "Cluster", uses the standard behavior of
	// routing to all endpoints evenly (possibly modified by topology and other
	// features). Note that traffic sent to an External IP or LoadBalancer IP from
	// within the cluster will always get "Cluster" semantics, but clients sending to
	// a NodePort from within the cluster may need to take traffic policy into account
	// when picking a node.
	//
	// More info: https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip
	//
	// +optional
	// +kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicy `json:"externalTrafficPolicy,omitempty"`
}
