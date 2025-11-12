package v1alpha1_test

import (
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	v1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

// dummyHub implements conversion.Hub but is not the expected type for conversion.
type dummyHub struct{}

func (d *dummyHub) Hub() {}

// Implement runtime.Object methods for dummyHub
func (d *dummyHub) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }
func (d *dummyHub) DeepCopyObject() runtime.Object   { return &dummyHub{} }

func TestKonnectGatewayControlPlane_ConvertTo(t *testing.T) {
	cases := []struct {
		name             string
		spec             v1alpha1.KonnectGatewayControlPlaneSpec
		mirror           *v1alpha1.MirrorSpec
		expectsCreateReq bool
		expectError      bool
	}{
		{
			name: "Origin with all fields",
			spec: v1alpha1.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: v1alpha1.CreateControlPlaneRequest{
					Name:         lo.ToPtr("test-name"),
					Description:  lo.ToPtr("desc"),
					ClusterType:  lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
					AuthType:     lo.ToPtr(sdkkonnectcomp.AuthTypePkiClientCerts),
					CloudGateway: lo.ToPtr(true),
					ProxyUrls: []sdkkonnectcomp.ProxyURL{
						{Host: "host1", Port: 8080, Protocol: "http"},
						{Host: "host2", Port: 8443, Protocol: "https"},
					},
					Labels: map[string]string{"foo": "bar"},
				},
				Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				Members: []corev1.LocalObjectReference{
					{Name: "member1"},
					{Name: "member2"},
				},
				KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{},
			},
			mirror:           nil,
			expectsCreateReq: true,
		},
		{
			name: "Mirror with MirrorSpec",
			spec: v1alpha1.KonnectGatewayControlPlaneSpec{
				Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
			},
			mirror:           &v1alpha1.MirrorSpec{Konnect: v1alpha1.MirrorKonnect{ID: commonv1alpha1.KonnectIDType("mirror-id")}},
			expectsCreateReq: false,
		},
		{
			name:        "error: wrong hub type",
			expectError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &v1alpha1.KonnectGatewayControlPlane{
				Spec: tc.spec,
			}
			obj.Spec.Mirror = tc.mirror
			if tc.expectError {
				err := obj.ConvertTo(&dummyHub{})
				assert.Error(t, err)
				expectedMsg := fmt.Sprintf("KonnectGatewayControlPlane ConvertTo: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T", &dummyHub{})
				assert.EqualError(t, err, expectedMsg)
				return
			}
			dst := &konnectv1alpha2.KonnectGatewayControlPlane{}
			err := obj.ConvertTo(dst)
			assert.NoError(t, err)
			if tc.expectsCreateReq {
				require.NotNil(t, dst.Spec.CreateControlPlaneRequest)
				assert.Equal(t, lo.FromPtr(tc.spec.Name), dst.Spec.CreateControlPlaneRequest.Name)
				assert.Equal(t, tc.spec.Description, dst.Spec.CreateControlPlaneRequest.Description)
				assert.Equal(t, tc.spec.ClusterType, dst.Spec.CreateControlPlaneRequest.ClusterType)
				assert.Equal(t, tc.spec.AuthType, dst.Spec.CreateControlPlaneRequest.AuthType)
				assert.Equal(t, tc.spec.CloudGateway, dst.Spec.CreateControlPlaneRequest.CloudGateway)
				assert.Equal(t, tc.spec.ProxyUrls, dst.Spec.CreateControlPlaneRequest.ProxyUrls)
				assert.Equal(t, tc.spec.Labels, dst.Spec.CreateControlPlaneRequest.Labels)
			} else {
				assert.Nil(t, dst.Spec.CreateControlPlaneRequest)
			}
			if tc.mirror != nil {
				require.NotNil(t, dst.Spec.Mirror)
				assert.Equal(t, tc.mirror.Konnect.ID, dst.Spec.Mirror.Konnect.ID)
			} else {
				assert.Nil(t, dst.Spec.Mirror)
			}
			assert.Equal(t, tc.spec.Source, dst.Spec.Source)
			assert.Equal(t, tc.spec.Members, dst.Spec.Members)
			assert.Equal(t, tc.spec.KonnectConfiguration, dst.Spec.KonnectConfiguration)
		})
	}
}

