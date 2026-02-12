package deploy

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// GatewayConfiguration deploys a GatewayConfiguration resource and returns it.
func GatewayConfiguration(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *operatorv2beta1.GatewayConfiguration {
	t.Helper()

	gwConfig := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gwconfig-",
		},
	}
	for _, opt := range opts {
		opt(gwConfig)
	}
	require.NoError(t, cl.Create(ctx, gwConfig))
	t.Logf("deployed GatewayConfiguration %s/%s", gwConfig.Namespace, gwConfig.Name)

	return gwConfig
}

// GatewayClass deploys a GatewayClass resource and returns it.
func GatewayClass(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *gatewayv1.GatewayClass {
	t.Helper()

	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gatewayclass-",
		},
	}
	for _, opt := range opts {
		opt(gc)
	}
	require.NoError(t, cl.Create(ctx, gc))
	t.Logf("deployed GatewayClass %s", gc.Name)

	return gc
}

// Gateway deploys a Gateway resource and returns it.
func Gateway(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *gatewayv1.Gateway {
	t.Helper()

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gateway-",
		},
	}
	for _, opt := range opts {
		opt(gw)
	}
	require.NoError(t, cl.Create(ctx, gw))
	t.Logf("deployed Gateway %s/%s", gw.Namespace, gw.Name)

	return gw
}

// WithGatewayConfigKonnectAuthRef returns an ObjOption that sets the Konnect
// APIAuthConfigurationRef on a GatewayConfiguration.
func WithGatewayConfigKonnectAuthRef(name, namespace string) ObjOption {
	return func(obj client.Object) {
		gwConfig, ok := obj.(*operatorv2beta1.GatewayConfiguration)
		if !ok {
			panic("WithGatewayConfigKonnectAuthRef can only be used with GatewayConfiguration")
		}
		if gwConfig.Spec.Konnect == nil {
			gwConfig.Spec.Konnect = &operatorv2beta1.KonnectOptions{}
		}
		gwConfig.Spec.Konnect.APIAuthConfigurationRef = &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
			Name:      name,
			Namespace: lo.ToPtr(namespace),
		}
	}
}

// WithGatewayClassControllerName returns an ObjOption that sets the controller
// name on a GatewayClass.
func WithGatewayClassControllerName(controllerName string) ObjOption {
	return func(obj client.Object) {
		gc, ok := obj.(*gatewayv1.GatewayClass)
		if !ok {
			panic("WithGatewayClassControllerName can only be used with GatewayClass")
		}
		gc.Spec.ControllerName = gatewayv1.GatewayController(controllerName)
	}
}

// WithGatewayClassParametersRef returns an ObjOption that sets the parameters
// ref on a GatewayClass.
func WithGatewayClassParametersRef(group, kind, name, namespace string) ObjOption {
	return func(obj client.Object) {
		gc, ok := obj.(*gatewayv1.GatewayClass)
		if !ok {
			panic("WithGatewayClassParametersRef can only be used with GatewayClass")
		}
		gc.Spec.ParametersRef = &gatewayv1.ParametersReference{
			Group:     gatewayv1.Group(group),
			Kind:      gatewayv1.Kind(kind),
			Name:      name,
			Namespace: lo.ToPtr(gatewayv1.Namespace(namespace)),
		}
	}
}

// WithGatewayClassName returns an ObjOption that sets the gateway class name on
// a Gateway.
func WithGatewayClassName(className string) ObjOption {
	return func(obj client.Object) {
		gw, ok := obj.(*gatewayv1.Gateway)
		if !ok {
			panic("WithGatewayClassName can only be used with Gateway")
		}
		gw.Spec.GatewayClassName = gatewayv1.ObjectName(className)
	}
}

// WithGatewayListeners returns an ObjOption that sets listeners on a Gateway.
func WithGatewayListeners(listeners ...gatewayv1.Listener) ObjOption {
	return func(obj client.Object) {
		gw, ok := obj.(*gatewayv1.Gateway)
		if !ok {
			panic("WithGatewayListeners can only be used with Gateway")
		}
		gw.Spec.Listeners = listeners
	}
}
