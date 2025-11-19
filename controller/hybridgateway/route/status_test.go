package route

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

// Test helpers for BuildProgrammedCondition
type fakeListClient struct {
	client.Client
	fail  bool
	items []*unstructured.Unstructured
}

func (f *fakeListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if f.fail {
		return fmt.Errorf("list error")
	}
	ulist, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return fmt.Errorf("not unstructured list")
	}
	ulist.Items = make([]unstructured.Unstructured, len(f.items))
	for i, item := range f.items {
		ulist.Items[i] = *item
	}
	return nil
}

func strPtr(s string) *gatewayv1.Hostname {
	h := gatewayv1.Hostname(s)
	return &h
}

func Test_parentRefKey(t *testing.T) {
	tests := []struct {
		name  string
		input gwtypes.ParentReference
		want  string
	}{
		{
			name: "all fields set",
			input: gwtypes.ParentReference{
				Group:       groupPtr("group"),
				Kind:        kindPtr("kind"),
				Namespace:   nsPtr("namespace"),
				Name:        "name",
				SectionName: sectionPtr("section"),
				Port:        portPtr(8080),
			},
			want: "group/kind/namespace/name/section/8080",
		},
		{
			name: "some fields nil",
			input: gwtypes.ParentReference{
				Group:       nil,
				Kind:        nil,
				Namespace:   nil,
				Name:        "name",
				SectionName: nil,
				Port:        nil,
			},
			want: "/" + "/" + "/" + "name" + "/" + "/",
		},
		{
			name: "only name set",
			input: gwtypes.ParentReference{
				Name: "name",
			},
			want: "/" + "/" + "/" + "name" + "/" + "/",
		},
		{
			name: "port set",
			input: gwtypes.ParentReference{
				Name: "name",
				Port: portPtr(1234),
			},
			want: "/" + "/" + "/" + "name" + "/" + "/" + "1234",
		},
		{
			name: "section name set",
			input: gwtypes.ParentReference{
				Name:        "name",
				SectionName: sectionPtr("section"),
			},
			want: "/" + "/" + "/" + "name" + "/" + "section" + "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parentRefKey(tt.input)
			if got != tt.want {
				t.Errorf("parentRefKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_isParentRefEqual(t *testing.T) {
	name := gatewayv1.ObjectName("name")

	tests := []struct {
		name string
		a, b gwtypes.ParentReference
		want bool
	}{
		// Basic equality and difference cases
		{
			name: "all fields equal",
			a: gwtypes.ParentReference{
				Group: groupPtr("group"), Kind: kindPtr("kind"), Namespace: nsPtr("ns"), Name: name, SectionName: sectionPtr("section"), Port: portPtr(8080),
			},
			b: gwtypes.ParentReference{
				Group: groupPtr("group"), Kind: kindPtr("kind"), Namespace: nsPtr("ns"), Name: name, SectionName: sectionPtr("section"), Port: portPtr(8080),
			},
			want: true,
		},
		{
			name: "different group",
			a:    gwtypes.ParentReference{Group: groupPtr("group1"), Name: name},
			b:    gwtypes.ParentReference{Group: groupPtr("group2"), Name: name},
			want: false,
		},
		{
			name: "one group nil",
			a:    gwtypes.ParentReference{Name: name},
			b:    gwtypes.ParentReference{Group: groupPtr("group"), Name: name},
			want: false,
		},
		{
			name: "different kind",
			a:    gwtypes.ParentReference{Kind: kindPtr("kind1"), Name: name},
			b:    gwtypes.ParentReference{Kind: kindPtr("kind2"), Name: name},
			want: false,
		},
		{
			name: "different name",
			a:    gwtypes.ParentReference{Name: gatewayv1.ObjectName("name1")},
			b:    gwtypes.ParentReference{Name: gatewayv1.ObjectName("name2")},
			want: false,
		},
		{
			name: "different namespace",
			a:    gwtypes.ParentReference{Namespace: nsPtr("ns1"), Name: name},
			b:    gwtypes.ParentReference{Namespace: nsPtr("ns2"), Name: name},
			want: false,
		},
		{
			name: "different section name",
			a:    gwtypes.ParentReference{SectionName: sectionPtr("section1"), Name: name},
			b:    gwtypes.ParentReference{SectionName: sectionPtr("section2"), Name: name},
			want: false,
		},
		{
			name: "different port",
			a:    gwtypes.ParentReference{Port: portPtr(8080), Name: name},
			b:    gwtypes.ParentReference{Port: portPtr(9090), Name: name},
			want: false,
		},
		{
			name: "all fields nil except name",
			a:    gwtypes.ParentReference{Name: name},
			b:    gwtypes.ParentReference{Name: name},
			want: true,
		},
		{
			name: "one port nil",
			a:    gwtypes.ParentReference{Name: name, Port: portPtr(8080)},
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		// Nil edge cases for each field
		{
			name: "Group nil vs set",
			a: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Group = groupPtr("g")
				return r
			}(),
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		{
			name: "Group set vs nil",
			a:    gwtypes.ParentReference{Name: name},
			b: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Group = groupPtr("g")
				return r
			}(),
			want: false,
		},
		{
			name: "Kind nil vs set",
			a: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Kind = kindPtr("k")
				return r
			}(),
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		{
			name: "Kind set vs nil",
			a:    gwtypes.ParentReference{Name: name},
			b: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Kind = kindPtr("k")
				return r
			}(),
			want: false,
		},
		{
			name: "Namespace nil vs set",
			a: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Namespace = nsPtr("ns")
				return r
			}(),
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		{
			name: "Namespace set vs nil",
			a:    gwtypes.ParentReference{Name: name},
			b: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Namespace = nsPtr("ns")
				return r
			}(),
			want: false,
		},
		{
			name: "SectionName nil vs set",
			a: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.SectionName = sectionPtr("sec")
				return r
			}(),
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		{
			name: "SectionName set vs nil",
			a:    gwtypes.ParentReference{Name: name},
			b: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.SectionName = sectionPtr("sec")
				return r
			}(),
			want: false,
		},
		{
			name: "Port nil vs set",
			a: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Port = portPtr(1)
				return r
			}(),
			b:    gwtypes.ParentReference{Name: name},
			want: false,
		},
		{
			name: "Port set vs nil",
			a:    gwtypes.ParentReference{Name: name},
			b: func() gwtypes.ParentReference {
				r := gwtypes.ParentReference{Name: name}
				r.Port = portPtr(1)
				return r
			}(),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isParentRefEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("isParentRefEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isConditionEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b metav1.Condition
		want bool
	}{
		{
			name: "all fields equal",
			a:    metav1.Condition{Type: "A", Status: "True", Reason: "R", Message: "M", ObservedGeneration: 1},
			b:    metav1.Condition{Type: "A", Status: "True", Reason: "R", Message: "M", ObservedGeneration: 1},
			want: true,
		},
		{
			name: "different Type",
			a:    metav1.Condition{Type: "A"},
			b:    metav1.Condition{Type: "B"},
			want: false,
		},
		{
			name: "different Status",
			a:    metav1.Condition{Status: "True"},
			b:    metav1.Condition{Status: "False"},
			want: false,
		},
		{
			name: "different Reason",
			a:    metav1.Condition{Reason: "R1"},
			b:    metav1.Condition{Reason: "R2"},
			want: false,
		},
		{
			name: "different Message",
			a:    metav1.Condition{Message: "M1"},
			b:    metav1.Condition{Message: "M2"},
			want: false,
		},
		{
			name: "different ObservedGeneration",
			a:    metav1.Condition{ObservedGeneration: 1},
			b:    metav1.Condition{ObservedGeneration: 2},
			want: false,
		},
		{
			name: "all fields different",
			a:    metav1.Condition{Type: "A", Status: "True", Reason: "R1", Message: "M1", ObservedGeneration: 1},
			b:    metav1.Condition{Type: "B", Status: "False", Reason: "R2", Message: "M2", ObservedGeneration: 2},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConditionEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("isConditionEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_GetRouteGroupKind(t *testing.T) {
	tests := []struct {
		name  string
		gvk   schema.GroupVersionKind
		wantG string
		wantK string
	}{
		{
			name:  "custom group and kind",
			gvk:   schema.GroupVersionKind{Group: "custom.group", Kind: "CustomKind"},
			wantG: "custom.group",
			wantK: "CustomKind",
		},
		{
			name:  "empty group defaults to gateway.networking.k8s.io",
			gvk:   schema.GroupVersionKind{Group: "", Kind: "HTTPRoute"},
			wantG: "gateway.networking.k8s.io",
			wantK: "HTTPRoute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(tt.gvk)
			got := GetRouteGroupKind(obj)
			if got.Group == nil || string(*got.Group) != tt.wantG {
				t.Errorf("Group = %v, want %v", got.Group, tt.wantG)
			}
			if string(got.Kind) != tt.wantK {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.wantK)
			}
		})
	}
}

func Test_SetConditionMeta(t *testing.T) {
	tests := []struct {
		name       string
		cond       metav1.Condition
		generation int64
	}{
		{
			name:       "sets observed generation and last transition time",
			cond:       metav1.Condition{Type: "A", Status: "True"},
			generation: 42,
		},
		{
			name:       "zero generation",
			cond:       metav1.Condition{Type: "B", Status: "False"},
			generation: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := &gatewayv1.HTTPRoute{}
			route.Generation = tt.generation
			result := SetConditionMeta(tt.cond, route)
			if result.ObservedGeneration != tt.generation {
				t.Errorf("ObservedGeneration = %v, want %v", result.ObservedGeneration, tt.generation)
			}
			if result.LastTransitionTime.IsZero() {
				t.Errorf("LastTransitionTime should be set, got zero")
			}
		})
	}
}

func Test_isProgrammed(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want bool
	}{
		{
			name: "no status field",
			obj:  map[string]any{},
			want: false,
		},
		{
			name: "status but no conditions",
			obj: map[string]any{
				"status": map[string]any{},
			},
			want: false,
		},
		{
			name: "empty conditions slice",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{},
				},
			},
			want: false,
		},
		{
			name: "condition missing type",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{map[string]any{}},
				},
			},
			want: false,
		},
		{
			name: "condition type not Programmed",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{map[string]any{"type": "Other", "status": "True"}},
				},
			},
			want: false,
		},
		{
			name: "condition Programmed but status not True",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{map[string]any{"type": "Programmed", "status": "False"}},
				},
			},
			want: false,
		},
		{
			name: "condition Programmed and status True",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{map[string]any{"type": "Programmed", "status": "True"}},
				},
			},
			want: true,
		},
		{
			name: "multiple conditions, only one Programmed True",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{
						map[string]any{"type": "Other", "status": "True"},
						map[string]any{"type": "Programmed", "status": "True"},
					},
				},
			},
			want: true,
		},
		{
			name: "multiple conditions, Programmed but not True",
			obj: map[string]any{
				"status": map[string]any{
					"conditions": []any{
						map[string]any{"type": "Programmed", "status": "False"},
						map[string]any{"type": "Other", "status": "True"},
					},
				},
			},
			want: false,
		},
		// Error branch: NestedSlice returns error
		{
			name: "NestedSlice error",
			obj:  map[string]any{"status": "not-a-map"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{Object: tt.obj}
			got := isProgrammed(u)
			if got != tt.want {
				t.Errorf("isProgrammed() = %v, want %v", got, tt.want)
			}
		})
	}
}

