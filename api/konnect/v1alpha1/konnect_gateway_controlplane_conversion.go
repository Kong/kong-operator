package v1alpha1

import (
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

const (
	errWrongConvertToKonnectGatewayControlPlane   = "KonnectGatewayControlPlane ConvertTo: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T"
	errWrongConvertFromKonnectGatewayControlPlane = "KonnectGatewayControlPlane ConvertFrom: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T"
)

// ConvertTo converts this KonnectGatewayControlPlane (v1alpha1) to the Hub version (v1alpha2).
func (kgcp *KonnectGatewayControlPlane) ConvertTo(dstRaw conversion.Hub) error {

	dst, ok := dstRaw.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertToKonnectGatewayControlPlane, dstRaw)
	}

	dst.ObjectMeta = kgcp.ObjectMeta

	// Convert the changed fields between v1alpha1 and v1alpha2.
	dst.Spec.CreateControlPlaneRequest = createControlPlaneRequestFromSpec(kgcp.Spec)

	if kgcp.Spec.Mirror != nil {
		dst.Spec.Mirror = &konnectv1alpha2.MirrorSpec{
			Konnect: konnectv1alpha2.MirrorKonnect{
				ID: kgcp.Spec.Mirror.Konnect.ID,
			},
		}
	} else {
		dst.Spec.Mirror = nil
	}
	dst.Spec.Source = kgcp.Spec.Source
	dst.Spec.Members = kgcp.Spec.Members

	dst.Spec.KonnectConfiguration = konnectv1alpha2.ControlPlaneKonnectConfiguration{
		APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
			Name: kgcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		},
	}

	return nil
}

// ConvertFrom converts the Hub version (v1alpha2) to this KonnectGatewayControlPlane (v1alpha1).
func (kgcp *KonnectGatewayControlPlane) ConvertFrom(srcRaw conversion.Hub) error {

	src, ok := srcRaw.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertFromKonnectGatewayControlPlane, srcRaw)
	}

	kgcp.ObjectMeta = src.ObjectMeta

	kgcp.Spec.CreateControlPlaneRequest = createInlineControlPlaneRequest(src.Spec.CreateControlPlaneRequest)
	if src.Spec.Mirror != nil {
		kgcp.Spec.Mirror = &MirrorSpec{
			Konnect: MirrorKonnect{
				ID: src.Spec.Mirror.Konnect.ID,
			},
		}
	} else {
		kgcp.Spec.Mirror = nil
	}
	kgcp.Spec.Source = src.Spec.Source
	kgcp.Spec.Members = src.Spec.Members

	kgcp.Spec.KonnectConfiguration = konnectv1alpha2.KonnectConfiguration{
		APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
			Name: src.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		},
	}

	return nil
}

// createControlPlaneRequestFromSpec converts a KonnectGatewayControlPlaneSpec to a CreateControlPlaneRequest
// handling nil pointers and type conversions appropriately.
func createControlPlaneRequestFromSpec(spec KonnectGatewayControlPlaneSpec) *sdkkonnectcomp.CreateControlPlaneRequest {
	// Only create the request if this is an Origin type (not Mirror)
	if spec.Source != nil && *spec.Source == "Mirror" {
		return nil
	}

	return &sdkkonnectcomp.CreateControlPlaneRequest{
		Name:         lo.FromPtr(spec.Name),
		Description:  spec.Description,
		ClusterType:  spec.ClusterType,
		AuthType:     spec.AuthType,
		CloudGateway: spec.CloudGateway,
		ProxyUrls:    spec.ProxyUrls,
		Labels:       spec.Labels,
	}
}

// createInlineControlPlaneRequest fills a v1alpha1 CreateControlPlaneRequest from an sdk CreateControlPlaneRequest.
func createInlineControlPlaneRequest(req *sdkkonnectcomp.CreateControlPlaneRequest) CreateControlPlaneRequest {
	if req == nil {
		return CreateControlPlaneRequest{}
	}
	return CreateControlPlaneRequest{
		Name:         new(req.Name),
		Description:  req.Description,
		ClusterType:  req.ClusterType,
		AuthType:     req.AuthType,
		CloudGateway: req.CloudGateway,
		ProxyUrls:    req.ProxyUrls,
		Labels:       req.Labels,
	}
}
