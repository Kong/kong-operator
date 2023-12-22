package builder

// Define builders to build objects used in tests.

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
)

type testDataPlaneBuilder struct {
	dataplane *operatorv1beta1.DataPlane
}

// NewDataPlaneBuilder returns a builder for DataPlane object.
func NewDataPlaneBuilder() *testDataPlaneBuilder {
	return &testDataPlaneBuilder{
		dataplane: &operatorv1beta1.DataPlane{
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Network: operatorv1beta1.DataPlaneNetworkOptions{
						Services: &operatorv1beta1.DataPlaneServices{
							Ingress: &operatorv1beta1.ServiceOptions{},
						},
					},
				},
			},
		},
	}
}

// Build returns constructed DataPlane object.
func (b *testDataPlaneBuilder) Build() *operatorv1beta1.DataPlane {
	return b.dataplane
}

// WithObjectMeta sets the ObjectMeta of the DataPlane object.
func (b *testDataPlaneBuilder) WithObjectMeta(objectMeta metav1.ObjectMeta) *testDataPlaneBuilder {
	b.dataplane.ObjectMeta = objectMeta
	return b
}

func (b *testDataPlaneBuilder) initIngressServiceOptions() {
	if b.dataplane.Spec.DataPlaneOptions.Network.Services == nil {
		b.dataplane.Spec.DataPlaneOptions.Network.Services = &operatorv1beta1.DataPlaneServices{}
	}
	if b.dataplane.Spec.DataPlaneOptions.Network.Services.Ingress == nil {
		b.dataplane.Spec.DataPlaneOptions.Network.Services.Ingress = &operatorv1beta1.ServiceOptions{}
	}
}

// WithIngressServiceType sets the ServiceType of the Ingress service.
func (b *testDataPlaneBuilder) WithIngressServiceType(typ corev1.ServiceType) *testDataPlaneBuilder {
	b.initIngressServiceOptions()
	b.dataplane.Spec.DataPlaneOptions.Network.Services.Ingress.Type = typ
	return b
}

// WithIngressServiceAnnotations sets the Annotations of the Ingress service.
func (b *testDataPlaneBuilder) WithIngressServiceAnnotations(anns map[string]string) *testDataPlaneBuilder {
	b.initIngressServiceOptions()
	b.dataplane.Spec.DataPlaneOptions.Network.Services.Ingress.Annotations = anns
	return b
}

func (b *testDataPlaneBuilder) initDeploymentRolloutBlueGreen() {
	if b.dataplane.Spec.Deployment.Rollout == nil {
		b.dataplane.Spec.Deployment.Rollout = &operatorv1beta1.Rollout{}
	}
	if b.dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen == nil {
		b.dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen = &operatorv1beta1.BlueGreenStrategy{}
	}
}

// WithPromotionStrategy sets the PromotionStrategy of the DataPlane object.
func (b *testDataPlaneBuilder) WithPromotionStrategy(promotionStrategy operatorv1beta1.PromotionStrategy) *testDataPlaneBuilder {
	b.initDeploymentRolloutBlueGreen()
	b.dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy = promotionStrategy
	return b
}
