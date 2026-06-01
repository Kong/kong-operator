package konnect

import (
	"context"
	"testing"

	"github.com/samber/lo"
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
//   - GetCPForRef error (CP missing)
//   - same-namespace authRef (Namespace nil) verifies the resolved namespace
//     is the CP's own namespace, not the namespace argument passed by the caller
//     (this is the bug Fix 1 addresses)
//   - same-namespace authRef (Namespace explicitly equal to CP's), guards the
//     "!= cpNamespace" condition
//   - cross-namespace authRef without a grant, surfaces ReferenceNotGrantedError
//   - cross-namespace authRef with a valid grant, returns the authRef's namespace
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
			objects:         []client.Object{makeCP(lo.ToPtr(cpNamespace))},
			callerNamespace: callerNs,
			wantNN: types.NamespacedName{
				Name:      authName,
				Namespace: cpNamespace,
			},
		},
		{
			name:                "cross-namespace authRef without grant returns ReferenceNotGrantedError",
			objects:             []client.Object{makeCP(lo.ToPtr(authNamespace))},
			callerNamespace:     callerNs,
			wantErrorContains:   "is not granted",
			wantNotGrantedError: true,
		},
		{
			name:            "cross-namespace authRef with valid grant returns authRef's namespace",
			objects:         []client.Object{makeCP(lo.ToPtr(authNamespace)), grant},
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
					var notGranted *crossnamespace.ErrReferenceNotGranted
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