type errorClient struct {
	client.Client
	failGateway      bool
	failGatewayClass bool
}

func (e *errorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if e.failGateway && key.Name == "my-gateway" && key.Namespace == "default" {
		return fmt.Errorf("generic gateway error")
	}
	if e.failGatewayClass && key.Name == "my-class" {
		return fmt.Errorf("generic gatewayclass error")
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func Test_BuildAcceptedCondition(t *testing.T) {
	ctx := context.Background()

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "gw",
		},
		Spec: gwtypes.GatewaySpec{
			Listeners: []gwtypes.Listener{
				{Name: "listener1", Port: 80, Protocol: gwtypes.HTTPProtocolType},
			},
			GatewayClassName: "my-class",
		},
		Status: gatewayv1.GatewayStatus{
			Listeners: []gatewayv1.ListenerStatus{
				{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gwtypes.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
			},
		},
	}

	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "route",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Hostnames: []gwtypes.Hostname{"example.com"},
		},
	}

	pRef := gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr("gateway.networking.k8s.io"), Name: "gw"}

	// Fake client with default namespace
	s := runtime.NewScheme()
	_ = gatewayv1.Install(s)
	_ = corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}).Build()

	tests := []struct {
		name       string
		gateway    *gwtypes.Gateway
		route      *gwtypes.HTTPRoute
		pRef       gwtypes.ParentReference
		client     client.Client
		setup      func(*gwtypes.Gateway, *gwtypes.HTTPRoute)
		wantType   string
		wantStatus metav1.ConditionStatus
		wantReason string
	}{
		{
			name: "no matching listeners",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"},
				Spec:       gwtypes.GatewaySpec{Listeners: []gwtypes.Listener{}},
			},
			route:      route,
			pRef:       pRef,
			client:     cl,
			wantType:   string(gwtypes.RouteConditionAccepted),
			wantStatus: metav1.ConditionFalse,
			wantReason: string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:    "not allowed by listeners",
			gateway: gateway,
			route:   route,
			pRef:    pRef,
			client:  cl,
			setup: func(gw *gwtypes.Gateway, r *gwtypes.HTTPRoute) {
				gw.Spec.Listeners[0].AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gatewayv1.NamespacesFromSame)}}
				gw.Namespace = "other"
			},
			wantType:   string(gwtypes.RouteConditionAccepted),
			wantStatus: metav1.ConditionFalse,
			wantReason: string(gwtypes.RouteReasonNotAllowedByListeners),
		},
		{
			name: "hostname mismatch",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "listener1", Port: 80, Protocol: gatewayv1.HTTPProtocolType, AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: fromNamespacesPtr(gatewayv1.NamespacesFromAll)}}, Hostname: strPtr("example.com")},
					},
					GatewayClassName: "my-class",
				},
				Status: gatewayv1.GatewayStatus{
					Listeners: []gatewayv1.ListenerStatus{
						{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
					},
				},
			},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec:       gwtypes.HTTPRouteSpec{Hostnames: []gwtypes.Hostname{"not-matching.com"}},
			},
			pRef:       pRef,
			client:     cl,
			wantType:   string(gwtypes.RouteConditionAccepted),
			wantStatus: metav1.ConditionFalse,
			wantReason: string(gwtypes.RouteReasonNoMatchingListenerHostname),
		},
		{
			name: "accepted route",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gatewayv1.Listener{
						{Name: "listener1", Port: 80, Protocol: gatewayv1.HTTPProtocolType, AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: fromNamespacesPtr(gatewayv1.NamespacesFromAll)}}, Hostname: strPtr("example.com")},
					},
					GatewayClassName: "my-class",
				},
				Status: gatewayv1.GatewayStatus{
					Listeners: []gatewayv1.ListenerStatus{
						{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
					},
				},
			},
			route:      route,
			pRef:       pRef,
			client:     cl,
			wantType:   string(gwtypes.RouteConditionAccepted),
			wantStatus: metav1.ConditionTrue,
			wantReason: string(gwtypes.RouteReasonAccepted),
		},
		{
			name: "missing namespace triggers error branch",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{Name: "listener1", Port: 80, Protocol: gwtypes.HTTPProtocolType},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Listeners: []gatewayv1.ListenerStatus{
						{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
					},
				},
			},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "nonexistent", Name: "route"},
				Spec:       gwtypes.HTTPRouteSpec{},
			},
			pRef:       gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr("gateway.networking.k8s.io"), Name: "gw", SectionName: sectionPtr("listener1")},
			client:     fake.NewClientBuilder().WithScheme(s).Build(), // no namespace object
			wantType:   "",
			wantStatus: "",
			wantReason: "",
		},
		{
			name: "invalid label selector triggers error branch",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "gw"},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     "listener1",
							Port:     80,
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: fromNamespacesPtr(gatewayv1.NamespacesFromSelector),
									Selector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{{
											Key:      "foo",
											Operator: "InvalidOperator", // will trigger error
											Values:   []string{"bar"},
										}},
									},
								},
							},
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Listeners: []gatewayv1.ListenerStatus{
						{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gatewayv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
					},
				},
			},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec:       gwtypes.HTTPRouteSpec{},
			},
			pRef:       gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr("gateway.networking.k8s.io"), Name: "gw", SectionName: sectionPtr("listener1")},
			client:     cl,
			wantType:   "",
			wantStatus: "",
			wantReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(tt.gateway, tt.route)
			}
			cond, err := BuildAcceptedCondition(ctx, logr.Discard(), tt.client, tt.gateway, tt.route, tt.pRef)
			if tt.name == "missing namespace triggers error branch" || tt.name == "invalid label selector triggers error branch" {
				require.Error(t, err)
				require.Nil(t, cond)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cond)
			require.Equal(t, tt.wantType, cond.Type)
			require.Equal(t, tt.wantStatus, cond.Status)
			require.Equal(t, tt.wantReason, cond.Reason)
		})
	}
}

