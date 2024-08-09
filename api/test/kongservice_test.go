package test

import (
	"testing"

	"github.com/stretchr/testify/require"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongService(t *testing.T) {
	svc := &configurationv1alpha1.KongService{}

	require.Nil(t, svc.GetKonnectStatus())
	require.Empty(t, svc.GetKonnectStatus().GetKonnectID())
	require.Empty(t, svc.GetKonnectStatus().GetOrgID())
	require.Empty(t, svc.GetKonnectStatus().GetServerURL())

	svc.SetControlPlaneID("123")
	require.Equal(t, "123", svc.GetControlPlaneID())
	require.Equal(t, "123", svc.Status.Konnect.ControlPlaneID)
}
