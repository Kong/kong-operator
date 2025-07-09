package test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

func TestKonnectFuncs(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		SetControlPlaneID(string)
		GetControlPlaneID() string
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
		GetConditions() []metav1.Condition
		SetConditions([]metav1.Condition)
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
			typeName: "KongKey",
			object:   &configurationv1alpha1.KongKey{},
		},
		{
			typeName: "KongSNI",
			object:   &configurationv1alpha1.KongSNI{},
		},
		{
			typeName: "KongCredentialBasicAuth",
			object:   &configurationv1alpha1.KongCredentialBasicAuth{},
		},
		{
			typeName: "KongCredentialACL",
			object:   &configurationv1alpha1.KongCredentialACL{},
		},
		{
			typeName: "KongCredentialAPIKey",
			object:   &configurationv1alpha1.KongCredentialAPIKey{},
		},
		{
			typeName: "KongCredentialJWT",
			object:   &configurationv1alpha1.KongCredentialJWT{},
		},
		{
			typeName: "KongCredentialHMAC",
			object:   &configurationv1alpha1.KongCredentialHMAC{},
		},
		{
			typeName: "KongDataPlaneClientCertificate",
			object:   &configurationv1alpha1.KongDataPlaneClientCertificate{},
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

			require.Empty(t, obj.GetConditions())
			obj.SetConditions([]metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			})
			require.Equal(t,
				[]metav1.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
				obj.GetConditions(),
			)
		})
	}
}

func TestKonnectFuncsNoKonnectStatus(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		GetConditions() []metav1.Condition
		SetConditions([]metav1.Condition)
	}

	testcases := []struct {
		object   KonnectEntity
		typeName string
	}{
		{
			typeName: "KonnectAPIAuthConfiguration",
			object:   &konnectv1alpha1.KonnectAPIAuthConfiguration{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, obj.GetTypeName(), tc.typeName)
			require.Empty(t, obj.GetConditions())
			obj.SetConditions([]metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			})
			require.Equal(t,
				[]metav1.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
				obj.GetConditions(),
			)
		})
	}
}

func TestKonnectFuncsStandAlone(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
		GetConditions() []metav1.Condition
		SetConditions([]metav1.Condition)
	}

	testcases := []struct {
		object   KonnectEntity
		typeName string
	}{
		{
			typeName: "KonnectGatewayControlPlane",
			object:   &konnectv1alpha1.KonnectGatewayControlPlane{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, obj.GetTypeName(), tc.typeName)
			require.Empty(t, obj.GetKonnectStatus())
			require.Empty(t, obj.GetKonnectStatus().GetKonnectID())
			require.Empty(t, obj.GetKonnectStatus().GetOrgID())
			require.Empty(t, obj.GetKonnectStatus().GetServerURL())
			require.Empty(t, obj.GetConditions())
			obj.SetConditions([]metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			})
			require.Equal(t,
				[]metav1.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
				obj.GetConditions(),
			)
		})
	}
}

func TestKonnectFuncsNetworkRef(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
		GetConditions() []metav1.Condition
		SetConditions([]metav1.Condition)
		GetNetworkID() string
		SetNetworkID(string)
	}

	testcases := []struct {
		object   KonnectEntity
		typeName string
	}{
		{
			typeName: "KonnectCloudGatewayTransitGateway",
			object:   &konnectv1alpha1.KonnectCloudGatewayTransitGateway{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, obj.GetTypeName(), tc.typeName)
			require.Empty(t, obj.GetKonnectStatus())
			require.Empty(t, obj.GetKonnectStatus().GetKonnectID())
			require.Empty(t, obj.GetKonnectStatus().GetOrgID())
			require.Empty(t, obj.GetKonnectStatus().GetServerURL())
			require.Empty(t, obj.GetConditions())

			obj.SetConditions([]metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			})
			require.Equal(t,
				[]metav1.Condition{
					{
						Type:   "Ready",
						Status: metav1.ConditionTrue,
					},
				},
				obj.GetConditions(),
			)

			obj.SetNetworkID("network")
			require.Equal(t, "network", obj.GetNetworkID())
		})
	}
}

