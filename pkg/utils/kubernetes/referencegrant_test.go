package kubernetes

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var referenceGrantTypeMeta = metav1.TypeMeta{
	APIVersion: gatewayv1beta1.GroupVersion.String(),
	Kind:       "ReferenceGrant",
}

func TestAllowedByReferenceGrants(t *testing.T) {
	testCases := []struct {
		name            string
		from            gatewayv1beta1.ReferenceGrantFrom
		targetNamespace string
		to              gatewayv1beta1.ReferenceGrantTo
		objs            []runtime.Object
		allow           bool
	}{
		{
			name: "should allow for same namespace",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "ns-1",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
			},
			targetNamespace: "ns-1",
			allow:           true,
		},
		{
			name: "should allow if one of `spec.from` and `spec.to` matches in the ReferenceGrant in the target namespace",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
			},
			targetNamespace: "target-namespace",
			objs: []runtime.Object{
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-1",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "some-group.k8s.io",
								Kind:      "SomeKind",
								Namespace: "source-namespace",
							},
							{
								Group:     "some-group.k8s.io",
								Kind:      "AnotherKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "another-group.k8s.io",
								Kind:  "AnotherKind",
							},
						},
					},
				},
			},
			allow: true,
		},
		{
			name: "should not allow if no ReferenceGrant allows",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
			},
			targetNamespace: "target-namespace",
			allow:           false,
		},
		{
			name: "should process 'core' group correctly",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "core",
				Kind:      "Service",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "",
				Kind:  "Secret",
			},
			targetNamespace: "target-namespace",
			objs: []runtime.Object{
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-1",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "",
								Kind:      "Service",
								Namespace: "source-namespace",
							},
							{
								Group:     "some-group.k8s.io",
								Kind:      "AnotherKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
							},
						},
					},
				},
			},
			allow: true,
		},
		{
			name: "should allow if name matches",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
				Name:  lo.ToPtr(gatewayv1beta1.ObjectName("some-name")),
			},
			targetNamespace: "target-namespace",
			objs: []runtime.Object{
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-1",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "some-group.k8s.io",
								Kind:      "SomeKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "another-group.k8s.io",
								Kind:  "AnotherKind",
								Name:  lo.ToPtr(gatewayv1beta1.ObjectName("some-name")),
							},
						},
					},
				},
			},
			allow: true,
		},
		{
			name: "should not allow if name does not match",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
				Name:  lo.ToPtr(gatewayv1beta1.ObjectName("some-name")),
			},
			targetNamespace: "target-namespace",
			objs: []runtime.Object{
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-1",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "some-group.k8s.io",
								Kind:      "SomeKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "another-group.k8s.io",
								Kind:  "AnotherKind",
								Name:  lo.ToPtr(gatewayv1beta1.ObjectName("another-name")),
							},
						},
					},
				},
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-2",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "some-group.k8s.io",
								Kind:      "AnotherKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "another-group.k8s.io",
								Kind:  "AnotherKind",
								Name:  lo.ToPtr(gatewayv1beta1.ObjectName("some-name")),
							},
						},
					},
				},
			},
			allow: false,
		},
		{
			name: "should allow if input specifies name and ReferenceGrant does not",
			from: gatewayv1beta1.ReferenceGrantFrom{
				Group:     "some-group.k8s.io",
				Kind:      "SomeKind",
				Namespace: "source-namespace",
			},
			to: gatewayv1beta1.ReferenceGrantTo{
				Group: "another-group.k8s.io",
				Kind:  "AnotherKind",
				Name:  lo.ToPtr(gatewayv1beta1.ObjectName("some-name")),
			},
			targetNamespace: "target-namespace",
			objs: []runtime.Object{
				&gatewayv1beta1.ReferenceGrant{
					TypeMeta: referenceGrantTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "target-namespace",
						Name:      "ref-grant-1",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     "some-group.k8s.io",
								Kind:      "SomeKind",
								Namespace: "source-namespace",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "another-group.k8s.io",
								Kind:  "AnotherKind",
							},
						},
					},
				},
			},
			allow: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewFakeClient(tc.objs...)
			require.NoError(t, gatewayv1beta1.Install(cl.Scheme()))
			allow, err := AllowedByReferenceGrants(t.Context(), cl, tc.from, tc.targetNamespace, tc.to)
			require.NoError(t, err)
			require.Equal(t, tc.allow, allow)
		})
	}
}
