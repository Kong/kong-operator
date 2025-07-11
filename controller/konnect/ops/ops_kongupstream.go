package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
)

func createUpstream(
	ctx context.Context,
	sdk sdkops.UpstreamsSDK,
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
	sdk sdkops.UpstreamsSDK,
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
		return errWrap
	}

	return nil
}

// deleteUpstream deletes a KongUpstream in Konnect.
// It is assumed that provided KongUpstream has Konnect ID set in status.
// It returns an error if the operation fails.
func deleteUpstream(
	ctx context.Context,
	sdk sdkops.UpstreamsSDK,
	upstream *configurationv1alpha1.KongUpstream,
) error {
	id := upstream.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteUpstream(ctx, upstream.Status.Konnect.ControlPlaneID, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, upstream); errWrap != nil {
		return handleDeleteError(ctx, err, upstream)
	}

	return nil
}

func kongUpstreamToSDKUpstreamInput(
	upstream *configurationv1alpha1.KongUpstream,
) sdkkonnectcomp.Upstream {
	return sdkkonnectcomp.Upstream{
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
	sdk sdkops.UpstreamsSDK,
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

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), u)
}
