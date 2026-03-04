package ref

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestCheckReferenceGrantForSecret(t *testing.T) {
	customizeReferenceGrant := func(rg gwtypes.ReferenceGrant, opts ...func(rg *gwtypes.ReferenceGrant)) gwtypes.ReferenceGrant {
		rg = *rg.DeepCopy()
		for _, opt := range opts {
			opt(&rg)
		}
		return rg
	}

	const (
		badSecretName   = gwtypes.ObjectName("wrong-secret")
		emptySecretName = gwtypes.ObjectName("")
		goodSecretName  = gwtypes.ObjectName("good-secret")
	)
	referenceGrantForObj := func(obj client.Object) gwtypes.ReferenceGrant {
		return gwtypes.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ref-grant-gateway",
				Namespace: "default",
			},
			Spec: gwtypes.ReferenceGrantSpec{
				From: []gwtypes.ReferenceGrantFrom{
					{
						Group:     gatewayv1.Group(obj.GetObjectKind().GroupVersionKind().Group),
						Kind:      gatewayv1.Kind(obj.GetObjectKind().GroupVersionKind().Kind),
						Namespace: gatewayv1.Namespace(obj.GetNamespace()),
					},
				},
				To: []gwtypes.ReferenceGrantTo{
					{
						Group: "",
						Kind:  "Secret",
						Name:  new(goodSecretName),
					},
				},
			},
		}
	}
	var (
		objGateway = &gatewayv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Gateway",
				APIVersion: gatewayv1.GroupVersion.Group,
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "goodNamespace",
			},
		}
		objKPI = &operatorv1alpha1.KongPluginInstallation{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongPluginInstallation",
				APIVersion: operatorv1alpha1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "goodNamespace",
			},
		}
	)

	testCases := []struct {
		name            string
		forObj          client.Object
		referenceGrants []gwtypes.ReferenceGrant
		isGranted       bool
	}{
		{
			name:      "no referenceGrants",
			forObj:    objGateway,
			isGranted: false,
		},
		{
			name:   "granted for Gateway",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				referenceGrantForObj(objGateway),
			},
			isGranted: true,
		},
		{
			name:   "granted for KPI",
			forObj: objKPI,
			referenceGrants: []gwtypes.ReferenceGrant{
				referenceGrantForObj(objKPI),
			},
			isGranted: true,
		},
		{
			name:   "not granted, wrong Kind",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				referenceGrantForObj(objKPI),
			},
			isGranted: false,
		},
		{
			name:   "not granted, bad 'from' group",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objGateway), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.From[0].Group = "wrong-group"
				}),
			},
			isGranted: false,
		},
		{
			name:   "granted, for one Kind Gateway bad 'from' group, but it expects KPI that is properly granted",
			forObj: objKPI,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objGateway), func(rg *gwtypes.ReferenceGrant) {
					rg.Name = "wrong-group"
					rg.Spec.From[0].Group = "wrong-group"
				}),
				referenceGrantForObj(objKPI),
			},
			isGranted: true,
		},
		{
			name:   "not granted, bad 'to' group",
			forObj: objKPI,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objKPI), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.To[0].Group = "wrong-group"
				}),
			},
			isGranted: false,
		},
		{
			name:   "not granted, bad 'from' kind",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objGateway), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.From[0].Kind = "wrong-kind"
				}),
			},
			isGranted: false,
		},
		{
			name:   "not granted, bad 'to' kind",
			forObj: objKPI,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objKPI), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.To[0].Kind = "wrong-kind"
				}),
			},
			isGranted: false,
		},
		{
			name:   "not granted, bad 'from' namespace",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objGateway), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.From[0].Namespace = "bad-namespace"
				}),
			},
			isGranted: false,
		},
		{
			name:   "not granted, empty 'to' secret name",
			forObj: objKPI,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objKPI), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.To[0].Name = new(emptySecretName)
				}),
			},
			isGranted: false,
		},
		{
			name:   "not granted, bad 'to' secret name",
			forObj: objGateway,
			referenceGrants: []gwtypes.ReferenceGrant{
				customizeReferenceGrant(referenceGrantForObj(objGateway), func(rg *gwtypes.ReferenceGrant) {
					rg.Spec.To[0].Name = new(badSecretName)
				}),
			},
			isGranted: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewFakeClient(lo.Map(tc.referenceGrants, func(rg gwtypes.ReferenceGrant, _ int) runtime.Object {
				return &rg
			})...)
			require.NoError(t, gatewayv1.Install(cl.Scheme()))
			_, granted, err := CheckReferenceGrantForSecret(
				t.Context(), cl,
				tc.forObj,
				gatewayv1.SecretObjectReference{
					Namespace: new(gatewayv1.Namespace("default")),
					Name:      "good-secret",
				},
			)
			require.NoError(t, err)
			assert.Equal(t, tc.isGranted, granted)
		})
	}
}
