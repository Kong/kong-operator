package gateway

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGetParentStatuses(t *testing.T) {
	t.Run("HTTPRoute", func(t *testing.T) {
		tests := []struct {
			name  string
			route *gwtypes.HTTPRoute
			want  map[string]*gwtypes.RouteParentStatus
		}{
			{
				name: "basic",
				route: &gwtypes.HTTPRoute{
					Name:      uuid.NewString(),
					Namespace: uuid.NewString(),
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("group").
										Kind("kind").
										Namespace("namespace").
										Name("name").
										SectionName("section1").
										Build(),
								},
							},
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/name/section1": {
						ParentRef: builder.NewParentReference().
							Group("group").
							Kind("kind").
							Namespace("namespace").
							Name("name").
							SectionName("section1").
							Build(),
					},
				},
			},
			{
				name: "nil namespace",
				route: &gwtypes.HTTPRoute{
					Name:      uuid.NewString(),
					Namespace: "object-namespace",
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("group").
										Kind("kind").
										Name("name").
										SectionName("section1").
										Build(),
								},
							},
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"object-namespace/name/section1": {
						ParentRef: builder.NewParentReference().
							Group("group").
							Kind("kind").
							Name("name").
							SectionName("section1").
							Build(),
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, getParentStatuses(tt.route, tt.route.Status.Parents))
			})
		}
	})

	t.Run("UDPRoute", func(t *testing.T) {
		tests := []struct {
			name  string
			route *gwtypes.UDPRoute
			want  map[string]*gwtypes.RouteParentStatus
		}{
			{
				name: "basic",
				route: &gwtypes.UDPRoute{
					Name:      uuid.NewString(),
					Namespace: uuid.NewString(),
					Status: gwtypes.UDPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("group").
										Kind("kind").
										Namespace("namespace").
										Name("name").
										Build(),
								},
							},
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/name": {
						ParentRef: builder.NewParentReference().
							Group("group").
							Kind("kind").
							Namespace("namespace").
							Name("name").
							Build(),
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, getParentStatuses(tt.route, tt.route.Status.Parents))
			})
		}
	})

	t.Run("TCPRoute", func(t *testing.T) {
		tests := []struct {
			name  string
			route *gwtypes.TCPRoute
			want  map[string]*gwtypes.RouteParentStatus
		}{
			{
				name: "basic",
				route: &gwtypes.TCPRoute{
					Name:      uuid.NewString(),
					Namespace: uuid.NewString(),
					Status: gwtypes.TCPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("group").
										Kind("kind").
										Namespace("namespace").
										Name("name").
										Build(),
								},
							},
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/name": {
						ParentRef: builder.NewParentReference().
							Group("group").
							Kind("kind").
							Namespace("namespace").
							Name("name").
							Build(),
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, getParentStatuses(tt.route, tt.route.Status.Parents))
			})
		}
	})

	t.Run("TLSRoute", func(t *testing.T) {
		tests := []struct {
			name  string
			route *gwtypes.TLSRoute
			want  map[string]*gwtypes.RouteParentStatus
		}{
			{
				name: "basic",
				route: &gwtypes.TLSRoute{
					Name:      uuid.NewString(),
					Namespace: uuid.NewString(),
					Status: gwtypes.TLSRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("group").
										Kind("kind").
										Namespace("namespace").
										Name("name").
										Build(),
								},
							},
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/name": {
						ParentRef: builder.NewParentReference().
							Group("group").
							Kind("kind").
							Namespace("namespace").
							Name("name").
							Build(),
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, getParentStatuses(tt.route, tt.route.Status.Parents))
			})
		}
	})
}

