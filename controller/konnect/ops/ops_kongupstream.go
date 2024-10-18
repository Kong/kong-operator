package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func createUpstream(
	ctx context.Context,
	sdk UpstreamsSDK,
	upstream *configurationv1alpha1.KongUpstream,
) error {
	if upstream.GetControlPlaneID() == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: upstream, Op: CreateOp}
	}

	resp, err := sdk.CreateUpstream(ctx,
		upstream.Status.Konnect.ControlPlaneID,
		kongUpstreamToSDKUpstreamInput(upstream),
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, upstream); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Upstream == nil || resp.Upstream.ID == nil {
		return fmt.Errorf("failed creating %s: %w", upstream.GetTypeName(), ErrNilResponse)
	}

	upstream.SetKonnectID(*resp.Upstream.ID)

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
		return CantPerformOperationWithoutControlPlaneIDError{Entity: upstream, Op: UpdateOp}
	}

	id := upstream.GetKonnectStatus().GetKonnectID()
	_, err := sdk.UpsertUpstream(ctx,
		sdkkonnectops.UpsertUpstreamRequest{
			ControlPlaneID: upstream.GetControlPlaneID(),
			UpstreamID:     id,
			Upstream:       kongUpstreamToSDKUpstreamInput(upstream),
		},
	)

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

		return errWrap
	}

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
		Tags:                   GenerateTagsForObject(upstream, upstream.Spec.Tags...),
		UseSrvName:             upstream.Spec.UseSrvName,
	}
}

// getKongUpstreamForUID lists upstreams in Konnect with given k8s uid as its tag.
func getKongUpstreamForUID(
	ctx context.Context,
	sdk UpstreamsSDK,
	u *configurationv1alpha1.KongUpstream,
) (string, error) {
	cpID := u.GetControlPlaneID()

	reqList := sdkkonnectops.ListUpstreamRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(u)),
	}

	resp, err := sdk.ListUpstream(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", u.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", u.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.Object.Data), u)
}
