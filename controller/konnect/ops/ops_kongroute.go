package ops

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

func createRoute(
	ctx context.Context,
	sdk sdkops.RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	if route.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: route, Op: CreateOp}
	}

	resp, err := sdk.CreateRoute(ctx, route.Status.Konnect.ControlPlaneID, kongRouteToSDKRouteInput(route))

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, route); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Route == nil || resp.Route.RouteJSON.ID == nil {
		return fmt.Errorf("failed creating %s: %w", route.GetTypeName(), ErrNilResponse)
	}

	route.SetKonnectID(*resp.Route.RouteJSON.ID)

	return nil
}

// updateRoute updates the Konnect Route entity.
// It is assumed that provided KongRoute has Konnect ID set in status.
// It returns an error if the KongRoute does not have a ControlPlaneRef or
// if the operation fails.
func updateRoute(
	ctx context.Context,
	sdk sdkops.RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	cpID := route.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: route, Op: UpdateOp}
	}

	id := route.GetKonnectStatus().GetKonnectID()
	_, err := sdk.UpsertRoute(ctx, sdkkonnectops.UpsertRouteRequest{
		ControlPlaneID: cpID,
		RouteID:        id,
		Route:          kongRouteToSDKRouteInput(route),
	})

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, route); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteRoute deletes a KongRoute in Konnect.
// It is assumed that provided KongRoute has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteRoute(
	ctx context.Context,
	sdk sdkops.RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	id := route.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteRoute(ctx, route.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, route); errWrap != nil {
		return handleDeleteError(ctx, err, route)
	}

	return nil
}

func kongRouteToSDKRouteInput(
	route *configurationv1alpha1.KongRoute,
) sdkkonnectcomp.Route {
	r := sdkkonnectcomp.Route{
		RouteJSON: &sdkkonnectcomp.RouteJSON{
			Destinations:            route.Spec.Destinations,
			Headers:                 route.Spec.Headers,
			Hosts:                   route.Spec.Hosts,
			HTTPSRedirectStatusCode: route.Spec.HTTPSRedirectStatusCode,
			Methods:                 route.Spec.Methods,
			Name:                    route.Spec.Name,
			PathHandling:            route.Spec.PathHandling,
			Paths:                   route.Spec.Paths,
			PreserveHost:            route.Spec.PreserveHost,
			Protocols:               route.Spec.Protocols,
			RegexPriority:           route.Spec.RegexPriority,
			RequestBuffering:        route.Spec.RequestBuffering,
			ResponseBuffering:       route.Spec.ResponseBuffering,
			Snis:                    route.Spec.Snis,
			Sources:                 route.Spec.Sources,
			StripPath:               route.Spec.StripPath,
			Tags:                    GenerateTagsForObject(route, route.Spec.Tags...),
		},
	}
	if route.Status.Konnect != nil && route.Status.Konnect.ServiceID != "" {
		r.RouteJSON.Service = &sdkkonnectcomp.RouteJSONService{
			ID: sdkkonnectgo.String(route.Status.Konnect.ServiceID),
		}
	}
	return r
}

// getKongRouteForUID returns the Konnect ID of the KongRoute
// that matches the UID of the provided KongRoute.
func getKongRouteForUID(
	ctx context.Context,
	sdk sdkops.RoutesSDK,
	r *configurationv1alpha1.KongRoute,
) (string, error) {
	reqList := sdkkonnectops.ListRouteRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		Tags:           lo.ToPtr(UIDLabelForObject(r)),
		ControlPlaneID: r.GetControlPlaneID(),
	}

	resp, err := sdk.ListRoute(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", r.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", r.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(
		sliceToEntityWithIDPtrSlice(
			lo.Map(resp.Object.Data, func(route sdkkonnectcomp.Route, _ int) sdkkonnectcomp.RouteJSON {
				return *route.RouteJSON
			}),
		), r)
}
