package gwadminref_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func gateway(namespace, name, gwAdminNamespace, gwAdminName string) *eventgatewayv1alpha1.OnPremEventGateway {
	g := &eventgatewayv1alpha1.OnPremEventGateway{
		Namespace: namespace, Name: name,
		Spec: eventgatewayv1alpha1.OnPremEventGatewaySpec{
			GWAdminRef: eventgatewayv1alpha1.GWAdminRef{Name: gwAdminName},
		},
	}
	if gwAdminNamespace != "" {
		g.Spec.GWAdminRef.Namespace = &gwAdminNamespace
	}
	return g
}

// grant returns a KongReferenceGrant in namespace allowing the
// OnPremEventGateways of from to reference the GWAdmin toName, or any GWAdmin
// when toName is empty.
func grant(namespace, from, toName string) *configurationv1alpha1.KongReferenceGrant {
	return gwadminreftest.Grant(namespace, "grant-"+from, from, toName)
}

func newClient(t *testing.T, funcs *interceptor.Funcs, objs ...client.Object) client.Client {
	t.Helper()
	// Build sets the ResourceVersion of the objects: the parallel subtests
	// sharing them each get copies.
	copies := make([]client.Object, 0, len(objs))
	for _, o := range objs {
		copies = append(copies, o.DeepCopyObject().(client.Object))
	}
	b := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(copies...)
	for _, o := range gwadminref.IndexOptions() {
		b = b.WithIndex(o.Object, o.Field, o.ExtractValueFn)
	}
	if funcs != nil {
		b = b.WithInterceptorFuncs(*funcs)
	}
	return b.Build()
}

var errList = errors.New("list failed")

func failingList() *interceptor.Funcs {
	return &interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			return errList
		},
	}
}

func names(gateways []eventgatewayv1alpha1.OnPremEventGateway) []string {
	out := make([]string, 0, len(gateways))
	for _, g := range gateways {
		out = append(out, g.Namespace+"/"+g.Name)
	}
	return out
}

func TestIndexOptions(t *testing.T) {
	t.Parallel()

	opts := gwadminref.IndexOptions()
	require.Len(t, opts, 1)
	opt := opts[0]
	assert.IsType(t, &eventgatewayv1alpha1.OnPremEventGateway{}, opt.Object)
	assert.Equal(t, gwadminref.IndexFieldOnPremEventGatewayOnGWAdmin, opt.Field)

	testCases := []struct {
		name     string
		obj      client.Object
		expected []string
	}{
		{
			name:     "a GWAdmin without namespace is in the namespace of the gateway",
			obj:      gateway("ns", "gw", "", "admin"),
			expected: []string{"ns/admin"},
		},
		{
			name:     "a GWAdmin with a namespace",
			obj:      gateway("ns", "gw", "other", "admin"),
			expected: []string{"other/admin"},
		},
		{
			name:     "another kind has no value",
			obj:      &corev1.Secret{Namespace: "ns", Name: "s"},
			expected: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, opt.ExtractValueFn(tc.obj))
		})
	}

	t.Run("an explicitly empty namespace defaults to the namespace of the gateway", func(t *testing.T) {
		t.Parallel()
		g := gateway("ns", "gw", "", "admin")
		g.Spec.GWAdminRef.Namespace = new("")
		assert.Equal(t, []string{"ns/admin"}, opt.ExtractValueFn(g))
	})
}

func TestGatewaysOf(t *testing.T) {
	t.Parallel()

	gwAdmin := types.NamespacedName{Namespace: "admin-ns", Name: "admin"}
	cl := newClient(t, nil,
		gateway("admin-ns", "local", "", "admin"),
		gateway("other", "remote", "admin-ns", "admin"),
		gateway("other", "elsewhere", "", "admin"),
		gateway("admin-ns", "another-admin", "", "another"),
	)

	t.Run("returns the gateways referencing the GWAdmin, in any namespace", func(t *testing.T) {
		t.Parallel()
		gateways, err := gwadminref.GatewaysOf(t.Context(), cl, gwAdmin)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"admin-ns/local", "other/remote"}, names(gateways))
	})

	t.Run("a GWAdmin nothing references has no gateways", func(t *testing.T) {
		t.Parallel()
		gateways, err := gwadminref.GatewaysOf(t.Context(), cl, types.NamespacedName{Namespace: "admin-ns", Name: "unused"})
		require.NoError(t, err)
		assert.Empty(t, gateways)
	})

	t.Run("a List error is returned", func(t *testing.T) {
		t.Parallel()
		_, err := gwadminref.GatewaysOf(t.Context(), newClient(t, failingList()), gwAdmin)
		require.ErrorIs(t, err, errList)
	})
}

