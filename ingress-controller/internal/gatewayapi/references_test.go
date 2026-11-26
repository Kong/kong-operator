package gatewayapi

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestGetPermittedForReferenceGrantFrom(t *testing.T) {
	grants := []*gwtypes.ReferenceGrant{
		{
			Name:      uuid.NewString(),
			Namespace: "fitrat",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("garbage"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("behbudiy"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("qodiriy"),
					},
				},
				To: []gwtypes.ReferenceGrantTo{
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("GrantOne"),
					},
				},
			},
		},
		{
			Name:      uuid.NewString(),
			Namespace: "cholpon",
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("UDPRoute"),
						Namespace: gwtypes.Namespace("behbudiy"),
					},
					{
						Group:     gwtypes.Group("gateway.networking.k8s.io"),
						Kind:      gwtypes.Kind("TCPRoute"),
						Namespace: gwtypes.Namespace("qodiriy"),
					},
				},
				To: []gwtypes.ReferenceGrantTo{
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("GrantTwo"),
					},
				},
			},
		},
	}
	tests := []struct {
		msg    string
		from   gwtypes.ReferenceGrantFrom
		result map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo
	}{
		{
			msg: "no matches whatsoever",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("invalid.example"),
				Kind:      gwtypes.Kind("invalid"),
				Namespace: gwtypes.Namespace("invalid"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{},
		},
		{
			msg: "non-matching namespace",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("gateway.networking.k8s.io"),
				Kind:      gwtypes.Kind("UDPRoute"),
				Namespace: gwtypes.Namespace("niyazi"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{},
		},
		{
			msg: "non-matching kind",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("gateway.networking.k8s.io"),
				Kind:      gwtypes.Kind("TLSRoute"),
				Namespace: gwtypes.Namespace("behbudiy"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{},
		},
		{
			msg: "non-matching group",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("invalid.example"),
				Kind:      gwtypes.Kind("UDPRoute"),
				Namespace: gwtypes.Namespace("behbudiy"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{},
		},
		{
			msg: "single match",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("gateway.networking.k8s.io"),
				Kind:      gwtypes.Kind("UDPRoute"),
				Namespace: gwtypes.Namespace("behbudiy"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				"cholpon": {
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("GrantTwo"),
					},
				},
			},
		},
		{
			msg: "multiple matches",
			from: gwtypes.ReferenceGrantFrom{
				Group:     gwtypes.Group("gateway.networking.k8s.io"),
				Kind:      gwtypes.Kind("TCPRoute"),
				Namespace: gwtypes.Namespace("qodiriy"),
			},
			result: map[gwtypes.Namespace][]gwtypes.ReferenceGrantTo{
				"cholpon": {
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("GrantTwo"),
					},
				},
				"fitrat": {
					{
						Group: gwtypes.Group(""),
						Kind:  gwtypes.Kind("GrantOne"),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			result := GetPermittedForReferenceGrantFrom(logr.Discard(), tt.from, grants)
			assert.Equal(t, tt.result, result)
		})
	}
}
