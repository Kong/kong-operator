package v2beta1

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

// +kubebuilder:validation:XValidation:message="Using both replicas and scaling fields is not allowed.",rule="!(has(self.scaling) && has(self.replicas))"

// DeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the number of replicas
// or pod options like container image and resource requirements.
// version, as well as Env variable overrides.
//
// +apireference:kgo:include
type DeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This is effectively shorthand for setting a scaling minimum and maximum
	// to the same value. This field and the scaling field are mutually exclusive:
	// You can only configure one or the other.
	//
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Scaling defines the scaling options for the deployment.
	//
	// +optional
	Scaling *Scaling `json:"scaling,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	// It's being applied on top of the generated Deployments using
	// [StrategicMergePatch](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch#StrategicMergePatch).
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// Scaling defines the scaling options for the deployment.
// +apireference:kgo:include
type Scaling struct {
	// HorizontalScaling defines horizontal scaling options for the deployment.
	// +optional
	HorizontalScaling *HorizontalScaling `json:"horizontal,omitempty"`
}

// HorizontalScaling defines horizontal scaling options for the deployment.
// It holds all the options from the HorizontalPodAutoscalerSpec besides the
// ScaleTargetRef which is being controlled by the Operator.
// +apireference:kgo:include
type HorizontalScaling struct {
	// minReplicas is the lower limit for the number of replicas to which the autoscaler
	// can scale down.  It defaults to 1 pod.  minReplicas is allowed to be 0 if the
	// alpha feature gate HPAScaleToZero is enabled and at least one Object or External
	// metric is configured.  Scaling is active as long as at least one metric value is
	// available.
	//
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty" protobuf:"varint,2,opt,name=minReplicas"`

	// maxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	// It cannot be less that minReplicas.
	//
	// +required
	MaxReplicas int32 `json:"maxReplicas" protobuf:"varint,3,opt,name=maxReplicas"`

	// metrics contains the specifications for which to use to calculate the
	// desired replica count (the maximum replica count across all metrics will
	// be used).  The desired replica count is calculated multiplying the
	// ratio between the target value and the current value by the current
	// number of pods.  Ergo, metrics used must decrease as the pod count is
	// increased, and vice-versa.  See the individual metric source types for
	// more information about how each type of metric must respond.
	// If not set, the default metric will be set to 80% average CPU utilization.
	//
	// +listType=atomic
	// +optional
	Metrics []autoscalingv2.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,4,rep,name=metrics"`

	// behavior configures the scaling behavior of the target
	// in both Up and Down directions (scaleUp and scaleDown fields respectively).
	// If not set, the default HPAScalingRules for scale up and scale down are used.
	//
	// +optional
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty" protobuf:"bytes,5,opt,name=behavior"`
}

// Rollout defines options for rollouts.
// +apireference:kgo:include
type Rollout struct {
	// Strategy contains the deployment strategy for rollout.
	//
	// +required
	Strategy RolloutStrategy `json:"strategy"`
}

// RolloutStrategy holds the rollout strategy options.
// +apireference:kgo:include
type RolloutStrategy struct {
	// BlueGreen holds the options specific for Blue Green Deployments.
	//
	// +required
	BlueGreen BlueGreenStrategy `json:"blueGreen"`
}

// BlueGreenStrategy defines the Blue Green deployment strategy.
// +apireference:kgo:include
type BlueGreenStrategy struct {
	// Promotion defines how the operator handles promotion of resources.
	//
	// +optional
	// +kubebuilder:default={"strategy":"BreakBeforePromotion"}
	Promotion *Promotion `json:"promotion,omitempty"`

	// Resources controls what happens to operator managed resources during or
	// after a rollout.
	//
	// +optional
	// +kubebuilder:default={"plan":{"deployment":"ScaleDownOnPromotionScaleUpOnRollout"}}
	Resources *RolloutResources `json:"resources,omitempty"`
}

// Promotion is a type that contains fields that define how the operator handles
// promotion of resources during a blue/green rollout.
// +apireference:kgo:include
type Promotion struct {
	// TODO: implement AutomaticPromotion https://github.com/Kong/gateway-operator/issues/164

	// Strategy indicates how you want the operator to handle the promotion of
	// the preview (green) resources (Deployments and Services) after all workflows
	// and tests succeed, OR if you even want it to break before performing
	// the promotion to allow manual inspection.
	//
	// +kubebuilder:validation:Enum=BreakBeforePromotion
	// +kubebuilder:default=BreakBeforePromotion
	// +optional
	Strategy *PromotionStrategy `json:"strategy,omitempty"`
}

// PromotionStrategy is the type of promotion strategy consts.
//
// Allowed values:
//
//   - `BreakBeforePromotion` is a promotion strategy which will ensure all new
//     resources are ready and then break, to enable manual inspection.
//     The user must indicate manually when they want the promotion to continue.
//     That can be done by annotating the `DataPlane` object with
//     `"gateway-operator.konghq.com/promote-when-ready": "true"`.
//
// +apireference:kgo:include
type PromotionStrategy string

