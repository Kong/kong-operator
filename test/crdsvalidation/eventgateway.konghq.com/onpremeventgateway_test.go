package crdsvalidation_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validOnPremEventGateway(ns string) *eventgatewayv1alpha1.OnPremEventGateway {
	return &eventgatewayv1alpha1.OnPremEventGateway{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{
			GWAdmin: eventgatewayv1alpha1.GWAdmin{
				Service: eventgatewayv1alpha1.GWAdminService{
					Name:     "gw-admin",
					PortName: "admin",
				},
			},
		},
	}
}

func onPremEventGatewayWith(ns string, mutate func(*eventgatewayv1alpha1.OnPremEventGateway)) *eventgatewayv1alpha1.OnPremEventGateway {
	obj := validOnPremEventGateway(ns)
	mutate(obj)
	return obj
}

func TestOnPremEventGateway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	withServiceName := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return onPremEventGatewayWith(ns.Name, func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
			obj.Spec.GWAdmin.Service.Name = name
		})
	}
	withPortName := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return onPremEventGatewayWith(ns.Name, func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
			obj.Spec.GWAdmin.Service.PortName = name
		})
	}

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.OnPremEventGateway]{
			{
				Name:       "service name and port name - valid",
				TestObject: validOnPremEventGateway(ns.Name),
			},
			{
				// An empty gwAdmin is omitted, so the whole spec is.
				Name: "missing spec - invalid",
				TestObject: &eventgatewayv1alpha1.OnPremEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
				},
				ExpectedErrorMessage: new("spec: Required value"),
			},
			{
				Name:                 "missing service name - invalid",
				TestObject:           withServiceName(""),
				ExpectedErrorMessage: new("spec.gwAdmin.service.name: Required value"),
			},
			{
				Name:                 "missing port name - invalid",
				TestObject:           withPortName(""),
				ExpectedErrorMessage: new("spec.gwAdmin.service.portName: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("service name", func(t *testing.T) {
		const msg = "name must be a valid DNS-1035 label"
		common.TestCasesGroup[*eventgatewayv1alpha1.OnPremEventGateway]{
			{Name: "single letter - valid", TestObject: withServiceName("a")},
			{Name: "63 characters - valid", TestObject: withServiceName("a" + strings.Repeat("0", 62))},
			{Name: "64 characters - invalid", TestObject: withServiceName("a" + strings.Repeat("0", 63)), ExpectedErrorMessage: new("Too long: may not be")},
			{Name: "starts with a digit - invalid", TestObject: withServiceName("1gw-admin"), ExpectedErrorMessage: new(msg)},
			{Name: "uppercase - invalid", TestObject: withServiceName("GW-admin"), ExpectedErrorMessage: new(msg)},
			{Name: "ends with a dash - invalid", TestObject: withServiceName("gw-admin-"), ExpectedErrorMessage: new(msg)},
			{Name: "contains a dot - invalid", TestObject: withServiceName("gw.admin"), ExpectedErrorMessage: new(msg)},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("port name", func(t *testing.T) {
		const msg = "portName must be a valid DNS-1123 label"
		common.TestCasesGroup[*eventgatewayv1alpha1.OnPremEventGateway]{
			{Name: "starts with a digit - valid", TestObject: withPortName("8443-admin")},
			{Name: "digits only - valid", TestObject: withPortName("8443")},
			{Name: "consecutive dashes - valid", TestObject: withPortName("gw--admin")},
			{Name: "63 characters - valid", TestObject: withPortName(strings.Repeat("a", 63))},
			{Name: "64 characters - invalid", TestObject: withPortName(strings.Repeat("a", 64)), ExpectedErrorMessage: new("Too long: may not be")},
			{Name: "uppercase - invalid", TestObject: withPortName("Admin"), ExpectedErrorMessage: new(msg)},
			{Name: "starts with a dash - invalid", TestObject: withPortName("-admin"), ExpectedErrorMessage: new(msg)},
			{Name: "ends with a dash - invalid", TestObject: withPortName("admin-"), ExpectedErrorMessage: new(msg)},
			{Name: "contains a dot - invalid", TestObject: withPortName("gw.admin"), ExpectedErrorMessage: new(msg)},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.OnPremEventGateway]{
			{
				Name:       "conditions and https serverURL - valid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ServerURL = "https://gw-admin.kong.svc:8443"
					obj.Status.ObservedGeneration = 1
					meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
						Type:               string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType),
						Status:             metav1.ConditionTrue,
						Reason:             "Programmed",
						ObservedGeneration: 1,
					})
					meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
						Type:               string(eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType),
						Status:             metav1.ConditionFalse,
						Reason:             "NoPods",
						ObservedGeneration: 1,
					})
				},
			},
			{
				Name:       "http serverURL - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ServerURL = "http://gw-admin.kong.svc:8443"
				},
				ExpectedStatusUpdateErrorMessage: new("serverURL must be an https URL with a host"),
			},
			{
				Name:       "serverURL without a host - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ServerURL = "https://"
				},
				ExpectedStatusUpdateErrorMessage: new("serverURL must be an https URL with a host"),
			},
			{
				Name:       "serverURL that is not a URL - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ServerURL = "gw-admin.kong.svc"
				},
				ExpectedStatusUpdateErrorMessage: new("serverURL must be an https URL with a host"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
