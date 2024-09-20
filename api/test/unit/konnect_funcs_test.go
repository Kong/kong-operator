package test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectFuncs(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		SetControlPlaneID(string)
		GetControlPlaneID() string
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
	}

	testcases := []struct {
		object   KonnectEntity
		typeName string
	}{
		{
			typeName: "KongConsumer",
			object:   &configurationv1.KongConsumer{},
		},
		{
			typeName: "KongCACertificate",
			object:   &configurationv1alpha1.KongCACertificate{},
		},
		{
			typeName: "KongConsumerGroup",
			object:   &configurationv1beta1.KongConsumerGroup{},
		},
		{
			typeName: "KongPluginBinding",
			object:   &configurationv1alpha1.KongPluginBinding{},
		},
		{
			typeName: "KongUpstream",
			object:   &configurationv1alpha1.KongUpstream{},
		},
		{
			typeName: "KongTarget",
			object:   &configurationv1alpha1.KongTarget{},
		},
		{
			typeName: "KongService",
			object:   &configurationv1alpha1.KongService{},
		},
		{
			typeName: "KongVault",
			object:   &configurationv1alpha1.KongVault{},
		},
		{
			typeName: "KongCredentialBasicAuth",
			object:   &configurationv1alpha1.KongCredentialBasicAuth{},
		},
		{
			typeName: "KongKey",
			object:   &configurationv1alpha1.KongKey{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, obj.GetTypeName(), tc.typeName)
			require.Nil(t, obj.GetKonnectStatus())
			require.Empty(t, obj.GetKonnectStatus().GetKonnectID())
			require.Empty(t, obj.GetKonnectStatus().GetOrgID())
			require.Empty(t, obj.GetKonnectStatus().GetServerURL())

			require.Equal(t, "", obj.GetControlPlaneID())
			obj.SetControlPlaneID("123")
			require.Equal(t, "123", obj.GetControlPlaneID())
		})
	}
}
