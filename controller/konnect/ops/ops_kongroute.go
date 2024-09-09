package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, route); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				route.GetGeneration(),
			),
			route,
		)
		return errWrapped
	}

	route.Status.Konnect.SetKonnectID(*resp.Route.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			route.GetGeneration(),
		),
		route,
	)

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
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, route); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				route.GetGeneration(),
			),
			route,
		)
		return errWrapped
	}

	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			route.GetGeneration(),
		),
		route,
	)

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
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, route); errWrapped != nil {
		// Service delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
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
			Err: errWrapped,
		}
	}

	return nil
}

func kongRouteToSDKRouteInput(
	route *configurationv1alpha1.KongRoute,
) sdkkonnectcomp.RouteInput {
	return sdkkonnectcomp.RouteInput{
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
		Tags:                    append(route.Spec.KongRouteAPISpec.Tags, GenerateKubernetesMetadataTags(route)...),
		Service: &sdkkonnectcomp.RouteService{
			ID: sdkkonnectgo.String(route.Status.Konnect.ServiceID),
		},
	}
}
