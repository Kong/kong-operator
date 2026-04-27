package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestKonnectAPIAuthReferencingTypesOnlyIncludeSupportedEntities(t *testing.T) {
	t.Parallel()

	require.Len(t, konnectAPIAuthReferencingTypes, 4)
	for _, ent := range konnectAPIAuthReferencingTypes {
		switch ent.(type) {
		case *konnectv1alpha1.KonnectCloudGatewayNetwork,
			*konnectv1alpha2.KonnectGatewayControlPlane,
			*konnectv1alpha2.KonnectExtension,
			*konnectv1alpha1.KonnectEventGateway:
		default:
			t.Fatalf("unexpected KonnectAPIAuthConfiguration referencing type %T", ent)
		}
	}
}

func TestKonnectAPIAuthReferencingTypeListsOnlyIncludeSupportedEntities(t *testing.T) {
	t.Parallel()

	require.Len(t, konnectAPIAuthReferencingTypeListsWithIndexes, 4)
	for objList := range konnectAPIAuthReferencingTypeListsWithIndexes {
		assertSupportedKonnectAPIAuthReferencingListType(t, objList)
	}
}

func assertSupportedKonnectAPIAuthReferencingListType(t *testing.T, objList client.ObjectList) {
	t.Helper()

	switch objList.(type) {
	case *konnectv1alpha1.KonnectCloudGatewayNetworkList,
		*konnectv1alpha2.KonnectGatewayControlPlaneList,
		*konnectv1alpha2.KonnectExtensionList,
		*konnectv1alpha1.KonnectEventGatewayList:
	default:
		t.Fatalf("unexpected KonnectAPIAuthConfiguration referencing list type %T", objList)
	}
}
