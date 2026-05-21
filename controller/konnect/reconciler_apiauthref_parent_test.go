package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

type getAPIAuthConfigurationRefFromParentTestCase struct {
	name              string
	parentRef         commonv1alpha1.ObjectRef
	withParent        bool
	withAPIAuth       bool
	wantNN            types.NamespacedName
	wantErrorContains string
}

type getParentForRefTestCase struct {
	name              string
	ref               commonv1alpha1.ObjectRef
	namespace         string
	withParent        bool
	parentNamespace   string
	wantNN            types.NamespacedName
	wantErrorContains string
	wantNotFoundError bool
}

func TestGetAPIAuthConfigurationRefFromParent_Portal(t *testing.T) {
	testGetAPIAuthConfigurationRefFromParent(t,
		func(ref commonv1alpha1.ObjectRef) objectWithParentRef {
			return &konnectv1alpha1.PortalPage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "portal-page",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.PortalPageSpec{
					PortalRef: ref,
				},
			}
		},
		func(namespace, apiAuthName string) *konnectv1alpha1.Portal {
			return &konnectv1alpha1.Portal{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "parent",
					Namespace: namespace,
				},
				Spec: konnectv1alpha1.PortalSpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: apiAuthName,
						},
					},
				},
			}
		},
	)
}

func TestGetAPIAuthConfigurationRefFromParent_EventGateway(t *testing.T) {
	testGetAPIAuthConfigurationRefFromParent(t,
		func(ref commonv1alpha1.ObjectRef) objectWithParentRef {
			return &konnectv1alpha1.EventGatewayBackendCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "backend-cluster",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
					GatewayRef: ref,
				},
			}
		},
		func(namespace, apiAuthName string) *konnectv1alpha1.KonnectEventGateway {
			return &konnectv1alpha1.KonnectEventGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "parent",
					Namespace: namespace,
				},
				Spec: konnectv1alpha1.KonnectEventGatewaySpec{
					KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
							Name: apiAuthName,
						},
					},
				},
			}
		},
	)
}

func testGetAPIAuthConfigurationRefFromParent[
	ParentT parentT,
	ParentTPtr parentWithAPIAuthTPtr[ParentT],
](
	t *testing.T,
	childBuilder func(commonv1alpha1.ObjectRef) objectWithParentRef,
	parentBuilder func(namespace, apiAuthName string) ParentTPtr,
) {
	t.Helper()

	const (
		childNamespace = "default"
		parentName     = "parent"
		apiAuthName    = "api-auth"
	)

	testCases := []getAPIAuthConfigurationRefFromParentTestCase{
		{
			name: "returns API auth configuration from parent",
			parentRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: parentName,
				},
			},
			withParent:  true,
			withAPIAuth: true,
			wantNN: types.NamespacedName{
				Namespace: childNamespace,
				Name:      apiAuthName,
			},
		},
		{
			name: "returns error for non namespaced parent ref",
			parentRef: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new(string),
			},
			wantErrorContains: "invalid parent reference",
		},
		{
			name: "returns error for missing namespaced ref",
			parentRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
			},
			wantErrorContains: "invalid parent reference",
		},
		{
			name: "returns error when parent does not exist",
			parentRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: parentName,
				},
			},
			wantErrorContains: "failed to get",
		},
		{
			name: "returns error when API auth configuration does not exist",
			parentRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: parentName,
				},
			},
			withParent:        true,
			wantErrorContains: "failed to get APIAuthConfiguration",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			child := childBuilder(tc.parentRef)

			builder := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(child)

			if tc.withParent {
				builder = builder.WithObjects(parentBuilder(childNamespace, apiAuthName))
			}
			if tc.withAPIAuth {
				builder = builder.WithObjects(&konnectv1alpha1.KonnectAPIAuthConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      apiAuthName,
						Namespace: childNamespace,
					},
				})
			}

			cl := builder.Build()

			nn, err := getAPIAuthConfigurationRefFromParent[ParentT, ParentTPtr](t.Context(), cl, child, tc.parentRef)
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

func TestGetParentForRef_Portal(t *testing.T) {
	testGetParentForRef(t, func(namespace string) *konnectv1alpha1.Portal {
		return &konnectv1alpha1.Portal{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "parent",
				Namespace: namespace,
			},
		}
	})
}

func TestGetParentForRef_EventGateway(t *testing.T) {
	testGetParentForRef(t, func(namespace string) *konnectv1alpha1.KonnectEventGateway {
		return &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "parent",
				Namespace: namespace,
			},
		}
	})
}

func testGetParentForRef[
	ParentT parentT,
	ParentTPtr parentTPtr[ParentT],
](
	t *testing.T,
	parentBuilder func(namespace string) ParentTPtr,
) {
	t.Helper()

	const (
		defaultNamespace = "default"
		otherNamespace   = "other"
		parentName       = "parent"
	)

	testCases := []getParentForRefTestCase{
		{
			name: "returns parent from caller namespace",
			ref: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: parentName,
				},
			},
			namespace:       defaultNamespace,
			withParent:      true,
			parentNamespace: defaultNamespace,
			wantNN: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      parentName,
			},
		},
		{
			name: "returns parent from explicit reference namespace",
			ref: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name:      parentName,
					Namespace: new(otherNamespace),
				},
			},
			namespace:       defaultNamespace,
			withParent:      true,
			parentNamespace: otherNamespace,
			wantNN: types.NamespacedName{
				Namespace: otherNamespace,
				Name:      parentName,
			},
		},
		{
			name: "returns error for namespaced ref without namespaced ref payload",
			ref: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
			},
			namespace:         defaultNamespace,
			wantErrorContains: "ref.namespacedRef is required",
		},
		{
			name: "returns typed error when parent does not exist",
			ref: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: parentName,
				},
			},
			namespace:         defaultNamespace,
			wantErrorContains: "does not exist",
			wantNotFoundError: true,
			wantNN: types.NamespacedName{
				Namespace: defaultNamespace,
				Name:      parentName,
			},
		},
		{
			name: "returns error for konnect id ref",
			ref: commonv1alpha1.ObjectRef{
				Type:      commonv1alpha1.ObjectRefTypeKonnectID,
				KonnectID: new(string),
			},
			namespace:         defaultNamespace,
			wantErrorContains: "unsupported ref type",
		},
		{
			name:              "returns error for unsupported ref type",
			ref:               commonv1alpha1.ObjectRef{},
			namespace:         defaultNamespace,
			wantErrorContains: "unsupported ref type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme.Get())
			if tc.withParent {
				builder = builder.WithObjects(parentBuilder(tc.parentNamespace))
			}

			cl := builder.Build()

			parent, nn, err := getParentForRef[ParentT, ParentTPtr](t.Context(), cl, tc.ref, tc.namespace)
			if tc.wantErrorContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrorContains)
				if tc.wantNotFoundError {
					var notFoundErr ReferencedObjectDoesNotExistError
					require.ErrorAs(t, err, &notFoundErr)
					require.Equal(t, tc.wantNN, notFoundErr.Reference)
				}
				require.Nil(t, parent)
				require.Equal(t, tc.wantNN, nn)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parent)
			require.Equal(t, tc.wantNN, nn)
			require.Equal(t, parentName, parent.GetName())
			require.Equal(t, tc.wantNN.Namespace, parent.GetNamespace())
		})
	}
}