func Test_BuildProgrammedCondition(t *testing.T) {
	ctx := context.Background()
	pRef := gwtypes.ParentReference{Name: "gw"}
	route := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
	}
	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "FakeResource"}

	// Helper to create unstructured with Programmed condition
	makeUnstructured := func(programmed bool) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		cond := map[string]any{"type": "Programmed", "status": "True"}
		if !programmed {
			cond["status"] = "False"
		}
		obj.Object["status"] = map[string]any{"conditions": []any{cond}}
		return obj
	}

	tests := []struct {
		name    string
		client  client.Client
		gvks    []schema.GroupVersionKind
		wantLen int
		wantErr bool
	}{
		{
			name:    "no resources found",
			client:  &fakeListClient{items: []*unstructured.Unstructured{}},
			gvks:    []schema.GroupVersionKind{gvk},
			wantLen: 0,
			wantErr: false,
		},
		{
			name:    "one programmed, one not",
			client:  &fakeListClient{items: []*unstructured.Unstructured{makeUnstructured(true), makeUnstructured(false)}},
			gvks:    []schema.GroupVersionKind{gvk},
			wantLen: 1, // Deduplicated by type
			wantErr: false,
		},
		{
			name:    "multiple GVKs",
			client:  &fakeListClient{items: []*unstructured.Unstructured{makeUnstructured(true)}},
			gvks:    []schema.GroupVersionKind{gvk, gvk},
			wantLen: 1, // Deduplication by type: only one condition remains
			wantErr: false,
		},
		{
			name:    "client.List error",
			client:  &fakeListClient{fail: true},
			gvks:    []schema.GroupVersionKind{gvk},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		conds, err := BuildProgrammedCondition(ctx, logr.Discard(), tt.client, route, pRef, tt.gvks)
		if tt.wantErr {
			require.Error(t, err, tt.name)
			require.Nil(t, conds, tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		require.Len(t, conds, tt.wantLen, tt.name)
		if tt.name == "one programmed, one not" && len(conds) == 1 {
			// For unknown GVKs, GetProgrammedConditionForGVK returns an empty condition
			require.Empty(t, conds[0].Type, "Type should be empty for unknown GVK")
		}
	}
}

func Test_SetStatusConditions(t *testing.T) {
	controllerName := "kong-controller"
	pRef := gwtypes.ParentReference{Name: "gw"}
	baseCond := metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Initial", Message: "Ready", ObservedGeneration: 1}
	updatedCond := metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "NotReady", Message: "Not ready", ObservedGeneration: 2}

	tests := []struct {
		name       string
		init       *gwtypes.HTTPRoute
		conds      []metav1.Condition
		wantUpdate bool
		verify     func(*testing.T, *gwtypes.HTTPRoute)
	}{
		{
			name:       "creates new ParentStatus if none exists",
			init:       &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{}}}},
			conds:      []metav1.Condition{baseCond},
			wantUpdate: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, pRef, route.Status.Parents[0].ParentRef)
				require.Equal(t, controllerName, string(route.Status.Parents[0].ControllerName))
				require.Equal(t, baseCond.Type, route.Status.Parents[0].Conditions[0].Type)
			},
		},
		{
			name:       "updates existing condition if different",
			init:       &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName), Conditions: []metav1.Condition{baseCond}}}}}},
			conds:      []metav1.Condition{updatedCond},
			wantUpdate: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Equal(t, updatedCond.Status, route.Status.Parents[0].Conditions[0].Status)
			},
		},
		{
			name:       "adds new condition if type not present",
			init:       &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName), Conditions: []metav1.Condition{baseCond}}}}}},
			conds:      []metav1.Condition{baseCond, {Type: "Other", Status: metav1.ConditionTrue}},
			wantUpdate: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents[0].Conditions, 2)
			},
		},
		{
			name:       "removes stale conditions not in new set",
			init:       &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName), Conditions: []metav1.Condition{baseCond, {Type: "Stale", Status: metav1.ConditionTrue}}}}}}},
			conds:      []metav1.Condition{baseCond},
			wantUpdate: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents[0].Conditions, 1)
				require.Equal(t, "Ready", route.Status.Parents[0].Conditions[0].Type)
			},
		},
		{
			name: "removes multiple stale Programmed conditions when resources deleted",
			init: &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{
				ParentRef:      pRef,
				ControllerName: gwtypes.GatewayController(controllerName),
				Conditions: []metav1.Condition{
					{Type: "Accepted", Status: metav1.ConditionTrue},
					{Type: "ResolvedRefs", Status: metav1.ConditionTrue},
					{Type: "Programmed-KongRoute-1", Status: metav1.ConditionTrue},
					{Type: "Programmed-KongRoute-2", Status: metav1.ConditionTrue},
					{Type: "Programmed-KongTarget-1", Status: metav1.ConditionTrue},
				},
			}}}}},
			conds: []metav1.Condition{
				{Type: "Accepted", Status: metav1.ConditionTrue},
				{Type: "ResolvedRefs", Status: metav1.ConditionTrue},
			},
			wantUpdate: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents[0].Conditions, 2)
				require.Equal(t, "Accepted", route.Status.Parents[0].Conditions[0].Type)
				require.Equal(t, "ResolvedRefs", route.Status.Parents[0].Conditions[1].Type)
			},
		},
		{
			name:       "no update if condition is identical",
			init:       &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName), Conditions: []metav1.Condition{baseCond}}}}}},
			conds:      []metav1.Condition{baseCond},
			wantUpdate: false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Equal(t, baseCond.Status, route.Status.Parents[0].Conditions[0].Status)
			},
		},
	}

	for _, tt := range tests {
		route := tt.init.DeepCopy()
		updated := SetStatusConditions(route, pRef, controllerName, tt.conds...)
		require.Equal(t, tt.wantUpdate, updated, tt.name)
		if tt.verify != nil {
			tt.verify(t, route)
		}
	}
}