const (
	// AutomaticPromotion indicates that once all workflows and tests have completed successfully,
	// the new resources should be promoted and replace the previous resources.
	AutomaticPromotion PromotionStrategy = "AutomaticPromotion"

	// BreakBeforePromotion is the same as AutomaticPromotion but with an added breakpoint
	// to enable manual inspection.
	// The user must indicate manually when they want the promotion to continue.
	// That can be done by annotating the DataPlane object with
	// `"gateway-operator.konghq.com/promote-when-ready": "true"`.
	BreakBeforePromotion PromotionStrategy = "BreakBeforePromotion"
)

// RolloutResources is the type which contains the fields which control how the operator
// manages the resources it manages during or after the rollout concludes.
// +apireference:kgo:include
type RolloutResources struct {
	// Plan defines the resource plan for managing resources during and after a rollout.
	//
	// +optional
	// +kubebuilder:default={"deployment":"ScaleDownOnPromotionScaleUpOnRollout"}
	Plan RolloutResourcePlan `json:"plan,omitempty"`
}

// RolloutResourcePlan is a type that holds rollout resource plan related fields
// which control how the operator handles resources during and after a rollout.
// +apireference:kgo:include
type RolloutResourcePlan struct {
	// TODO: https://github.com/Kong/gateway-operator/issues/163

	// Deployment describes how the operator manages Deployments during and after a rollout.
	//
	// +kubebuilder:validation:Enum=ScaleDownOnPromotionScaleUpOnRollout
	// +kubebuilder:default=ScaleDownOnPromotionScaleUpOnRollout
	// +optional
	Deployment RolloutResourcePlanDeployment `json:"deployment,omitempty"`
}

// RolloutResourcePlanDeployment is the type that holds the resource plan for
// managing the Deployment objects during and after a rollout.
//
// Allowed values:
//
//   - `ScaleDownOnPromotionScaleUpOnRollout` is a rollout
//     resource plan for Deployment which makes the operator scale down
//     the Deployment to 0 when the rollout is not initiated by a spec change
//     and then to scale it up when the rollout is initiated (the owner resource
//     like a DataPlane is patched or updated).
//
// +apireference:kgo:include
type RolloutResourcePlanDeployment string

const (
	// RolloutResourcePlanDeploymentScaleDownOnPromotionScaleUpOnRollout is a rollout
	// resource plan for Deployment which makes the operator scale down
	// the Deployment to 0 when the rollout is not initiated by a spec change
	// and then to scale it up when the rollout is initiated (the owner resource
	// like a DataPlane is patched or updated).
	RolloutResourcePlanDeploymentScaleDownOnPromotionScaleUpOnRollout RolloutResourcePlanDeployment = "ScaleDownOnPromotionScaleUpOnRollout"
	// RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout which makes the operator delete the
	// Deployment the rollout is not initiated by a spec change and then to
	// re-create it when the rollout is initiated (the owner resource like
	// a DataPlane is patched or updated)
	RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout RolloutResourcePlanDeployment = "DeleteOnPromotionRecreateOnRollout"
)

// GatewayConfigurationTargetKind is an object kind that can be targeted for
// GatewayConfiguration attachment.
// +apireference:kgo:include
type GatewayConfigurationTargetKind string

const (
	// GatewayConfigurationTargetKindGateway is a target kind which indicates
	// that a Gateway resource is the target.
	GatewayConfigurationTargetKindGateway GatewayConfigurationTargetKind = "Gateway"

	// GatewayConfigurationTargetKindGatewayClass is a target kind which indicates
	// that a GatewayClass resource is the target.
	GatewayConfigurationTargetKindGatewayClass GatewayConfigurationTargetKind = "GatewayClass"
)

const (
	// DataPlanePromoteWhenReadyAnnotationKey is the annotation key which can be used
	// to annotate a DataPlane object to signal that the live resources should be
	// promoted and replace the preview resources. It is used in conjunction with
	// the BreakBeforePromotion promotion strategy.
	// It has to be set to `true` to take effect. Once the operator detects the annotation, it will proceed with the
	// promotion and remove the annotation.
	DataPlanePromoteWhenReadyAnnotationKey = "gateway-operator.konghq.com/promote-when-ready"

	// DataPlanePromoteWhenReadyAnnotationTrue is the annotation value that needs to be set to the DataPlane's
	// DataPlanePromoteWhenReadyAnnotationKey annotation to signal that the new resources should be promoted.
	DataPlanePromoteWhenReadyAnnotationTrue = "true"
)

// NamespacedName is a resource identified by name and optional namespace.
//
// +apireference:kgo:include
type NamespacedName struct {
	// Name is the name of the resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
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