func TestKonnectGatewayControlPlane_ConvertFrom(t *testing.T) {
	name := "test-name"
	desc := "desc"
	clusterType := sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane
	authType := sdkkonnectcomp.AuthTypePkiClientCerts
	cloudGateway := true
	proxyUrls := []sdkkonnectcomp.ProxyURL{
		{Host: "host1", Port: 8080, Protocol: "http"},
		{Host: "host2", Port: 8443, Protocol: "https"},
	}
	labels := map[string]string{"foo": "bar"}
	source := commonv1alpha1.EntitySourceOrigin
	members := []corev1.LocalObjectReference{{Name: "member1"}, {Name: "member2"}}
	konnectConfig := konnectv1alpha2.KonnectConfiguration{}

	cases := []struct {
		name             string
		src              konnectv1alpha2.KonnectGatewayControlPlaneSpec
		mirror           *konnectv1alpha2.MirrorSpec
		expectsCreateReq bool
		expectError      bool
	}{
		{
			name: "With CreateControlPlaneRequest and Mirror",
			src: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
					Name:         name,
					Description:  &desc,
					ClusterType:  &clusterType,
					AuthType:     &authType,
					CloudGateway: &cloudGateway,
					ProxyUrls:    proxyUrls,
					Labels:       labels,
				},
				Source:               lo.ToPtr(source),
				Members:              members,
				KonnectConfiguration: konnectConfig,
			},
			mirror:           &konnectv1alpha2.MirrorSpec{Konnect: konnectv1alpha2.MirrorKonnect{ID: commonv1alpha1.KonnectIDType("mirror-id")}},
			expectsCreateReq: true,
		},
		{
			name: "No CreateControlPlaneRequest, no Mirror",
			src: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				Source: lo.ToPtr(source),
			},
			mirror:           nil,
			expectsCreateReq: false,
		},
		{
			name:        "error: wrong hub type",
			expectError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &v1alpha1.KonnectGatewayControlPlane{}
			if tc.expectError {
				err := obj.ConvertFrom(&dummyHub{})
				assert.Error(t, err)
				expectedMsg := fmt.Sprintf("KonnectGatewayControlPlane ConvertFrom: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T", &dummyHub{})
				assert.EqualError(t, err, expectedMsg)
				return
			}
			src := &konnectv1alpha2.KonnectGatewayControlPlane{
				Spec: tc.src,
			}
			src.Spec.Mirror = tc.mirror
			require.NoError(t, obj.ConvertFrom(src))
			if tc.expectsCreateReq {
				require.NotNil(t, obj.Spec.CreateControlPlaneRequest)
				assert.Equal(t, lo.ToPtr(tc.src.CreateControlPlaneRequest.Name), obj.Spec.Name)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.Description, obj.Spec.Description)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.ClusterType, obj.Spec.ClusterType)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.AuthType, obj.Spec.AuthType)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.CloudGateway, obj.Spec.CloudGateway)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.ProxyUrls, obj.Spec.ProxyUrls)
				assert.Equal(t, tc.src.CreateControlPlaneRequest.Labels, obj.Spec.Labels)
			} else {
				assert.Equal(t, obj.Spec.CreateControlPlaneRequest, v1alpha1.CreateControlPlaneRequest{})
			}
			if tc.mirror != nil {
				require.NotNil(t, obj.Spec.Mirror)
				assert.Equal(t, tc.mirror.Konnect.ID, obj.Spec.Mirror.Konnect.ID)
			} else {
				assert.Nil(t, obj.Spec.Mirror)
			}
			assert.Equal(t, tc.src.Source, obj.Spec.Source)
			assert.Equal(t, tc.src.Members, obj.Spec.Members)
			assert.Equal(t, tc.src.KonnectConfiguration, obj.Spec.KonnectConfiguration)
		})
	}
}
