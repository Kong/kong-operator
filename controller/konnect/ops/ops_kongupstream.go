package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func createUpstream(
	ctx context.Context,
	sdk UpstreamsSDK,
	upstream *configurationv1alpha1.KongUpstream,
) error {
	if upstream.GetControlPlaneID() == "" {
		return fmt.Errorf(
			"can't create %T %s without a Konnect ControlPlane ID",
			upstream, client.ObjectKeyFromObject(upstream),
		)
	}

	resp, err := sdk.CreateUpstream(ctx,
		upstream.Status.Konnect.ControlPlaneID,
		kongUpstreamToSDKUpstreamInput(upstream),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, upstream); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(upstream, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	upstream.Status.Konnect.SetKonnectID(*resp.Upstream.ID)
	SetKonnectEntityProgrammedCondition(upstream)

	return nil
}

// updateUpstream updates the Konnect Upstream entity.
// It is assumed that provided KongUpstream has Konnect ID set in status.
// It returns an error if the KongUpstream does not have a ControlPlaneRef or
// if the operation fails.
func updateUpstream(
	ctx context.Context,
	sdk UpstreamsSDK,
	upstream *configurationv1alpha1.KongUpstream,
) error {
	if upstream.GetControlPlaneID() == "" {
		return fmt.Errorf("can't update %T %s without a Konnect ControlPlane ID",
			upstream, client.ObjectKeyFromObject(upstream),
		)
	}

	id := upstream.GetKonnectStatus().GetKonnectID()
	_, err := sdk.UpsertUpstream(ctx,
		sdkkonnectops.UpsertUpstreamRequest{
			ControlPlaneID: upstream.GetControlPlaneID(),
			UpstreamID:     id,
			Upstream:       kongUpstreamToSDKUpstreamInput(upstream),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, upstream); errWrap != nil {
		// Upstream update operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				if err := createUpstream(ctx, sdk, upstream); err != nil {
					return FailedKonnectOpError[configurationv1alpha1.KongUpstream]{
						Op:  UpdateOp,
						Err: err,
					}
				}
				// Create succeeded, createUpstream sets the status so no need to do this here.

				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongUpstream]{
					Op:  UpdateOp,
					Err: sdkError,
				}
			}
		}

		SetKonnectEntityProgrammedConditionFalse(upstream, "FailedToUpdate", errWrap.Error())
		return errWrap
	}

	SetKonnectEntityProgrammedCondition(upstream)

	return nil
}

// deleteUpstream deletes a KongUpstream in Konnect.
// It is assumed that provided KongUpstream has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteUpstream(
	ctx context.Context,
	sdk UpstreamsSDK,
	svc *configurationv1alpha1.KongUpstream,
) error {
	id := svc.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteUpstream(ctx, svc.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, svc); errWrap != nil {
		// Upstream delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			switch sdkError.StatusCode {
			case 404:
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", svc.GetTypeName(), "id", id,
					)
				return nil
			default:
				return FailedKonnectOpError[configurationv1alpha1.KongUpstream]{
					Op:  DeleteOp,
					Err: sdkError,
				}
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongUpstream]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

func kongUpstreamToSDKUpstreamInput(
	upstream *configurationv1alpha1.KongUpstream,
) sdkkonnectcomp.UpstreamInput {
	var (
		specTags       = upstream.Spec.Tags
		annotationTags = metadata.ExtractTags(upstream)
		k8sTags        = GenerateKubernetesMetadataTags(upstream)
	)
	// Deduplicate tags to avoid rejection by Konnect.
	tags := lo.Uniq(slices.Concat(specTags, annotationTags, k8sTags))

	return sdkkonnectcomp.UpstreamInput{
		Algorithm:              upstream.Spec.Algorithm,
		ClientCertificate:      upstream.Spec.ClientCertificate,
		HashFallback:           upstream.Spec.HashFallback,
		HashFallbackHeader:     upstream.Spec.HashFallbackHeader,
		HashFallbackQueryArg:   upstream.Spec.HashFallbackQueryArg,
		HashFallbackURICapture: upstream.Spec.HashFallbackURICapture,
		HashOn:                 upstream.Spec.HashOn,
		HashOnCookie:           upstream.Spec.HashOnCookie,
		HashOnCookiePath:       upstream.Spec.HashOnCookiePath,
		HashOnHeader:           upstream.Spec.HashOnHeader,
		HashOnQueryArg:         upstream.Spec.HashOnQueryArg,
		HashOnURICapture:       upstream.Spec.HashOnURICapture,
		Healthchecks:           upstream.Spec.Healthchecks,
		HostHeader:             upstream.Spec.HostHeader,
		Name:                   upstream.Spec.Name,
		Slots:                  upstream.Spec.Slots,
		Tags:                   tags,
		UseSrvName:             upstream.Spec.UseSrvName,
	}
}
