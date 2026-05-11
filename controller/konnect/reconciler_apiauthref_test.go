package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// populateGVKOnGet returns an interceptor that sets the TypeMeta on objects
// retrieved via Get. The fake client strips TypeMeta during deserialization;
// the real Kubernetes client preserves it. Without TypeMeta, callers that rely
// on `obj.GetObjectKind().GroupVersionKind()` see an empty GVK and any code
// path comparing GVKs (e.g. KongReferenceGrant lookups) fails to match.
func populateGVKOnGet(scheme *runtime.Scheme) interceptor.Funcs {
	return interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if err := c.Get(ctx, key, obj, opts...); err != nil {
				return err
			}
			gvk, err := apiutil.GVKForObject(obj, scheme)
			if err != nil {
				return err
			}
			obj.GetObjectKind().SetGroupVersionKind(gvk)
			return nil
		},
	}
}

// TestGetCPAuthRefForRef covers all branches of getCPAuthRefForRef:
//   - CP missing: GetCPForRef returns an error and it is propagated.
//   - Same-namespace authRef (Namespace nil): the returned namespace is the CP's
//     namespace, regardless of which namespace the caller passed in.
//   - Same-namespace authRef (Namespace explicitly equal to the CP's): the
//     cross-namespace branch is skipped (covers the `!= cpNamespace` condition).
//   - Cross-namespace authRef without a grant: ReferenceNotGrantedError is returned.
//   - Cross-namespace authRef with a valid grant: the returned namespace is the
//     authRef's namespace.
func TestGetCPAuthRefForRef(t *testing.T) {
	const (
		cpName        = "cp"
		cpNamespace   = "cp-ns"
		authName      = "konnect-api-auth"
		authNamespace = "auth-ns"
		callerNs      = "caller-ns"
	)

	cpRef := commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
			Name:      cpName,
			Namespace: cpNamespace,
		},
	}

	makeCP := func(authRefNamespace *string) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			TypeMeta: metav1.TypeMeta{
				APIVersion: konnectv1alpha2.GroupVersion.String(),
				Kind:       "KonnectGatewayControlPlane",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cpName,
				Namespace: cpNamespace,
			},
			Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
					APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
						Name:      authName,
						Namespace: authRefNamespace,
					},
				},
			},
		}
	}

	grant := &configurationv1alpha1.KongReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cp-to-auth",
			Namespace: authNamespace,
		},
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     configurationv1alpha1.Group(konnectv1alpha2.GroupVersion.Group),
					Kind:      "KonnectGatewayControlPlane",
					Namespace: configurationv1alpha1.Namespace(cpNamespace),
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: configurationv1alpha1.Group(konnectv1alpha2.GroupVersion.Group),
					Kind:  "KonnectAPIAuthConfiguration",
				},
			},
		},
	}

	testCases := []struct {
		name                string
		objects             []client.Object
		callerNamespace     string
		wantNN              types.NamespacedName
		wantErrorContains   string
		wantNotGrantedError bool
	}{
		{
			name:              "returns error when CP does not exist",
			callerNamespace:   callerNs,
			wantErrorContains: "does not exist",
		},
		{
			name:    "same-namespace authRef (Namespace nil) resolves to CP's namespace, ignoring caller namespace",
			objects: []client.Object{makeCP(nil)},
			// caller-ns differs from cp-ns;
			callerNamespace: callerNs,
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: cpNamespace,
			},
		},
		{
			name:            "same-namespace authRef (Namespace explicitly equal to CP's) skips cross-namespace branch",
			objects:         []client.Object{makeCP(new(cpNamespace))},
			callerNamespace: callerNs,
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: cpNamespace,
			},
		},
		{
			name:                "cross-namespace authRef without grant returns ReferenceNotGrantedError",
			objects:             []client.Object{makeCP(new(authNamespace))},
			callerNamespace:     callerNs,
			wantErrorContains:   "is not granted",
			wantNotGrantedError: true,
		},
		{
			name:            "cross-namespace authRef with valid grant returns authRef's namespace",
			objects:         []client.Object{makeCP(new(authNamespace)), grant},
			callerNamespace: callerNs,
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: authNamespace,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := scheme.Get()
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tc.objects...).
				WithInterceptorFuncs(populateGVKOnGet(s)).
				Build()

			nn, err := getCPAuthRefForRef(t.Context(), cl, cpRef, tc.callerNamespace)

			if tc.wantErrorContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrorContains)
				if tc.wantNotGrantedError {
					var notGranted *crossnamespace.ReferenceNotGrantedError
					require.ErrorAs(t, err, &notGranted)
				}
				require.Equal(t, types.NamespacedName{}, nn)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantNN, nn)
		})
	}
}

