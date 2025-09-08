package v1alpha1

import (
	"fmt"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

const (
	errWrongConvertToKonnectGatewayControlPlane   = "KonnectGatewayControlPlane ConvertTo: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T"
	errWrongConvertFromKonnectGatewayControlPlane = "KonnectGatewayControlPlane ConvertFrom: expected *konnectv1alpha2.KonnectGatewayControlPlane, got %T"
)

// ConvertTo converts this KonnectGatewayControlPlane (v1alpha1) to the Hub version (v1alpha2).
func (src *KonnectGatewayControlPlane) ConvertTo(dstRaw conversion.Hub) error {

	dst, ok := dstRaw.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertToKonnectGatewayControlPlane, dstRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	// Convert the changed fields between v1alpha1 and v1alpha2.
	dst.Spec.CreateControlPlaneRequest = createControlplaneRequestFromSpec(src.Spec)

	if src.Spec.Mirror != nil {
		dst.Spec.Mirror = &konnectv1alpha2.MirrorSpec{
			Konnect: konnectv1alpha2.MirrorKonnect{
				ID: src.Spec.Mirror.Konnect.ID,
			},
		}
	} else {
		dst.Spec.Mirror = nil
	}
	dst.Spec.Source = src.Spec.Source
	dst.Spec.Members = src.Spec.Members

	dst.Spec.KonnectConfiguration = src.Spec.KonnectConfiguration

	return nil
}

// ConvertFrom converts the Hub version (v1alpha2) to this KonnectGatewayControlPlane (v1alpha1).
func (dst *KonnectGatewayControlPlane) ConvertFrom(srcRaw conversion.Hub) error { //nolint:staticcheck // ST1016 methods on the same type can have different receiver names

	src, ok := srcRaw.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return fmt.Errorf(errWrongConvertFromKonnectGatewayControlPlane, srcRaw)
	}

	dst.ObjectMeta = src.ObjectMeta

	dst.Spec.CreateControlPlaneRequest = createInlineControlPlaneRequest(src.Spec.CreateControlPlaneRequest)
	if src.Spec.Mirror != nil {
		dst.Spec.Mirror = &MirrorSpec{
			Konnect: MirrorKonnect{
				ID: src.Spec.Mirror.Konnect.ID,
			},
		}
	} else {
		dst.Spec.Mirror = nil
	}
	dst.Spec.Source = src.Spec.Source
	dst.Spec.Members = src.Spec.Members

	dst.Spec.KonnectConfiguration = src.Spec.KonnectConfiguration

	return nil
}

// createControlplaneRequestFromSpec converts a KonnectGatewayControlPlaneSpec to a CreateControlPlaneRequest
// handling nil pointers and type conversions appropriately.
func createControlplaneRequestFromSpec(spec KonnectGatewayControlPlaneSpec) *sdkkonnectcomp.CreateControlPlaneRequest {
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
		Name:         lo.ToPtr(req.Name),
		Description:  req.Description,
		ClusterType:  req.ClusterType,
		AuthType:     req.AuthType,
		CloudGateway: req.CloudGateway,
		ProxyUrls:    req.ProxyUrls,
		Labels:       req.Labels,
	}
}