func TestParentStatusesForRoute(t *testing.T) {
	t.Run("HTTPRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			route    *gwtypes.HTTPRoute
			statuses []gwtypes.RouteParentStatus
			gateways []supportedGatewayWithCondition
			want     map[string]*gwtypes.RouteParentStatus
			changed  bool
		}{
			{
				name: "no Programmed condition on a route, update and return true",
				route: &gwtypes.HTTPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										SectionName("section1").
										Build(),
									ControllerName: GetControllerName(),
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
						listenerName: "section1",
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1/section1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							SectionName("section1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: true,
			},
			{
				name: "Programmed condition is as exptected on a route, don't update and return false",
				route: &gwtypes.HTTPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.HTTPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										SectionName("section1").
										Build(),
									ControllerName: GetControllerName(),
									Conditions: []metav1.Condition{
										newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
									},
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
						listenerName: "section1",
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1/section1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							SectionName("section1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, changed := parentStatusesForRoute(tt.route, tt.route.Status.Parents, tt.gateways...)
				ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
				if !cmp.Equal(tt.want, got, ignoreLastTransitionTime) {
					assert.Equal(t, tt.want, got)
				}
				assert.Equal(t, tt.changed, changed)
			})
		}
	})

	t.Run("UDPRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			route    *gwtypes.UDPRoute
			statuses []gwtypes.RouteParentStatus
			gateways []supportedGatewayWithCondition
			want     map[string]*gwtypes.RouteParentStatus
			changed  bool
		}{
			{
				name: "no Programmed condition on a route, update and return true",
				route: &gwtypes.UDPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.UDPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: true,
			},
			{
				name: "Programmed condition is as exptected on a route, don't update and return false",
				route: &gwtypes.UDPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.UDPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
									Conditions: []metav1.Condition{
										newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
									},
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, changed := parentStatusesForRoute(tt.route, tt.route.Status.Parents, tt.gateways...)
				ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
				if !cmp.Equal(tt.want, got, ignoreLastTransitionTime) {
					assert.Equal(t, tt.want, got)
				}
				assert.Equal(t, tt.changed, changed)
			})
		}
	})

	t.Run("TCPRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			route    *gwtypes.TCPRoute
			statuses []gwtypes.RouteParentStatus
			gateways []supportedGatewayWithCondition
			want     map[string]*gwtypes.RouteParentStatus
			changed  bool
		}{
			{
				name: "no Programmed condition on a route, update and return true",
				route: &gwtypes.TCPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.TCPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: true,
			},
			{
				name: "Programmed condition is as exptected on a route, don't update and return false",
				route: &gwtypes.TCPRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.TCPRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
									Conditions: []metav1.Condition{
										newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
									},
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, changed := parentStatusesForRoute(tt.route, tt.route.Status.Parents, tt.gateways...)
				ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
				if !cmp.Equal(tt.want, got, ignoreLastTransitionTime) {
					assert.Equal(t, tt.want, got)
				}
				assert.Equal(t, tt.changed, changed)
			})
		}
	})

	t.Run("TLSRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			route    *gwtypes.TLSRoute
			statuses []gwtypes.RouteParentStatus
			gateways []supportedGatewayWithCondition
			want     map[string]*gwtypes.RouteParentStatus
			changed  bool
		}{
			{
				name: "no Programmed condition on a route, update and return true",
				route: &gwtypes.TLSRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.TLSRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: true,
			},
			{
				name: "Programmed condition is as exptected on a route, don't update and return false",
				route: &gwtypes.TLSRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.TLSRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										Build(),
									ControllerName: GetControllerName(),
									Conditions: []metav1.Condition{
										newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
									},
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, changed := parentStatusesForRoute(tt.route, tt.route.Status.Parents, tt.gateways...)
				ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
				if !cmp.Equal(tt.want, got, ignoreLastTransitionTime) {
					assert.Equal(t, tt.want, got)
				}
				assert.Equal(t, tt.changed, changed)
			})
		}
	})

	t.Run("GRPCRoute", func(t *testing.T) {
		tests := []struct {
			name     string
			route    *gwtypes.GRPCRoute
			statuses []gwtypes.RouteParentStatus
			gateways []supportedGatewayWithCondition
			want     map[string]*gwtypes.RouteParentStatus
			changed  bool
		}{
			{
				name: "no Programmed condition on a route, update and return true",
				route: &gwtypes.GRPCRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.GRPCRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										SectionName("section1").
										Build(),
									ControllerName: GetControllerName(),
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
						listenerName: "section1",
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1/section1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							SectionName("section1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: true,
			},
			{
				name: "Programmed condition is as exptected on a route, don't update and return false",
				route: &gwtypes.GRPCRoute{
					Name:       uuid.NewString(),
					Namespace:  uuid.NewString(),
					Generation: 7,
					Status: gwtypes.GRPCRouteStatus{
						RouteStatus: gwtypes.RouteStatus{
							Parents: []gwtypes.RouteParentStatus{
								{
									ParentRef: builder.NewParentReference().
										Group("gateway.networking.k8s.io").
										Kind("Gateway").
										Namespace("namespace").
										Name("gateway1").
										SectionName("section1").
										Build(),
									ControllerName: GetControllerName(),
									Conditions: []metav1.Condition{
										newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
									},
								},
							},
						},
					},
				},
				gateways: []supportedGatewayWithCondition{
					{
						gateway: &gwtypes.Gateway{
							Name:      "gateway1",
							Namespace: "namespace",
						},
						condition: metav1.Condition{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
							Reason: "Ready",
						},
						listenerName: "section1",
					},
				},
				want: map[string]*gwtypes.RouteParentStatus{
					"namespace/gateway1/section1": {
						ParentRef: builder.NewParentReference().
							Group("gateway.networking.k8s.io").
							Kind("Gateway").
							Namespace("namespace").
							Name("gateway1").
							SectionName("section1").
							Build(),
						ControllerName: GetControllerName(),
						Conditions: []metav1.Condition{
							newCondition("Ready", metav1.ConditionTrue, "Ready", 7),
						},
					},
				},
				changed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, changed := parentStatusesForRoute(tt.route, tt.route.Status.Parents, tt.gateways...)
				ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
				if !cmp.Equal(tt.want, got, ignoreLastTransitionTime) {
					assert.Equal(t, tt.want, got)
				}
				assert.Equal(t, tt.changed, changed)
			})
		}
	})
}

