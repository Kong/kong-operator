package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

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
			kind:  new(gwtypes.Kind("Service")),
			want:  true,
		},
		{
			name:  "empty group, Service kind",
			group: new(gwtypes.Group("")),
			kind:  new(gwtypes.Kind("Service")),
			want:  true,
		},
		{
			name:  "core group, Service kind",
			group: new(gwtypes.Group("core")),
			kind:  new(gwtypes.Kind("Service")),
			want:  true,
		},
		{
			name:  "corev1 group, Service kind",
			group: new(gwtypes.Group("corev1")),
			kind:  new(gwtypes.Kind("Service")),
			want:  false,
		},
		{
			name:  "v1 group, Service kind",
			group: new(gwtypes.Group("v1")),
			kind:  new(gwtypes.Kind("Service")),
			want:  false,
		},
		{
			name:  "unsupported group, Service kind",
			group: new(gwtypes.Group("foo")),
			kind:  new(gwtypes.Kind("Service")),
			want:  false,
		},
		{
			name:  "core group, unsupported kind",
			group: new(gwtypes.Group("core")),
			kind:  new(gwtypes.Kind("Deployment")),
			want:  false,
		},
		{
			name:  "nil kind defaults to Service",
			group: new(gwtypes.Group("core")),
			kind:  nil,
			want:  true,
		},
		{
			name:  "empty kind defaults to Service",
			group: new(gwtypes.Group("core")),
			kind:  new(gwtypes.Kind("")),
			want:  true,
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

func TestHTTPBackendRefsToBackendRefs(t *testing.T) {
	port80 := gatewayv1.PortNumber(80)
	port443 := gatewayv1.PortNumber(443)
	weight := int32(50)
	otherNS := gatewayv1.Namespace("other-namespace")

	tests := []struct {
		name     string
		input    []gatewayv1.HTTPBackendRef
		expected []gwtypes.BackendRef
	}{
		{
			name:     "nil input returns empty slice",
			input:    nil,
			expected: []gwtypes.BackendRef{},
		},
		{
			name:     "empty input returns empty slice",
			input:    []gatewayv1.HTTPBackendRef{},
			expected: []gwtypes.BackendRef{},
		},
		{
			name: "single ref extracted",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
			},
		},
		{
			name: "multiple refs extracted in order",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}}},
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port443}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-a", Port: &port80}},
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-b", Port: &port443}},
			},
		},
		{
			name: "HTTP filters are stripped, only BackendRef preserved",
			input: []gatewayv1.HTTPBackendRef{
				{
					BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-filtered", Port: &port80}},
					Filters: []gatewayv1.HTTPRouteFilter{
						{Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier},
					},
				},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-filtered", Port: &port80}},
			},
		},
		{
			name: "cross-namespace ref preserved",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other",
					Port:      &port80,
					Namespace: &otherNS,
				}}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{
					Name:      "svc-other",
					Port:      &port80,
					Namespace: &otherNS,
				}},
			},
		},
		{
			name: "weight preserved",
			input: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-weighted", Port: &port80},
					Weight:                 &weight,
				}},
			},
			expected: []gwtypes.BackendRef{
				{BackendObjectReference: gatewayv1.BackendObjectReference{Name: "svc-weighted", Port: &port80}, Weight: &weight},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HTTPBackendRefsToBackendRefs(tt.input)
			require.Len(t, got, len(tt.expected))
			for i := range tt.expected {
				assert.Equal(t, tt.expected[i].Name, got[i].Name)
				assert.Equal(t, tt.expected[i].Namespace, got[i].Namespace)
				assert.Equal(t, tt.expected[i].Port, got[i].Port)
				assert.Equal(t, tt.expected[i].Weight, got[i].Weight)
			}
		})
	}
}
