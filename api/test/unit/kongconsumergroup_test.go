package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

func TestKongConsumerGroup(t *testing.T) {
	cg := &configurationv1beta1.KongConsumerGroup{}

	require.Nil(t, cg.GetKonnectStatus())
	require.Empty(t, cg.GetKonnectStatus().GetKonnectID())
	require.Empty(t, cg.GetKonnectStatus().GetOrgID())
	require.Empty(t, cg.GetKonnectStatus().GetServerURL())

	require.Equal(t, "", cg.GetControlPlaneID())
	cg.SetControlPlaneID("123")
	require.Equal(t, "123", cg.GetControlPlaneID())
	require.Equal(t, "123", cg.Status.Konnect.ControlPlaneID)
}
