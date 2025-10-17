package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
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

// adoptRoute adopts an existing route with the ID
// given in the spec.adopt.konnect.id of the KongRoute.
// It returns an error if the operation fails.
func adoptRoute(
	ctx context.Context,
	sdk sdkops.RoutesSDK,
	route *configurationv1alpha1.KongRoute,
) error {
	cpID := route.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}

	adoptOptions := route.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetRoute(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	// KO only supports routes with "RouteJSON" type now.
	if resp.Route.Type != sdkkonnectcomp.RouteTypeRouteJSON {
		return fmt.Errorf("failed to adopt: route type %q not supported", resp.Route.Type)
	}
	if resp.Route.RouteJSON == nil {
		return fmt.Errorf("route content in RouteJSON is empty")
	}

	// Check if the service ID matches.
	if route.Spec.ServiceRef != nil {
		// if the KongRoute has a service reference, check if the referenced service matches.
		if route.Status.Konnect.ServiceID == "" {
			return fmt.Errorf("failed to adopt: service reference not resolved")
		}
		if resp.Route.RouteJSON.Service == nil ||
			resp.Route.RouteJSON.Service.ID == nil {
			return fmt.Errorf("failed to adopt: existing route does not have service reference")
		}
		if *resp.Route.RouteJSON.Service.ID != route.Status.Konnect.ServiceID {
			return fmt.Errorf("failed to adopt: reference service ID does not match")
		}
	} else if resp.Route.RouteJSON.Service != nil {
		// if the KongRoute does not have a service reference, the existing route should not have a reference service.
		return fmt.Errorf("failed to adopt: KongRoute has no service reference but existing route has service reference")
	}

	uidTag, hasUIDTag := findUIDTag(resp.Route.RouteJSON.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(route.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}

	switch adoptMode {
	case commonv1alpha1.AdoptModeOverride:
		routeCopy := route.DeepCopy()
		routeCopy.SetKonnectID(konnectID)
		if err = updateRoute(ctx, sdk, routeCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !routeJSONMatch(resp.Route.RouteJSON, route) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	route.SetKonnectID(konnectID)

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

// routeHeadersMatch compares two header matches in the two routes.
func routeHeadersMatch(headers1, headers2 map[string][]string) bool {
	// If they are both `nil`, they are equal.
	if headers1 == nil && headers2 == nil {
		return true
	}
	// If one of them is nil but not both, or they have different number of elements, they are not equal.
	if headers1 == nil || headers2 == nil || len(headers1) != len(headers2) {
		return false
	}
	// Check each header name in one map inside another.
	// If all the headers can be found, and the values are the same, they are equal
	// (Given that they have the same number of headers).
	// If any header is not found, or the matching values differ, they are not equal.
	for k1, v1 := range headers1 {
		v2, ok := headers2[k1]
		if !ok || !lo.ElementsMatch(v1, v2) {
			return false
		}
	}
	return true
}

// routeJSONMatch compares a Konnect in RouteJSON type with the spec of a KongRoute.
func routeJSONMatch(routeJSON *sdkkonnectcomp.RouteJSON, route *configurationv1alpha1.KongRoute) bool {
	routeJSONInput := kongRouteToSDKRouteInput(route).RouteJSON
	return equalWithDefault(routeJSON.Name, routeJSONInput.Name, "") &&
		lo.ElementsMatch(routeJSON.Hosts, routeJSONInput.Hosts) &&
		lo.ElementsMatch(routeJSON.Paths, routeJSONInput.Paths) &&
		lo.ElementsMatch(routeJSON.Methods, routeJSONInput.Methods) &&
		routeHeadersMatch(routeJSON.Headers, routeJSONInput.Headers) &&
		equalWithDefault(routeJSON.RegexPriority, routeJSONInput.RegexPriority, 0) &&
		equalWithDefault(routeJSON.HTTPSRedirectStatusCode, routeJSONInput.HTTPSRedirectStatusCode,
			sdkkonnectcomp.HTTPSRedirectStatusCodeFourHundredAndTwentySix) &&
		lo.ElementsMatch(routeJSON.Snis, routeJSONInput.Snis) &&
		equalWithDefault(routeJSON.PathHandling, routeJSONInput.PathHandling,
			sdkkonnectcomp.PathHandlingV0) &&
		lo.ElementsMatch(routeJSON.Sources, routeJSONInput.Sources) &&
		lo.ElementsMatch(routeJSON.Destinations, routeJSONInput.Destinations)

}