func TestNotAllowed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		gateway       *eventgatewayv1alpha1.OnPremEventGateway
		objs          []client.Object
		funcs         *interceptor.Funcs
		expectReason  bool
		expectedError error
	}{
		{
			name:    "a GWAdmin of the namespace of the gateway is allowed",
			gateway: gateway("ns", "gw", "", "admin"),
		},
		{
			name:    "a GWAdmin named in the namespace of the gateway is allowed",
			gateway: gateway("ns", "gw", "ns", "admin"),
			// No grant is read for the same namespace.
			funcs: failingList(),
		},
		{
			name:         "a GWAdmin of another namespace without a grant is not allowed",
			gateway:      gateway("ns", "gw", "admin-ns", "admin"),
			expectReason: true,
		},
		{
			name:    "a grant for the GWAdmin allows it",
			gateway: gateway("ns", "gw", "admin-ns", "admin"),
			objs:    []client.Object{grant("admin-ns", "ns", "admin")},
		},
		{
			name:    "a grant for every GWAdmin allows it",
			gateway: gateway("ns", "gw", "admin-ns", "admin"),
			objs:    []client.Object{grant("admin-ns", "ns", "")},
		},
		{
			name:         "a grant for another GWAdmin doesn't allow it",
			gateway:      gateway("ns", "gw", "admin-ns", "admin"),
			objs:         []client.Object{grant("admin-ns", "ns", "another")},
			expectReason: true,
		},
		{
			name:         "a grant for another namespace doesn't allow it",
			gateway:      gateway("ns", "gw", "admin-ns", "admin"),
			objs:         []client.Object{grant("admin-ns", "another-ns", "admin")},
			expectReason: true,
		},
		{
			name:         "a grant in the namespace of the gateway doesn't allow it",
			gateway:      gateway("ns", "gw", "admin-ns", "admin"),
			objs:         []client.Object{grant("ns", "ns", "admin")},
			expectReason: true,
		},
		{
			name:          "a List error is returned",
			gateway:       gateway("ns", "gw", "admin-ns", "admin"),
			funcs:         failingList(),
			expectedError: errList,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reason, err := gwadminref.NotAllowed(t.Context(), newClient(t, tc.funcs, tc.objs...), tc.gateway)
			if tc.expectedError != nil {
				require.ErrorIs(t, err, tc.expectedError)
				assert.Empty(t, reason)
				return
			}
			require.NoError(t, err)
			if !tc.expectReason {
				assert.Empty(t, reason)
				return
			}
			assert.Equal(t, "no KongReferenceGrant in namespace admin-ns allows the OnPremEventGateways of namespace ns to reference the GWAdmin admin-ns/admin", reason)
		})
	}
}

func TestNamespacesGrantedBy(t *testing.T) {
	t.Parallel()

	otherKind := grant("admin-ns", "a", "")
	otherKind.Spec.To[0].Kind = "Service"
	otherFrom := grant("admin-ns", "a", "")
	otherFrom.Spec.From[0].Kind = "DataPlane"
	several := grant("admin-ns", "a", "")
	several.Spec.From = append(several.Spec.From, several.Spec.From[0], grant("admin-ns", "b", "").Spec.From[0], grant("admin-ns", "admin-ns", "").Spec.From[0])

	testCases := []struct {
		name     string
		grant    *configurationv1alpha1.KongReferenceGrant
		expected []string
	}{
		{name: "the namespace of the gateways", grant: grant("admin-ns", "a", "admin"), expected: []string{"a"}},
		{name: "not to a GWAdmin", grant: otherKind},
		{name: "not from an OnPremEventGateway", grant: otherFrom},
		{name: "each namespace once, not its own", grant: several, expected: []string{"a", "b"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, gwadminref.NamespacesGrantedBy(tc.grant))
		})
	}
}

func TestOfGrant(t *testing.T) {
	t.Parallel()

	g := grant("admin-ns", "ns", "")
	gwAdmins := []client.Object{
		&operatorv1alpha1.GWAdmin{Namespace: "admin-ns", Name: "a"},
		&operatorv1alpha1.GWAdmin{Namespace: "admin-ns", Name: "b"},
		&operatorv1alpha1.GWAdmin{Namespace: "other", Name: "c"},
	}

	t.Run("returns the GWAdmins of the namespace of the grant", func(t *testing.T) {
		t.Parallel()
		keys := gwadminref.OfGrant(t.Context(), newClient(t, nil, gwAdmins...), g)
		assert.ElementsMatch(t, []types.NamespacedName{
			{Namespace: "admin-ns", Name: "a"},
			{Namespace: "admin-ns", Name: "b"},
		}, keys)
	})

	t.Run("a namespace without GWAdmins has none", func(t *testing.T) {
		t.Parallel()
		keys := gwadminref.OfGrant(t.Context(), newClient(t, nil, gwAdmins...), grant("empty", "ns", ""))
		assert.NotNil(t, keys)
		assert.Empty(t, keys)
	})

	t.Run("a List error returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, gwadminref.OfGrant(t.Context(), newClient(t, failingList(), gwAdmins...), g))
	})
}
