package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongPluginBinding(t *testing.T) {
	pb := &configurationv1alpha1.KongPluginBinding{}

	require.Nil(t, pb.GetKonnectStatus())
	require.Empty(t, pb.GetKonnectStatus().GetKonnectID())
	require.Empty(t, pb.GetKonnectStatus().GetOrgID())
	require.Empty(t, pb.GetKonnectStatus().GetServerURL())

	require.Equal(t, "", pb.GetControlPlaneID())
	pb.SetControlPlaneID("123")
	require.Equal(t, "123", pb.GetControlPlaneID())
	require.Equal(t, "123", pb.Status.Konnect.ControlPlaneID)
}