// TestGetAPIAuthRefNN_ServiceRef covers the serviceRef branch of GetAPIAuthRefNN.
// It is the path that resolves a KongRoute -> KongService -> CP -> auth chain.
//
// The branch must:
//   - look up the KongService using the namespace from `serviceRef.namespace` (falling
//     back to the route's namespace when not set), not the route's namespace unconditionally.
//   - call getCPAuthRefForRef with the resolved service's namespace, not the route's.
func TestGetAPIAuthRefNN_ServiceRef(t *testing.T) {
	const (
		routeNs   = "route-ns"
		serviceNs = "svc-ns"
		cpName    = "cp"
		svcName   = "cross-ns-service"
		authName  = "konnect-api-auth"
	)

	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cpName,
			Namespace: serviceNs,
		},
		Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: authName,
				},
			},
		},
	}

	svc := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: serviceNs,
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name:      cpName,
					Namespace: serviceNs,
				},
			},
		},
	}

	svcNoCPRef := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: serviceNs,
		},
	}

	makeRoute := func(svcRefNamespace *string) *configurationv1alpha1.KongRoute {
		return &configurationv1alpha1.KongRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "route",
				Namespace: routeNs,
			},
			Spec: configurationv1alpha1.KongRouteSpec{
				ServiceRef: &configurationv1alpha1.ServiceRef{
					Type: configurationv1alpha1.ServiceRefNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{
						Name:      svcName,
						Namespace: svcRefNamespace,
					},
				},
			},
		}
	}

	testCases := []struct {
		name              string
		route             *configurationv1alpha1.KongRoute
		objects           []client.Object
		wantNN            types.NamespacedName
		wantErrorContains string
	}{
		{
			name:  "cross-namespace serviceRef resolves auth from service's namespace",
			route: makeRoute(new(serviceNs)),
			objects: []client.Object{
				// Service lives in serviceNs, not the route's namespace. Verifies that
				// the lookup uses serviceRef.namespace rather than the route's namespace.
				svc, cp,
			},
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: serviceNs,
			},
		},
		{
			name:  "same-namespace serviceRef (Namespace nil) falls back to entity's namespace",
			route: makeRoute(nil),
			objects: []client.Object{
				// In the same-namespace case the service is co-located with the route.
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: routeNs},
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name:      cpName,
								Namespace: routeNs,
							},
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{Name: cpName, Namespace: routeNs},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
							APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
								Name: authName,
							},
						},
					},
				},
			},
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: routeNs,
			},
		},
		{
			name:              "service not found returns error",
			route:             makeRoute(new(serviceNs)),
			objects:           nil,
			wantErrorContains: "failed to get KongService",
		},
		{
			name:              "service without ControlPlaneRef returns error",
			route:             makeRoute(new(serviceNs)),
			objects:           []client.Object{svcNoCPRef},
			wantErrorContains: "does not have a ControlPlaneRef",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := scheme.Get()
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(append(tc.objects, tc.route)...).
				WithInterceptorFuncs(populateGVKOnGet(s)).
				Build()

			nn, err := GetAPIAuthRefNN(t.Context(), cl, tc.route)

			if tc.wantErrorContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrorContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantNN, nn)
		})
	}
}
