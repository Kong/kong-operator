package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongVault(t *testing.T) {
	v := &configurationv1alpha1.KongVault{}

	require.Nil(t, v.GetKonnectStatus())
	require.Empty(t, v.GetKonnectStatus().GetKonnectID())
	require.Empty(t, v.GetKonnectStatus().GetOrgID())
	require.Empty(t, v.GetKonnectStatus().GetServerURL())

	require.Equal(t, "", v.GetControlPlaneID())
	v.SetControlPlaneID("123")
	require.Equal(t, "123", v.GetControlPlaneID())
	require.Equal(t, "123", v.Status.Konnect.ControlPlaneID)
}
