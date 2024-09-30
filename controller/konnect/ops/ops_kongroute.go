package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func createRoute(
	ctx context.Context,
	sdk RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	if route.GetControlPlaneID() == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", route, client.ObjectKeyFromObject(route))
	}

	resp, err := sdk.CreateRoute(ctx, route.Status.Konnect.ControlPlaneID, kongRouteToSDKRouteInput(route))

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, route); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(route, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	route.Status.Konnect.SetKonnectID(*resp.Route.ID)
	SetKonnectEntityProgrammedCondition(route)

	return nil
}

// updateRoute updates the Konnect Route entity.
// It is assumed that provided KongRoute has Konnect ID set in status.
// It returns an error if the KongRoute does not have a ControlPlaneRef or
// if the operation fails.
func updateRoute(
	ctx context.Context,
	sdk RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	cpID := route.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T %s without a Konnect ControlPlane ID", route, client.ObjectKeyFromObject(route))
	}

	_, err := sdk.UpsertRoute(ctx, sdkkonnectops.UpsertRouteRequest{
		ControlPlaneID: cpID,
		RouteID:        route.Status.Konnect.ID,
		Route:          kongRouteToSDKRouteInput(route),
	})

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, route); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(route, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(route)

	return nil
}

// deleteRoute deletes a KongRoute in Konnect.
// It is assumed that provided KongRoute has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteRoute(
	ctx context.Context,
	sdk RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	id := route.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteRoute(ctx, route.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, route); errWrap != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", route.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongRoute]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongService]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func kongRouteToSDKRouteInput(
	route *configurationv1alpha1.KongRoute,
) sdkkonnectcomp.RouteInput {
	var (
		specTags       = route.Spec.KongRouteAPISpec.Tags
		annotationTags = metadata.ExtractTags(route)
		k8sTags        = GenerateKubernetesMetadataTags(route)
	)
	// Deduplicate tags to avoid rejection by Konnect.
	tags := lo.Uniq(slices.Concat(specTags, annotationTags, k8sTags))

	r := sdkkonnectcomp.RouteInput{
		Destinations:            route.Spec.KongRouteAPISpec.Destinations,
		Headers:                 route.Spec.KongRouteAPISpec.Headers,
		Hosts:                   route.Spec.KongRouteAPISpec.Hosts,
		HTTPSRedirectStatusCode: route.Spec.KongRouteAPISpec.HTTPSRedirectStatusCode,
		Methods:                 route.Spec.KongRouteAPISpec.Methods,
		Name:                    route.Spec.KongRouteAPISpec.Name,
		PathHandling:            route.Spec.KongRouteAPISpec.PathHandling,
		Paths:                   route.Spec.KongRouteAPISpec.Paths,
		PreserveHost:            route.Spec.KongRouteAPISpec.PreserveHost,
		Protocols:               route.Spec.KongRouteAPISpec.Protocols,
		RegexPriority:           route.Spec.KongRouteAPISpec.RegexPriority,
		RequestBuffering:        route.Spec.KongRouteAPISpec.RequestBuffering,
		ResponseBuffering:       route.Spec.KongRouteAPISpec.ResponseBuffering,
		Snis:                    route.Spec.KongRouteAPISpec.Snis,
		Sources:                 route.Spec.KongRouteAPISpec.Sources,
		StripPath:               route.Spec.KongRouteAPISpec.StripPath,
		Tags:                    tags,
	}
	if route.Status.Konnect.ServiceID != "" {
		r.Service = &sdkkonnectcomp.RouteService{
			ID: sdkkonnectgo.String(route.Status.Konnect.ServiceID),
		}
	}
	return r
}