func Test_CleanupOrphanedParentStatus(t *testing.T) {
	controllerName := "kong-controller"
	otherController := "other-controller"
	pRef := gwtypes.ParentReference{Name: "gw"}
	pRefOrphan := gwtypes.ParentReference{Name: "orphan"}
	parentStatus := gwtypes.RouteParentStatus{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName)}
	parentStatusOrphan := gwtypes.RouteParentStatus{ParentRef: pRefOrphan, ControllerName: gwtypes.GatewayController(controllerName)}
	parentStatusOther := gwtypes.RouteParentStatus{ParentRef: pRefOrphan, ControllerName: gwtypes.GatewayController(otherController)}

	tests := []struct {
		name   string
		init   *gwtypes.HTTPRoute
		want   bool
		verify func(*testing.T, *gwtypes.HTTPRoute)
	}{
		{
			name: "no parents in status",
			init: &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{}}}},
			want: false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Empty(t, route.Status.Parents)
			},
		},
		{
			name: "no orphaned parents",
			init: &gwtypes.HTTPRoute{
				Spec:   gwtypes.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gwtypes.ParentReference{pRef}}},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{parentStatus}}},
			},
			want: false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
			},
		},
		{
			name: "orphaned parent owned by controller is removed",
			init: &gwtypes.HTTPRoute{
				Spec:   gwtypes.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gwtypes.ParentReference{pRef}}},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{parentStatus, parentStatusOrphan}}},
			},
			want: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, pRef, route.Status.Parents[0].ParentRef)
			},
		},
		{
			name: "parent owned by another controller is not removed",
			init: &gwtypes.HTTPRoute{
				Spec:   gwtypes.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gwtypes.ParentReference{}}},
				Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{parentStatusOther}}},
			},
			want: false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, otherController, string(route.Status.Parents[0].ControllerName))
			},
		},
		{
			name: "mixed ownership and orphaned status",
			init: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default"},
				Spec:       gwtypes.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gwtypes.ParentReference{pRef}}},
				Status:     gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{parentStatus, parentStatusOrphan, parentStatusOther}}},
			},
			want: true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 2)
				// Only parentStatus and parentStatusOther should remain
				refs := []gwtypes.ParentReference{route.Status.RouteStatus.Parents[0].ParentRef, route.Status.RouteStatus.Parents[1].ParentRef}
				require.Contains(t, refs, pRef)
				// orphaned parent for current controller should be removed
				for _, ps := range route.Status.Parents {
					if ps.ControllerName == gwtypes.GatewayController(controllerName) {
						require.NotEqual(t, pRefOrphan, ps.ParentRef)
					}
				}
				// orphaned parent for other controller should remain
				foundOther := false
				for _, ps := range route.Status.Parents {
					if ps.ControllerName == gwtypes.GatewayController(otherController) && isParentRefEqual(ps.ParentRef, pRefOrphan) {
						foundOther = true
					}
				}
				require.True(t, foundOther, "Orphaned parent for other controller should remain")
			},
		},
	}

	for _, tt := range tests {
		route := tt.init.DeepCopy()
		logger := logr.Discard()
		removed := CleanupOrphanedParentStatus(logger, route, controllerName)
		require.Equal(t, tt.want, removed, tt.name)
		if tt.verify != nil {
			tt.verify(t, route)
		}
	}
}

func Test_RemoveStatusForParentRef(t *testing.T) {
	logger := logr.Discard()
	controllerName := "kong-controller"
	otherController := "other-controller"
	pRef := gwtypes.ParentReference{Name: "gw"}
	pRefOther := gwtypes.ParentReference{Name: "other"}
	parentStatus := gwtypes.RouteParentStatus{ParentRef: pRef, ControllerName: gwtypes.GatewayController(controllerName)}
	parentStatusOther := gwtypes.RouteParentStatus{ParentRef: pRefOther, ControllerName: gwtypes.GatewayController(otherController)}

	tests := []struct {
		name   string
		init   *gwtypes.HTTPRoute
		target gwtypes.ParentReference
		ctrl   string
		want   bool
		verify func(*testing.T, *gwtypes.HTTPRoute)
	}{
		{
			name:   "no parents in status",
			init:   &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gwtypes.RouteParentStatus{}}}},
			target: pRef,
			ctrl:   controllerName,
			want:   false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Empty(t, route.Status.Parents)
			},
		},
		{
			name:   "no matching parent/controller",
			init:   &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gwtypes.RouteParentStatus{parentStatusOther}}}},
			target: pRef,
			ctrl:   controllerName,
			want:   false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, pRefOther, route.Status.Parents[0].ParentRef)
			},
		},
		{
			name:   "matching parent/controller removed",
			init:   &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gwtypes.RouteParentStatus{parentStatus}}}},
			target: pRef,
			ctrl:   controllerName,
			want:   true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Empty(t, route.Status.Parents)
			},
		},
		{
			name:   "only other controller present",
			init:   &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gwtypes.RouteParentStatus{parentStatusOther}}}},
			target: pRefOther,
			ctrl:   controllerName,
			want:   false,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, otherController, string(route.Status.Parents[0].ControllerName))
			},
		},
		{
			name:   "multiple parents, only one matches and is removed",
			init:   &gwtypes.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gwtypes.RouteParentStatus{parentStatus, parentStatusOther}}}},
			target: pRef,
			ctrl:   controllerName,
			want:   true,
			verify: func(t *testing.T, route *gwtypes.HTTPRoute) {
				require.Len(t, route.Status.Parents, 1)
				require.Equal(t, pRefOther, route.Status.Parents[0].ParentRef)
				require.Equal(t, otherController, string(route.Status.Parents[0].ControllerName))
			},
		},
	}

	for _, tt := range tests {
		route := tt.init.DeepCopy()
		removed := RemoveStatusForParentRef(logger, route, tt.target, tt.ctrl)
		require.Equal(t, tt.want, removed, tt.name)
		if tt.verify != nil {
			tt.verify(t, route)
		}
	}
}

