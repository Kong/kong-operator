package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnectgoerrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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
	// sdk *sdkkonnectgo.SDK,
	sdk RoutesSDK,
	cl client.Client,
	route *configurationv1alpha1.KongRoute,
) error {
	if route.Spec.ServiceRef == nil {
		return fmt.Errorf("can't update %T without a ServiceRef", route)
	}

	// TODO(pmalek) handle other types of CP ref
	nnSvc := types.NamespacedName{
		Namespace: route.Namespace,
		Name:      route.Spec.ServiceRef.NamespacedRef.Name,
	}
	var svc configurationv1alpha1.KongService
	if err := cl.Get(ctx, nnSvc, &svc); err != nil {
		return fmt.Errorf("failed to get KongService %s: for KongRoute %s: %w",
			nnSvc, client.ObjectKeyFromObject(route), err,
		)
	}

	if svc.Status.Konnect.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KongService %s does not have the Konnect ID",
			route, nnSvc,
		)
	}

	var cp konnectv1alpha1.KonnectControlPlane
	nnCP := types.NamespacedName{
		Namespace: svc.Namespace,
		Name:      svc.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectControlPlane %s: for KongRoute %s: %w",
			nnSvc, client.ObjectKeyFromObject(route), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectControlPlane %s does not have the Konnect ID",
			route, nnSvc,
		)
	}

	resp, err := sdk.UpsertRoute(ctx, sdkkonnectgoops.UpsertRouteRequest{
		// resp, err := sdk.UpsertRoute(ctx, sdkkonnectgoops.UpsertRouteRequest{
		ControlPlaneID: cp.Status.ID,
		RouteID:        route.Status.Konnect.ID,
		Route:          kongRouteToSDKRouteInput(route),
	},
	)

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

	route.Status.Konnect.SetKonnectID(*resp.Route.ID)
	route.Status.Konnect.SetControlPlaneID(cp.Status.ID)
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
		var sdkError *sdkkonnectgoerrs.SDKError
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
) sdkkonnectgocomp.RouteInput {
	return sdkkonnectgocomp.RouteInput{
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
		Tags:                    route.Spec.KongRouteAPISpec.Tags,
		Service: &sdkkonnectgocomp.RouteService{
			ID: sdkkonnectgo.String(route.Status.Konnect.ServiceID),
		},
	}
}
