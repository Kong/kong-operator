package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongUpstream(t *testing.T) {
	u := &configurationv1alpha1.KongUpstream{}

	require.Nil(t, u.GetKonnectStatus())
	require.Empty(t, u.GetKonnectStatus().GetKonnectID())
	require.Empty(t, u.GetKonnectStatus().GetOrgID())
	require.Empty(t, u.GetKonnectStatus().GetServerURL())

	require.Equal(t, "", u.GetControlPlaneID())
	u.SetControlPlaneID("123")
	require.Equal(t, "123", u.GetControlPlaneID())
	require.Equal(t, "123", u.Status.Konnect.ControlPlaneID)
}