func Test_FilterMatchingListeners(t *testing.T) {
	pRef := gwtypes.ParentReference{Name: "listener1"}
	gw := &gwtypes.Gateway{
		Status: gatewayv1.GatewayStatus{
			Listeners: []gatewayv1.ListenerStatus{
				{Name: "listener1", Conditions: []metav1.Condition{{Type: string(gwtypes.ListenerConditionProgrammed), Status: metav1.ConditionTrue}}},
				{Name: "listener2", Conditions: []metav1.Condition{{Type: string(gwtypes.ListenerConditionProgrammed), Status: metav1.ConditionFalse}}},
			},
		},
	}
	listenerReady := gwtypes.Listener{Name: "listener1", Port: 80, Protocol: gwtypes.HTTPProtocolType}
	listenerNotReady := gwtypes.Listener{Name: "listener2", Port: 80, Protocol: gwtypes.HTTPProtocolType}
	listenerWrongProtocol := gwtypes.Listener{Name: "listener1", Port: 80, Protocol: "TCP"}
	tlsModePassthrough := gatewayv1.TLSModePassthrough
	listenerWrongTLS := gwtypes.Listener{Name: "listener1", Port: 80, Protocol: gwtypes.HTTPSProtocolType, TLS: &gatewayv1.GatewayTLSConfig{Mode: &tlsModePassthrough}}

	tests := []struct {
		name      string
		pRef      gwtypes.ParentReference
		listeners []gwtypes.Listener
		wantLen   int
		wantCond  bool
		condMsg   string
	}{
		{
			name:      "no listeners",
			pRef:      pRef,
			listeners: []gwtypes.Listener{},
			wantLen:   0,
			wantCond:  true,
			condMsg:   string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:      "section name mismatch",
			pRef:      gwtypes.ParentReference{Name: "listener1", SectionName: sectionPtr("notfound")},
			listeners: []gwtypes.Listener{listenerReady},
			wantLen:   0,
			wantCond:  true,
			condMsg:   string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:      "port mismatch",
			pRef:      gwtypes.ParentReference{Name: "listener1", Port: portPtr(81)},
			listeners: []gwtypes.Listener{listenerReady},
			wantLen:   0,
			wantCond:  true,
			condMsg:   string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:      "protocol mismatch",
			pRef:      pRef,
			listeners: []gwtypes.Listener{listenerWrongProtocol},
			wantLen:   0,
			wantCond:  true,
			condMsg:   string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:      "TLS mode mismatch",
			pRef:      pRef,
			listeners: []gwtypes.Listener{listenerWrongTLS},
			wantLen:   0,
			wantCond:  true,
			condMsg:   string(gwtypes.RouteReasonNoMatchingParent),
		},
		{
			name:      "listener matches but not ready",
			pRef:      gwtypes.ParentReference{Name: "listener2"},
			listeners: []gwtypes.Listener{listenerNotReady},
			wantLen:   0,
			wantCond:  true,
			condMsg:   "A Gateway Listener matches this route but is not ready",
		},
		{
			name:      "listener matches and is ready",
			pRef:      pRef,
			listeners: []gwtypes.Listener{listenerReady},
			wantLen:   1,
			wantCond:  false,
		},
		{
			name:      "multiple listeners, mixed readiness",
			pRef:      gwtypes.ParentReference{Name: "listener1"},
			listeners: []gwtypes.Listener{listenerReady, listenerNotReady},
			wantLen:   1,
			wantCond:  false,
		},
	}

	for _, tt := range tests {
		matches, cond := FilterMatchingListeners(logr.Discard(), gw, tt.pRef, tt.listeners)
		require.Len(t, matches, tt.wantLen, tt.name)
		if tt.wantCond {
			require.NotNil(t, cond, tt.name)
			require.Contains(t, cond.Message, tt.condMsg, tt.name)
		} else {
			require.Nil(t, cond, tt.name)
		}
	}
}

func Test_FilterListenersByAllowedRoutes(t *testing.T) {
	gw := &gwtypes.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"}}
	pRef := gwtypes.ParentReference{Name: "listener1"}
	listener := gwtypes.Listener{Name: "listener1", Port: 80, Protocol: gwtypes.HTTPProtocolType}
	kind := gwtypes.RouteGroupKind{Group: groupPtr("gateway.networking.k8s.io"), Kind: "HTTPRoute"}
	routeNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}}
	invalidSelector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "foo", Operator: "InvalidOperator", Values: []string{"bar"}}}}

	listenerAll := listener
	listenerAll.AllowedRoutes = &gwtypes.AllowedRoutes{}

	listenerKindMatch := listener
	listenerKindMatch.AllowedRoutes = &gwtypes.AllowedRoutes{Kinds: []gwtypes.RouteGroupKind{{Group: groupPtr("gateway.networking.k8s.io"), Kind: "HTTPRoute"}}}

	listenerKindMismatch := listener
	listenerKindMismatch.AllowedRoutes = &gwtypes.AllowedRoutes{Kinds: []gwtypes.RouteGroupKind{{Group: groupPtr("other"), Kind: "OtherRoute"}}}

	listenerNSAll := listener
	listenerNSAll.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gwtypes.NamespacesFromAll)}}

	listenerNSSame := listener
	listenerNSSame.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gwtypes.NamespacesFromSame)}}

	listenerNSSelector := listener
	listenerNSSelector.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gwtypes.NamespacesFromSelector), Selector: selector}}

	listenerNSSelectorNoMatch := listener
	listenerNSSelectorNoMatch.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gwtypes.NamespacesFromSelector), Selector: selector}}

	listenerNSSelectorInvalid := listener
	listenerNSSelectorInvalid.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: fromNamespacesPtr(gwtypes.NamespacesFromSelector), Selector: invalidSelector}}

	unknownFrom := gwtypes.NamespacesFromAll
	listenerNSUnknown := listener
	listenerNSUnknown.AllowedRoutes = &gwtypes.AllowedRoutes{Namespaces: &gwtypes.RouteNamespaces{From: &unknownFrom}}
	*listenerNSUnknown.AllowedRoutes.Namespaces.From = "Unknown"

	tests := []struct {
		name      string
		listeners []gwtypes.Listener
		kind      gwtypes.RouteGroupKind
		routeNS   *corev1.Namespace
		wantLen   int
		wantCond  bool
		wantErr   bool
	}{
		{
			name:      "AllowedRoutes nil (all allowed)",
			listeners: []gwtypes.Listener{listener},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Kind mismatch",
			listeners: []gwtypes.Listener{listenerKindMismatch},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   0,
			wantCond:  true,
			wantErr:   false,
		},
		{
			name:      "Kind match",
			listeners: []gwtypes.Listener{listenerKindMatch},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Namespaces nil (all allowed)",
			listeners: []gwtypes.Listener{listenerAll},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Namespaces From All",
			listeners: []gwtypes.Listener{listenerNSAll},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Namespaces From Same (match)",
			listeners: []gwtypes.Listener{listenerNSSame},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Namespaces From Selector (match)",
			listeners: []gwtypes.Listener{listenerNSSelector},
			kind:      kind,
			routeNS:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"foo": "bar"}}},
			wantLen:   1,
			wantCond:  false,
			wantErr:   false,
		},
		{
			name:      "Namespaces From Selector (no match)",
			listeners: []gwtypes.Listener{listenerNSSelectorNoMatch},
			kind:      kind,
			routeNS:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"foo": "baz"}}},
			wantLen:   0,
			wantCond:  true,
			wantErr:   false,
		},
		{
			name:      "Selector error branch",
			listeners: []gwtypes.Listener{listenerNSSelectorInvalid},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   0,
			wantCond:  false,
			wantErr:   true,
		},
		{
			name:      "Unknown From value error branch",
			listeners: []gwtypes.Listener{listenerNSUnknown},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   0,
			wantCond:  false,
			wantErr:   true,
		},
		{
			name:      "No matches returns condition",
			listeners: []gwtypes.Listener{listenerKindMismatch},
			kind:      kind,
			routeNS:   routeNS,
			wantLen:   0,
			wantCond:  true,
			wantErr:   false,
		},
		{
			name: "Namespaces From Selector (Selector nil)",
			listeners: []gwtypes.Listener{
				func() gwtypes.Listener {
					l := listener
					l.AllowedRoutes = &gwtypes.AllowedRoutes{
						Namespaces: &gwtypes.RouteNamespaces{
							From:     fromNamespacesPtr(gwtypes.NamespacesFromSelector),
							Selector: nil,
						},
					}
					return l
				}(),
			},
			kind:     kind,
			routeNS:  routeNS,
			wantLen:  0,
			wantCond: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		matches, cond, err := FilterListenersByAllowedRoutes(logr.Discard(), gw, pRef, tt.listeners, tt.kind, tt.routeNS)
		if tt.wantErr {
			require.Error(t, err, tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		require.Len(t, matches, tt.wantLen, tt.name)
		if tt.wantCond {
			require.NotNil(t, cond, tt.name)
		} else {
			require.Nil(t, cond, tt.name)
		}
	}
}

func Test_FilterListenersByHostnames(t *testing.T) {
	listenerNoHostname := gwtypes.Listener{Name: "no-hostname", Hostname: nil}
	listenerExact := gwtypes.Listener{Name: "exact", Hostname: strPtr("foo.example.com")}
	listenerMismatch := gwtypes.Listener{Name: "mismatch", Hostname: strPtr("bar.example.com")}
	listenerEmpty := gwtypes.Listener{Name: "empty", Hostname: strPtr("")}

	tests := []struct {
		name      string
		listeners []gwtypes.Listener
		hostnames []gwtypes.Hostname
		wantLen   int
		wantCond  bool
	}{
		{
			name:      "listener with no hostname matches all",
			listeners: []gwtypes.Listener{listenerNoHostname},
			hostnames: []gwtypes.Hostname{"foo.example.com"},
			wantLen:   1,
			wantCond:  false,
		},
		{
			name:      "listener with empty hostname matches all",
			listeners: []gwtypes.Listener{listenerEmpty},
			hostnames: []gwtypes.Hostname{"bar.example.com"},
			wantLen:   1,
			wantCond:  false,
		},
		{
			name:      "exact match listener",
			listeners: []gwtypes.Listener{listenerExact},
			hostnames: []gwtypes.Hostname{"foo.example.com"},
			wantLen:   1,
			wantCond:  false,
		},
		{
			name:      "no matching listener hostname",
			listeners: []gwtypes.Listener{listenerMismatch},
			hostnames: []gwtypes.Hostname{"foo.example.com"},
			wantLen:   0,
			wantCond:  true,
		},
		{
			name:      "multiple listeners, one matches",
			listeners: []gwtypes.Listener{listenerMismatch, listenerExact},
			hostnames: []gwtypes.Hostname{"foo.example.com"},
			wantLen:   1,
			wantCond:  false,
		},
	}

	for _, tt := range tests {
		matches, cond := FilterListenersByHostnames(logr.Discard(), tt.listeners, tt.hostnames)
		require.Len(t, matches, tt.wantLen, tt.name)
		if tt.wantCond {
			require.NotNil(t, cond, tt.name)
			require.Equal(t, string(gwtypes.RouteConditionAccepted), cond.Type)
			require.Equal(t, metav1.ConditionFalse, cond.Status)
			require.Equal(t, string(gwtypes.RouteReasonNoMatchingListenerHostname), cond.Reason)
		} else {
			require.Nil(t, cond, tt.name)
		}
	}
}

func TestFilterOutGVKByKind(t *testing.T) {
	gvks := []schema.GroupVersionKind{
		{Group: "foo", Version: "v1", Kind: "KongPlugin"},
		{Group: "foo", Version: "v1", Kind: "KongService"},
		{Group: "bar", Version: "v1", Kind: "KongRoute"},
		{Group: "foo", Version: "v1", Kind: "KongPlugin"},
	}

	tests := []struct {
		name         string
		input        []schema.GroupVersionKind
		kindToFilter string
		expects      []schema.GroupVersionKind
	}{
		{
			name:         "filter KongPlugin",
			input:        gvks,
			kindToFilter: "KongPlugin",
			expects: []schema.GroupVersionKind{
				{Group: "foo", Version: "v1", Kind: "KongService"},
				{Group: "bar", Version: "v1", Kind: "KongRoute"},
			},
		},
		{
			name:         "filter KongService",
			input:        gvks,
			kindToFilter: "KongService",
			expects: []schema.GroupVersionKind{
				{Group: "foo", Version: "v1", Kind: "KongPlugin"},
				{Group: "bar", Version: "v1", Kind: "KongRoute"},
				{Group: "foo", Version: "v1", Kind: "KongPlugin"},
			},
		},
		{
			name:         "filter KongRoute",
			input:        gvks,
			kindToFilter: "KongRoute",
			expects: []schema.GroupVersionKind{
				{Group: "foo", Version: "v1", Kind: "KongPlugin"},
				{Group: "foo", Version: "v1", Kind: "KongService"},
				{Group: "foo", Version: "v1", Kind: "KongPlugin"},
			},
		},
		{
			name:         "filter non-existent kind",
			input:        gvks,
			kindToFilter: "NonExistent",
			expects:      gvks,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterOutGVKByKind(tt.input, tt.kindToFilter)
			if !reflect.DeepEqual(got, tt.expects) {
				t.Errorf("unexpected result: got %+v, want %+v", got, tt.expects)
			}
		})
	}
}

