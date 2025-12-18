package refs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	hybridgatewayerrors "github.com/kong/kong-operator/controller/hybridgateway/errors"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

func Test_GetSupportedGatewayForParentRef(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	controllerName := vars.DefaultControllerName
	vars.SetControllerName(controllerName)

	s := runtime.NewScheme()
	_ = gatewayv1.Install(s)

	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-gateway",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "my-class",
		},
	}
	gateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gwtypes.GroupName,
		Version: "v1",
		Kind:    "Gateway",
	})
	gatewayClass := &gwtypes.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-class",
		},
		Spec: gwtypes.GatewayClassSpec{
			ControllerName: gwtypes.GatewayController(controllerName),
		},
	}
	gatewayClass.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gwtypes.GroupName,
		Version: "v1",
		Kind:    "GatewayClass",
	})

	tests := []struct {
		name            string
		pRef            gwtypes.ParentReference
		routeNS         string
		objs            []client.Object
		controllerVal   string
		interceptorFunc interceptor.Funcs
		wantErr         error
		wantNil         bool
	}{
		{
			name:    "unsupported kind",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("OtherKind"), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, gatewayClass},
			wantErr: hybridgatewayerrors.ErrUnsupportedKind,
		},
		{
			name:    "unsupported group",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr("other.group"), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, gatewayClass},
			wantErr: hybridgatewayerrors.ErrUnsupportedGroup,
		},
		{
			name:    "gateway not found",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "notfound"},
			routeNS: "default",
			objs:    []client.Object{},
			wantErr: hybridgatewayerrors.ErrNoGatewayFound,
		},
		{
			name:    "gateway class not found",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway},
			wantErr: hybridgatewayerrors.ErrNoGatewayClassFound,
		},
		{
			name:    "gateway class wrong controller",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs: []client.Object{gateway, &gwtypes.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{Name: "my-class"},
				Spec:       gwtypes.GatewayClassSpec{ControllerName: "wrong-controller"},
			}},
			wantErr: hybridgatewayerrors.ErrNoGatewayController,
		},
		{
			name:    "gateway class empty controller",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs: []client.Object{gateway, &gwtypes.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{Name: "my-class"},
				Spec:       gwtypes.GatewayClassSpec{ControllerName: ""},
			}},
			wantErr: hybridgatewayerrors.ErrNoGatewayController,
		},
		{
			name:    "supported parent ref",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, gatewayClass},
			wantNil: false,
		},
		{
			name:    "gateway get generic error",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, gatewayClass},
			interceptorFunc: interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "my-gateway" && key.Namespace == "default" {
						return fmt.Errorf("generic gateway error")
					}
					return client.Get(ctx, key, obj, opts...)
				},
			},
			wantErr: fmt.Errorf("failed to get gateway for ParentRef"),
		},
		{
			name:    "gatewayclass get generic error",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, gatewayClass},
			interceptorFunc: interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "my-class" && key.Namespace == "" {
						return fmt.Errorf("generic gatewayclass error")
					}
					return client.Get(ctx, key, obj, opts...)
				},
			},
			wantErr: fmt.Errorf("failed to get gatewayClass for ParentRef"),
		},
		{
			name:    "parentRef with custom namespace",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway", Namespace: nsPtr("custom-ns")},
			routeNS: "default",
			objs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "custom-ns",
						Name:      "my-gateway",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "my-class",
					},
				},
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: gwtypes.GatewayController(vars.DefaultControllerName),
					},
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(s).WithObjects(tt.objs...)
			if tt.interceptorFunc.Get != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(tt.interceptorFunc)
			}
			cl := clientBuilder.Build()
			gw, err := GetSupportedGatewayForParentRef(ctx, logger, cl, tt.pRef, tt.routeNS)
			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound) || errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound) || errors.Is(err, hybridgatewayerrors.ErrNoGatewayController) || errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind) || errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup) {
					// Specific error type matches
					require.ErrorIs(t, err, tt.wantErr)
					return
				}
				require.Contains(t, err.Error(), tt.wantErr.Error())
				return
			}
			if tt.wantNil {
				require.Nil(t, gw)
			} else {
				require.NotNil(t, gw)
			}
		})
	}
}

func groupPtr(s string) *gatewayv1.Group  { g := gatewayv1.Group(s); return &g }
func kindPtr(s string) *gatewayv1.Kind    { k := gatewayv1.Kind(s); return &k }
func nsPtr(s string) *gatewayv1.Namespace { n := gatewayv1.Namespace(s); return &n }
