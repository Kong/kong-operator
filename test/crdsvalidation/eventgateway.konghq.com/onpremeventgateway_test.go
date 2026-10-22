package crdsvalidation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func validOnPremEventGateway(ns string) *eventgatewayv1alpha1.OnPremEventGateway {
	return &eventgatewayv1alpha1.OnPremEventGateway{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{
			GWAdminRef: eventgatewayv1alpha1.GWAdminRef{
				Name: "gw-admin",
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

	withGWAdmin := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return onPremEventGatewayWith(ns.Name, func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
			obj.Spec.GWAdminRef.Name = name
		})
	}

	t.Run("spec", func(t *testing.T) {
		common.TestCasesGroup[*eventgatewayv1alpha1.OnPremEventGateway]{
			{
				Name:       "gwAdminRef - valid",
				TestObject: validOnPremEventGateway(ns.Name),
			},
			{
				// An empty gwAdminRef, e.g. without a name, is omitted, so the
				// whole spec is.
				Name: "missing spec - invalid",
				TestObject: &eventgatewayv1alpha1.OnPremEventGateway{
					ObjectMeta: common.CommonObjectMeta(ns.Name),
				},
				ExpectedErrorMessage: new("spec: Required value"),
			},
			{
				Name:                 "254 characters GWAdmin name - invalid",
				TestObject:           withGWAdmin(strings.Repeat("a", 254)),
				ExpectedErrorMessage: new("Too long: may not be"),
			},
			{
				// An update of something else evaluates the rule with the same
				// gwAdminRef.
				Name:       "unchanged gwAdminRef - valid",
				TestObject: validOnPremEventGateway(ns.Name),
				Update: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					if obj.Labels == nil {
						obj.Labels = map[string]string{}
					}
					obj.Labels["changed"] = "true"
				},
			},
			{
				Name:                 "GWAdmin name that is not a DNS subdomain - invalid",
				TestObject:           withGWAdmin("GW_Admin"),
				ExpectedErrorMessage: new("spec.gwAdminRef.name in body should match"),
			},
			{
				Name: "GWAdmin namespace that is not a DNS label - invalid",
				TestObject: onPremEventGatewayWith(ns.Name, func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Spec.GWAdminRef.Namespace = new("kong.system")
				}),
				ExpectedErrorMessage: new("spec.gwAdminRef.namespace in body should match"),
			},
			{
				Name: "gwAdminRef with a namespace - valid",
				TestObject: onPremEventGatewayWith(ns.Name, func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Spec.GWAdminRef.Namespace = new("kong")
				}),
			},
			{
				Name:       "changing the namespace of gwAdminRef - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				Update: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Spec.GWAdminRef.Namespace = new("kong")
				},
				ExpectedUpdateErrorMessage: new("gwAdminRef is immutable"),
			},
			{
				Name:       "changing gwAdminRef - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				Update: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Spec.GWAdminRef.Name = "other-gw-admin"
				},
				ExpectedUpdateErrorMessage: new("gwAdminRef is immutable"),
			},
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
			{
				Name:       "configRevision and configHash - valid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ConfigRevision = 1
					obj.Status.ConfigHash = "sha256:a"
				},
			},
			{
				Name:       "configHash too long - invalid",
				TestObject: validOnPremEventGateway(ns.Name),
				StatusUpdate: func(obj *eventgatewayv1alpha1.OnPremEventGateway) {
					obj.Status.ConfigRevision = 1
					obj.Status.ConfigHash = strings.Repeat("a", 129)
				},
				ExpectedStatusUpdateErrorMessage: new("Too long: may not be"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	// The rules on configRevision and configHash compare a status with the
	// one it replaces: each case writes the status twice.
	t.Run("status revision transitions", func(t *testing.T) {
		cl, err := client.New(cfg, client.Options{Scheme: scheme})
		require.NoError(t, err)
		type revision struct {
			revision int64
			hash     string
		}
		tests := []struct {
			name    string
			from    revision
			to      revision
			wantErr string
		}{
			{name: "the same revision - valid", from: revision{3, "sha256:a"}, to: revision{3, "sha256:a"}},
			{name: "a new revision with a new hash - valid", from: revision{3, "sha256:a"}, to: revision{4, "sha256:b"}},
			{name: "a new revision with the same hash - valid", from: revision{3, "sha256:a"}, to: revision{4, "sha256:a"}},
			{name: "a revision that decreases - invalid", from: revision{3, "sha256:a"}, to: revision{2, "sha256:a"},
				wantErr: "configRevision may not decrease"},
			{name: "a revision removed - invalid", from: revision{3, "sha256:a"}, to: revision{0, "sha256:a"},
				wantErr: "configRevision may not decrease"},
			{name: "a hash changed without a new revision - invalid", from: revision{3, "sha256:a"}, to: revision{3, "sha256:b"},
				wantErr: "configHash may only change along with a new configRevision"},
			{name: "a hash removed - invalid", from: revision{3, "sha256:a"}, to: revision{4, ""},
				wantErr: "configHash may only change along with a new configRevision"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				obj := validOnPremEventGateway(ns.Name)
				require.NoError(t, cl.Create(t.Context(), obj))
				t.Cleanup(func() { _ = cl.Delete(context.Background(), obj) })
				obj.Status.ConfigRevision, obj.Status.ConfigHash = tc.from.revision, tc.from.hash
				require.NoError(t, cl.Status().Update(t.Context(), obj))

				obj.Status.ConfigRevision, obj.Status.ConfigHash = tc.to.revision, tc.to.hash
				err := cl.Status().Update(t.Context(), obj)
				if tc.wantErr == "" {
					require.NoError(t, err)
					return
				}
				require.ErrorContains(t, err, tc.wantErr)
			})
		}
	})
}