func TestIsBackendRefSupported(t *testing.T) {
	tests := []struct {
		name  string
		group *gwtypes.Group
		kind  *gwtypes.Kind
		want  bool
	}{
		{
			name:  "nil group, Service kind",
			group: nil,
			kind:  kindPtr("Service"),
			want:  true,
		},
		{
			name:  "empty group, Service kind",
			group: groupPtr(""),
			kind:  kindPtr("Service"),
			want:  true,
		},
		{
			name:  "core group, Service kind",
			group: groupPtr("core"),
			kind:  kindPtr("Service"),
			want:  true,
		},
		{
			name:  "corev1 group, Service kind",
			group: groupPtr("corev1"),
			kind:  kindPtr("Service"),
			want:  false,
		},
		{
			name:  "v1 group, Service kind",
			group: groupPtr("v1"),
			kind:  kindPtr("Service"),
			want:  false,
		},
		{
			name:  "unsupported group, Service kind",
			group: groupPtr("foo"),
			kind:  kindPtr("Service"),
			want:  false,
		},
		{
			name:  "core group, unsupported kind",
			group: groupPtr("core"),
			kind:  kindPtr("Deployment"),
			want:  false,
		},
		{
			name:  "nil kind",
			group: groupPtr("core"),
			kind:  nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBackendRefSupported(tt.group, tt.kind)
			if got != tt.want {
				t.Errorf("IsBackendRefSupported(%v, %v) = %v, want %v", tt.group, tt.kind, got, tt.want)
			}
		})
	}
}

