package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongTarget(t *testing.T) {
	kt := &configurationv1alpha1.KongTarget{}

	require.Nil(t, kt.GetKonnectStatus())
	require.Empty(t, kt.GetKonnectStatus().GetKonnectID())
	require.Empty(t, kt.GetKonnectStatus().GetOrgID())
	require.Empty(t, kt.GetKonnectStatus().GetServerURL())

	require.Equal(t, "", kt.GetControlPlaneID())
	kt.SetControlPlaneID("123")
	require.Equal(t, "123", kt.GetControlPlaneID())
	require.Equal(t, "123", kt.Status.Konnect.ControlPlaneID)
}