func TestInitializeParentStatusesWithProgrammedCondition(t *testing.T) {
	foreignParentStatus := gwtypes.RouteParentStatus{
		ParentRef: builder.NewParentReference().
			Group("gateway.networking.k8s.io").
			Kind("Gateway").
			Namespace("namespace").
			Name("unmanaged-gateway").
			Build(),
		ControllerName: "gateway.networking.k8s.io/stale-controller",
		Conditions: []metav1.Condition{
			newCondition(string(gwtypes.RouteConditionAccepted), metav1.ConditionTrue, string(gwtypes.RouteReasonAccepted), 7),
		},
	}
	ownParentStatus := gwtypes.RouteParentStatus{
		ParentRef: builder.NewParentReference().
			Group("gateway.networking.k8s.io").
			Kind("Gateway").
			Namespace("namespace").
			Name("gateway1").
			Build(),
		ControllerName: GetControllerName(),
	}

	t.Run("foreign parent status entries are not modified", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			Name:       uuid.NewString(),
			Namespace:  "namespace",
			Generation: 7,
		}
		foreign := foreignParentStatus
		parentStatuses := map[string]*gwtypes.RouteParentStatus{
			"namespace/unmanaged-gateway": &foreign,
			"namespace/gateway1":          &ownParentStatus,
		}

		changed := initializeParentStatusesWithProgrammedCondition(route, parentStatuses)

		assert.True(t, changed)
		// The foreign entry must be left exactly as seeded.
		assert.Equal(t, foreignParentStatus, foreign)
		// Our own entry gets the Programmed=Unknown condition.
		assert.Len(t, ownParentStatus.Conditions, 1)
		assert.Equal(t, string(ConditionTypeProgrammed), ownParentStatus.Conditions[0].Type)
		assert.Equal(t, metav1.ConditionUnknown, ownParentStatus.Conditions[0].Status)
	})

	t.Run("no changes when only a foreign parent status entry exists", func(t *testing.T) {
		route := &gwtypes.HTTPRoute{
			Name:       uuid.NewString(),
			Namespace:  "namespace",
			Generation: 7,
		}
		foreign := foreignParentStatus
		parentStatuses := map[string]*gwtypes.RouteParentStatus{
			"namespace/unmanaged-gateway": &foreign,
		}

		changed := initializeParentStatusesWithProgrammedCondition(route, parentStatuses)

		assert.False(t, changed)
		assert.Equal(t, foreignParentStatus, foreign)
	})
}

func TestGatewayParentStatusForRoutePropagatesConditionMessage(t *testing.T) {
	route := &gwtypes.HTTPRoute{
		Name:       "route",
		Namespace:  "namespace",
		Generation: 7,
	}
	message := "rules[0].retry.codes is unsupported: Kong Gateway cannot retry on HTTP response status codes"
	parentStatus := gatewayParentStatusForRoute(route, supportedGatewayWithCondition{
		gateway: &gwtypes.Gateway{
			Name:      "gateway",
			Namespace: "namespace",
		},
		condition: metav1.Condition{
			Type:    string(gwtypes.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gwtypes.RouteReasonUnsupportedValue),
			Message: message,
		},
	})

	if assert.Len(t, parentStatus.Conditions, 1) {
		assert.Equal(t, message, parentStatus.Conditions[0].Message)
	}
}