func TestIsHTTPReferenceGranted(t *testing.T) {
	tests := []struct {
		name          string
		grantSpec     gwtypes.ReferenceGrantSpec
		backendRef    gwtypes.HTTPBackendRef
		fromNamespace string
		want          bool
	}{
		{
			name: "granted when all fields match",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  ptrObjName("my-service"),
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("my-service"),
						Kind:  kindPtr("Service"),
						Group: groupPtr("core"),
					},
				},
			},
			fromNamespace: "default",
			want:          true,
		},
		{
			name: "not granted when group does not match",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  ptrObjName("my-service"),
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("my-service"),
						Kind:  kindPtr("Service"),
						Group: groupPtr("other"),
					},
				},
			},
			fromNamespace: "default",
			want:          false,
		},
		{
			name: "not granted when kind does not match",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  ptrObjName("my-service"),
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("my-service"),
						Kind:  kindPtr("OtherKind"),
						Group: groupPtr("core"),
					},
				},
			},
			fromNamespace: "default",
			want:          false,
		},
		{
			name: "not granted when name does not match",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  ptrObjName("my-service"),
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("other-service"),
						Kind:  kindPtr("Service"),
						Group: groupPtr("core"),
					},
				},
			},
			fromNamespace: "default",
			want:          false,
		},
		{
			name: "granted when to.Name is nil (wildcard)",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  nil,
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("my-service"),
						Kind:  kindPtr("Service"),
						Group: groupPtr("core"),
					},
				},
			},
			fromNamespace: "default",
			want:          true,
		},
		{
			name: "not granted when from.Namespace does not match",
			grantSpec: gwtypes.ReferenceGrantSpec{
				From: []gatewayv1beta1.ReferenceGrantFrom{{
					Group:     gwtypes.GroupName,
					Kind:      "HTTPRoute",
					Namespace: gwtypes.Namespace("default"),
				}},
				To: []gatewayv1beta1.ReferenceGrantTo{{
					Group: gwtypes.Group("core"),
					Kind:  gwtypes.Kind("Service"),
					Name:  ptrObjName("my-service"),
				}},
			},
			backendRef: gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:  gwtypes.ObjectName("my-service"),
						Kind:  kindPtr("Service"),
						Group: groupPtr("core"),
					},
				},
			},
			fromNamespace: "other-ns",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsHTTPReferenceGranted(tt.grantSpec, tt.backendRef, tt.fromNamespace)
			if got != tt.want {
				t.Errorf("IsHTTPReferenceGranted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// erroringClient wraps a fake client and returns errors for specific operations.
type erroringClient struct {
	client.Client
	errorOnListReferenceGrants bool
	errorOnGetService          bool
}

func (e *erroringClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if e.errorOnListReferenceGrants {
		// Check if this is a ReferenceGrantList by type assertion.
		if _, ok := list.(*gwtypes.ReferenceGrantList); ok {
			return fmt.Errorf("simulated list error for ReferenceGrants")
		}
	}
	return e.Client.List(ctx, list, opts...)
}

func (e *erroringClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if e.errorOnGetService {
		// Check if this is a Service by type assertion.
		if _, ok := obj.(*corev1.Service); ok {
			return fmt.Errorf("simulated get error for Service %s/%s", key.Namespace, key.Name)
		}
	}
	return e.Client.Get(ctx, key, obj, opts...)
}

func TestBuildResolvedRefsCondition(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	// Create test services
	serviceDefault := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "svc",
		},
	}

	serviceOtherNS := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "other-ns",
			Name:      "test-svc",
		},
	}

	// Create base route
	routeBase := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "route",
		},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gwtypes.BackendObjectReference{
							Name:  gwtypes.ObjectName("svc"),
							Kind:  kindPtr("Service"),
							Group: groupPtr("core"),
						},
					},
				}},
			}},
		},
	}

	tests := []struct {
		name                  string
		clientObjs            []client.Object
		route                 *gwtypes.HTTPRoute
		referenceGrantEnabled bool
		wantStatus            metav1.ConditionStatus
		wantReason            string
		wantMsgPart           string
	}{
		{
			name:                  "all references resolved - same namespace",
			clientObjs:            []client.Object{serviceDefault},
			route:                 routeBase,
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionTrue,
			wantReason:            string(gwtypes.RouteReasonResolvedRefs),
			wantMsgPart:           "All references resolved",
		},
		{
			name:       "unsupported group/kind",
			clientObjs: []client.Object{serviceDefault},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:  gwtypes.ObjectName("svc"),
									Kind:  kindPtr("Unsupported"),
									Group: groupPtr("core"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonInvalidKind),
			wantMsgPart:           "Unsupported BackendRef",
		},
		{
			name:                  "service not found",
			clientObjs:            []client.Object{},
			route:                 routeBase,
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonBackendNotFound),
			wantMsgPart:           "not found",
		},
		{
			name:       "cross-namespace, grant disabled",
			clientObjs: []client.Object{serviceOtherNS},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gwtypes.ObjectName("test-svc"),
									Kind:      kindPtr("Service"),
									Group:     groupPtr("core"),
									Namespace: nsPtr("other-ns"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonRefNotPermitted),
			wantMsgPart:           "ReferenceGrant support is disabled",
		},
		{
			name:       "cross-namespace, no grants found",
			clientObjs: []client.Object{serviceOtherNS},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gwtypes.ObjectName("test-svc"),
									Kind:      kindPtr("Service"),
									Group:     groupPtr("core"),
									Namespace: nsPtr("other-ns"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: true,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonRefNotPermitted),
			wantMsgPart:           "No ReferenceGrants found",
		},
		{
			name: "cross-namespace, grant exists but not permitted",
			clientObjs: []client.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "other-ns",
						Name:      "other-svc",
					},
				},
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "other-ns",
						Name:      "grant",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{{
							Group:     gwtypes.GroupName,
							Kind:      "HTTPRoute",
							Namespace: gwtypes.Namespace("default"),
						}},
						To: []gatewayv1beta1.ReferenceGrantTo{{
							Group: gwtypes.Group("core"),
							Kind:  gwtypes.Kind("Service"),
							Name:  ptrObjName("allowed-svc"),
						}},
					},
				},
			},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gwtypes.ObjectName("other-svc"),
									Kind:      kindPtr("Service"),
									Group:     groupPtr("core"),
									Namespace: nsPtr("other-ns"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: true,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonRefNotPermitted),
			wantMsgPart:           "not permitted by any ReferenceGrant",
		},
		{
			name: "cross-namespace, grant permits reference",
			clientObjs: []client.Object{
				serviceOtherNS,
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "other-ns",
						Name:      "grant",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{{
							Group:     gwtypes.GroupName,
							Kind:      "HTTPRoute",
							Namespace: gwtypes.Namespace("default"),
						}},
						To: []gatewayv1beta1.ReferenceGrantTo{{
							Group: gwtypes.Group("core"),
							Kind:  gwtypes.Kind("Service"),
							Name:  ptrObjName("test-svc"),
						}},
					},
				},
			},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gwtypes.ObjectName("test-svc"),
									Kind:      kindPtr("Service"),
									Group:     groupPtr("core"),
									Namespace: nsPtr("other-ns"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: true,
			wantStatus:            metav1.ConditionTrue,
			wantReason:            string(gwtypes.RouteReasonResolvedRefs),
			wantMsgPart:           "All references resolved",
		},
		{
			name:       "multiple refs, first fails",
			clientObjs: []client.Object{serviceDefault},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								BackendRef: gwtypes.BackendRef{
									BackendObjectReference: gwtypes.BackendObjectReference{
										Name:  gwtypes.ObjectName("not-found"),
										Kind:  kindPtr("Service"),
										Group: groupPtr("core"),
									},
								},
							},
							{
								BackendRef: gwtypes.BackendRef{
									BackendObjectReference: gwtypes.BackendObjectReference{
										Name:  gwtypes.ObjectName("svc"),
										Kind:  kindPtr("Service"),
										Group: groupPtr("core"),
									},
								},
							},
						},
					}},
				},
			},
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionFalse,
			wantReason:            string(gwtypes.RouteReasonBackendNotFound),
			wantMsgPart:           "not found",
		},
		{
			name:       "empty group uses implicit core",
			clientObjs: []client.Object{serviceDefault},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:  gwtypes.ObjectName("svc"),
									Kind:  kindPtr("Service"),
									Group: groupPtr(""), // Empty group
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: false,
			wantStatus:            metav1.ConditionTrue,
			wantReason:            string(gwtypes.RouteReasonResolvedRefs),
			wantMsgPart:           "All references resolved",
		},
	}

	// Test cases that expect errors need special handling.
	errorTests := []struct {
		name                  string
		clientObjs            []client.Object
		route                 *gwtypes.HTTPRoute
		referenceGrantEnabled bool
		clientFactory         func([]client.Object) client.Client
		wantError             bool
		wantErrorContains     string
	}{
		{
			name:       "error listing reference grants",
			clientObjs: []client.Object{serviceOtherNS},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:      gwtypes.ObjectName("test-svc"),
									Kind:      kindPtr("Service"),
									Group:     groupPtr("core"),
									Namespace: nsPtr("other-ns"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: true,
			clientFactory: func(objs []client.Object) client.Client {
				s := runtime.NewScheme()
				_ = corev1.AddToScheme(s)
				_ = gatewayv1beta1.Install(s)
				return &erroringClient{
					Client:                     fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build(),
					errorOnListReferenceGrants: true,
				}
			},
			wantError:         true,
			wantErrorContains: "failed to list ReferenceGrants",
		},
		{
			name:       "error getting service",
			clientObjs: []client.Object{},
			route: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "route"},
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						BackendRefs: []gwtypes.HTTPBackendRef{{
							BackendRef: gwtypes.BackendRef{
								BackendObjectReference: gwtypes.BackendObjectReference{
									Name:  gwtypes.ObjectName("svc"),
									Kind:  kindPtr("Service"),
									Group: groupPtr("core"),
								},
							},
						}},
					}},
				},
			},
			referenceGrantEnabled: false,
			clientFactory: func(objs []client.Object) client.Client {
				s := runtime.NewScheme()
				_ = corev1.AddToScheme(s)
				_ = gatewayv1beta1.Install(s)
				return &erroringClient{
					Client:            fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build(),
					errorOnGetService: true,
				}
			},
			wantError:         true,
			wantErrorContains: "failed to get BackendRef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = gatewayv1beta1.Install(s)

			// Create fake client
			clientBuilder := fake.NewClientBuilder().WithScheme(s).WithObjects(tt.clientObjs...)

			cl := clientBuilder.Build()

			cond, err := BuildResolvedRefsCondition(ctx, logger, cl, tt.route, tt.referenceGrantEnabled)
			require.NoError(t, err)
			require.NotNil(t, cond)

			if cond.Status != tt.wantStatus {
				t.Errorf("got status %v, want %v", cond.Status, tt.wantStatus)
			}
			if cond.Reason != tt.wantReason {
				t.Errorf("got reason %v, want %v", cond.Reason, tt.wantReason)
			}
			if tt.wantMsgPart != "" && !strings.Contains(cond.Message, tt.wantMsgPart) {
				t.Errorf("message %q does not contain %q", cond.Message, tt.wantMsgPart)
			}
		})
	}

	// Run error tests that expect the function to return an error.
	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = gatewayv1beta1.Install(s)

			cl := tt.clientFactory(tt.clientObjs)

			cond, err := BuildResolvedRefsCondition(ctx, logger, cl, tt.route, tt.referenceGrantEnabled)
			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, cond)
				if tt.wantErrorContains != "" {
					require.Contains(t, err.Error(), tt.wantErrorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, cond)
			}
		})
	}
}

