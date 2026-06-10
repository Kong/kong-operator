package configuration_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	common "github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validListener(ns string, ports ...intstr.IntOrString) *configurationv1alpha1.EventGatewayListener {
	return &configurationv1alpha1.EventGatewayListener{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: configurationv1alpha1.EventGatewayListenerSpec{
			GatewayRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "my-event-gateway",
				},
			},
			APISpec: configurationv1alpha1.EventGatewayListenerAPISpec{
				Name:      "listener-1",
				Addresses: []string{"0.0.0.0"},
				Ports:     ports,
			},
		},
	}
}

func TestEventGatewayListener(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	t.Run("ports field accepts int-or-string values", func(t *testing.T) {
		common.TestCasesGroup[*configurationv1alpha1.EventGatewayListener]{
			{
				Name:       "integer port",
				TestObject: validListener(ns.Name, intstr.FromInt32(9092)),
			},
			{
				Name:       "string port range",
				TestObject: validListener(ns.Name, intstr.FromString("19092-19095")),
			},
			{
				Name: "mixed list: integer and string range",
				TestObject: validListener(ns.Name,
					intstr.FromInt32(9092),
					intstr.FromString("9093-9095"),
					intstr.FromInt32(9096),
				),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