func TestServiceRef(t *testing.T) {
	type CfgEntity interface {
		client.Object
		GetTypeName() string
		GetServiceRef() *configurationv1alpha1.ServiceRef
		SetServiceRef(*configurationv1alpha1.ServiceRef)
	}

	testcases := []struct {
		object   CfgEntity
		typeName string
	}{
		{
			typeName: "KongRoute",
			object:   &configurationv1alpha1.KongRoute{},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, obj.GetTypeName(), tc.typeName)
			require.Nil(t, obj.GetServiceRef())

			serviceRef := &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NameRef{
					Name: "test-service",
				},
			}
			obj.SetServiceRef(serviceRef)
			require.Equal(t, serviceRef, obj.GetServiceRef())
		})
	}
}

func TestCredentialTypes(t *testing.T) {
	type KonnectEntity interface {
		client.Object
		GetTypeName() string
		SetKonnectConsumerIDInStatus(id string)
		GetConsumerRefName() string
	}
	consumerRef := corev1.LocalObjectReference{
		Name: "test-kong-consumer",
	}

	testcases := []struct {
		object KonnectEntity
	}{
		{
			object: &configurationv1alpha1.KongCredentialBasicAuth{
				Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
					ConsumerRef: consumerRef,
				},
			},
		},
		{
			object: &configurationv1alpha1.KongCredentialACL{
				Spec: configurationv1alpha1.KongCredentialACLSpec{
					ConsumerRef: consumerRef,
				},
			},
		},
		{
			object: &configurationv1alpha1.KongCredentialAPIKey{
				Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
					ConsumerRef: consumerRef,
				},
			},
		},
		{
			object: &configurationv1alpha1.KongCredentialJWT{
				Spec: configurationv1alpha1.KongCredentialJWTSpec{
					ConsumerRef: consumerRef,
				},
			},
		},
		{
			object: &configurationv1alpha1.KongCredentialHMAC{
				Spec: configurationv1alpha1.KongCredentialHMACSpec{
					ConsumerRef: consumerRef,
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.object.GetTypeName(), func(t *testing.T) {
			obj := tc.object

			require.Equal(t, "test-kong-consumer", obj.GetConsumerRefName())
			obj.SetKonnectConsumerIDInStatus("123456")
		})
	}
}

func TestLists(t *testing.T) {
	testKonnectEntityList(t, &configurationv1.KongPluginList{}, 0)
	testKonnectEntityList(t, &configurationv1.KongPluginList{
		Items: []configurationv1.KongPlugin{
			{},
		},
	}, 1)
	testKonnectEntityList(t, &configurationv1.KongConsumerList{}, 0)
	testKonnectEntityList(t, &configurationv1.KongConsumerList{
		Items: []configurationv1.KongConsumer{
			{},
		},
	}, 1)
	testKonnectEntityList(t, &configurationv1beta1.KongConsumerGroupList{}, 0)
	testKonnectEntityList(t, &configurationv1beta1.KongConsumerGroupList{
		Items: []configurationv1beta1.KongConsumerGroup{
			{},
		},
	}, 1)
	testKonnectEntityList(t, &configurationv1alpha1.KongCredentialACLList{}, 0)
	testKonnectEntityList(t, &configurationv1alpha1.KongCredentialAPIKeyList{}, 0)
	testKonnectEntityList(t, &configurationv1alpha1.KongCredentialBasicAuthList{}, 0)
	testKonnectEntityList(t, &configurationv1alpha1.KongCredentialJWTList{}, 0)
	testKonnectEntityList(t, &configurationv1alpha1.KongCredentialHMACList{}, 0)
}

type ClientObject[T any] interface {
	GetTypeName() string
	GetName() string
	GetNamespace() string
	*T
}

func testKonnectEntityList[
	TList interface {
		client.ObjectList
		GetItems() []T
	},
	T any,
	TT ClientObject[T],
](t *testing.T, list TList, count int) {
	t.Helper()
	var obj TT = new(T)
	t.Run(
		obj.GetTypeName()+"/"+strconv.Itoa(count),
		func(t *testing.T) {
			require.Len(t, list.GetItems(), count)
		},
	)
}