func TestCheckReferenceGrant(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name             string
		bRef             *gwtypes.HTTPBackendRef
		routeNamespace   string
		clientObjs       []client.Object
		wantPermitted    bool
		wantFound        bool
		wantError        bool
		wantErrorMessage string
	}{
		{
			name: "nil namespace returns error",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name: "service",
					},
				},
			},
			routeNamespace:   "default",
			wantPermitted:    false,
			wantFound:        false,
			wantError:        true,
			wantErrorMessage: "backendRef namespace is not set for cross-namespace reference check, name service",
		},
		{
			name: "empty namespace returns error",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr(""),
					},
				},
			},
			routeNamespace:   "default",
			wantPermitted:    false,
			wantFound:        false,
			wantError:        true,
			wantErrorMessage: "backendRef namespace is not set for cross-namespace reference check, name service",
		},
		{
			name: "no reference grants found",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs:     []client.Object{},
			wantPermitted:  false,
			wantFound:      false,
			wantError:      false,
		},
		{
			name: "reference grants exist but none permit the reference",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("different-ns"), // Different namespace.
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  gatewayv1.Kind("Service"),
								Name:  ptrObjName("service"),
							},
						},
					},
				},
			},
			wantPermitted: false,
			wantFound:     true,
			wantError:     false,
		},
		{
			name: "reference grant permits the reference",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("source-ns"),
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  gatewayv1.Kind("Service"),
								Name:  ptrObjName("service"),
							},
						},
					},
				},
			},
			wantPermitted: true,
			wantFound:     true,
			wantError:     false,
		},
		{
			name: "reference grant permits with wildcard service name",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "any-service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("source-ns"),
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  gatewayv1.Kind("Service"),
								Name:  nil, // Wildcard - allows any service name.
							},
						},
					},
				},
			},
			wantPermitted: true,
			wantFound:     true,
			wantError:     false,
		},
		{
			name: "multiple grants, first doesn't match but second does",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("wrong-ns"),
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  gatewayv1.Kind("Service"),
								Name:  ptrObjName("service"),
							},
						},
					},
				},
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-2",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("source-ns"),
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  gatewayv1.Kind("Service"),
								Name:  ptrObjName("service"),
							},
						},
					},
				},
			},
			wantPermitted: true,
			wantFound:     true,
			wantError:     false,
		},
		{
			name: "backend ref with explicit group",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
						Group:     groupPtr("core"),
					},
				},
			},
			routeNamespace: "source-ns",
			clientObjs: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant-1",
						Namespace: "target-ns",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      gatewayv1.Kind("HTTPRoute"),
								Namespace: gatewayv1.Namespace("source-ns"),
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  gatewayv1.Kind("Service"),
								Name:  ptrObjName("service"),
							},
						},
					},
				},
			},
			wantPermitted: true,
			wantFound:     true,
			wantError:     false,
		},
	}

	// Test with error client.
	errorTests := []struct {
		name             string
		bRef             *gwtypes.HTTPBackendRef
		routeNamespace   string
		wantError        bool
		wantErrorMessage string
	}{
		{
			name: "error listing reference grants",
			bRef: &gwtypes.HTTPBackendRef{
				BackendRef: gwtypes.BackendRef{
					BackendObjectReference: gwtypes.BackendObjectReference{
						Name:      "service",
						Namespace: nsPtr("target-ns"),
						Kind:      kindPtr("Service"),
					},
				},
			},
			routeNamespace:   "source-ns",
			wantError:        true,
			wantErrorMessage: "failed to list ReferenceGrants in namespace target-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = gatewayv1beta1.Install(s)

			cl := fake.NewClientBuilder().WithScheme(s).WithObjects(tt.clientObjs...).Build()

			permitted, found, err := CheckReferenceGrant(ctx, cl, tt.bRef, tt.routeNamespace)

			if tt.wantError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrorMessage)
				require.False(t, permitted)
				require.False(t, found)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantPermitted, permitted, "permitted mismatch")
				require.Equal(t, tt.wantFound, found, "found mismatch")
			}
		})
	}

	// Test error cases with error client.
	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = gatewayv1beta1.Install(s)

			cl := &erroringClient{
				Client:                     fake.NewClientBuilder().WithScheme(s).Build(),
				errorOnListReferenceGrants: true,
			}

			permitted, found, err := CheckReferenceGrant(ctx, cl, tt.bRef, tt.routeNamespace)

			if tt.wantError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrorMessage)
				require.False(t, permitted)
				require.False(t, found)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func groupPtr(s string) *gatewayv1.Group                                     { g := gatewayv1.Group(s); return &g }
func kindPtr(s string) *gatewayv1.Kind                                       { k := gatewayv1.Kind(s); return &k }
func nsPtr(s string) *gatewayv1.Namespace                                    { n := gatewayv1.Namespace(s); return &n }
func sectionPtr(s string) *gatewayv1.SectionName                             { sec := gatewayv1.SectionName(s); return &sec }
func portPtr(i int32) *gatewayv1.PortNumber                                  { p := gatewayv1.PortNumber(i); return &p }
func ptrObjName(s string) *gwtypes.ObjectName                                { n := gwtypes.ObjectName(s); return &n }
func fromNamespacesPtr(v gatewayv1.FromNamespaces) *gatewayv1.FromNamespaces { return &v }
