package ops

import (
	"context"
	"fmt"
	"sort"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/iter"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

// ensureControlPlane ensures that the Konnect ControlPlane exists in Konnect. It is created
// if it does not exist and the source is Origin. If the source is Mirror, it checks
// if the ControlPlane exists in Konnect and returns an error if it does not.
func ensureControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	switch *cp.Spec.Source {
	case commonv1alpha1.EntitySourceOrigin:
		return createControlPlane(ctx, sdk, sdkGroups, cl, cp)
	case commonv1alpha1.EntitySourceMirror:
		resp, err := GetControlPlaneByID(
			ctx,
			sdk,
			// not nilness is ensured by CEL rules
			string(cp.Spec.Mirror.Konnect.ID),
		)
		if err != nil {
			return err
		}
		cp.SetKonnectID(string(cp.Spec.Mirror.Konnect.ID))
		cp.Status.ClusterType = resp.Config.ClusterType
		return nil
	default:
		// This should never happen, as the source type is validated by CEL rules.
		return fmt.Errorf("unsupported source type: %s", *cp.Spec.Source)
	}
}

// createControlPlane creates the ControlPlane as specified in provided ControlPlane's
// spec. Besides creating the ControlPlane, it also creates the group membership if the
// ControlPlane is a group. If the group membership creation fails, KonnectEntityCreatedButRelationsFailedError
// is returned so it can be handled properly downstream.
func createControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	req := cp.Spec.CreateControlPlaneRequest
	req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)

	resp, err := sdk.CreateControlPlane(ctx, *req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ControlPlane == nil || resp.ControlPlane.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	// At this point, the ControlPlane has been created in Konnect.
	id := resp.ControlPlane.ID
	cp.SetKonnectID(id)
	cp.Status.ClusterType = resp.ControlPlane.Config.ClusterType

	if err := setGroupMembers(ctx, cl, cp, id, sdkGroups); err != nil {
		// If we failed to set group membership, we should return a specific error with a reason
		// so the downstream can handle it properly.
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: id,
			Err:       err,
			Reason:    konnectv1alpha1.KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers,
		}
	}

	return nil
}

// deleteControlPlane deletes a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
func deleteControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	// if the source type is Mirror, don't touch the Konnect entity.
	if isMirrorEntity(cp) {
		return nil
	}
	id := cp.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteControlPlane(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		return handleDeleteError(ctx, err, cp)
	}

	return nil
}

// updateControlPlane updates a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
// Besides updating the ControlPlane, it also updates the group membership if the ControlPlane is a group.
// If the group membership update fails, KonnectEntityCreatedButRelationsFailedError is returned so it can
// be handled properly downstream.
func updateControlPlane(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) error {
	// if the source type is Mirror, don't touch the Konnect entity.
	if isMirrorEntity(cp) {
		return nil
	}
	id := cp.GetKonnectStatus().GetKonnectID()
	req := sdkkonnectcomp.UpdateControlPlaneRequest{
		Name:        sdkkonnectgo.String(cp.GetKonnectName()),
		Description: cp.GetKonnectDescription(),
		AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.GetKonnectAuthType()),
		ProxyUrls:   cp.GetKonnectProxyURLs(),
		Labels:      WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
	}

	resp, err := sdk.UpdateControlPlane(ctx, id, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cp); errWrap != nil {
		return handleUpdateError(ctx, err, cp, func(ctx context.Context) error {
			return createControlPlane(ctx, sdk, sdkGroups, cl, cp)
		})
	}

	if resp == nil || resp.ControlPlane == nil {
		return fmt.Errorf("failed updating ControlPlane: %w", ErrNilResponse)
	}
	id = resp.ControlPlane.ID

	if err := setGroupMembers(ctx, cl, cp, id, sdkGroups); err != nil {
		// If we failed to set group membership, we should return a specific error with a reason
		// so the downstream can handle it properly.
		return KonnectEntityCreatedButRelationsFailedError{
			KonnectID: id,
			Err:       err,
			Reason:    konnectv1alpha1.KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers,
		}
	}

	return nil
}

func setGroupMembers(
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	id string,
	sdkGroups sdkops.ControlPlaneGroupSDK,
) error {
	// if the source type is Mirror, don't touch the Konnect entity.
	if isMirrorEntity(cp) {
		return nil
	}
	if cp.GetKonnectClusterType() == nil ||
		*cp.GetKonnectClusterType() != sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup {
		return nil
	}

	members, err := iter.MapErr(cp.Spec.Members,
		func(member *corev1.LocalObjectReference) (sdkkonnectcomp.Members, error) {
			var (
				memberCP konnectv1alpha2.KonnectGatewayControlPlane
				nn       = client.ObjectKey{
					Namespace: cp.Namespace,
					Name:      member.Name,
				}
			)
			if err := cl.Get(ctx, nn, &memberCP); err != nil {
				return sdkkonnectcomp.Members{},
					GetControlPlaneGroupMemberFailedError{
						MemberName: memberCP.Name,
						Err:        err,
					}
			}
			if memberCP.GetKonnectID() == "" {
				return sdkkonnectcomp.Members{},
					ControlPlaneGroupMemberNoKonnectIDError{
						GroupName:  cp.Name,
						MemberName: memberCP.Name,
					}
			}
			return sdkkonnectcomp.Members{
				ID: memberCP.GetKonnectID(),
			}, nil
		})
	if err != nil {
		SetControlPlaneGroupMembersReferenceResolvedConditionFalse(
			cp,
			ControlPlaneGroupMembersReferenceResolvedReasonPartialNotResolved,
			err.Error(),
		)
		return fmt.Errorf("failed to set group members, some members couldn't be found: %w", err)
	}

	sort.Sort(membersByID(members))
	gm := sdkkonnectcomp.GroupMembership{
		Members: members,
	}
	_, err = sdkGroups.PutControlPlanesIDGroupMemberships(ctx, id, &gm)
	if err != nil {
		SetControlPlaneGroupMembersReferenceResolvedConditionFalse(
			cp,
			ControlPlaneGroupMembersReferenceResolvedReasonFailedToSet,
			err.Error(),
		)
		return fmt.Errorf("failed to set members on control plane group %s: %w",
			client.ObjectKeyFromObject(cp), err,
		)
	}

	SetControlPlaneGroupMembersReferenceResolvedCondition(
		cp,
	)
	return nil
}

type membersByID []sdkkonnectcomp.Members

func (m membersByID) Len() int           { return len(m) }
func (m membersByID) Less(i, j int) bool { return m[i].ID < m[j].ID }
func (m membersByID) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

// getControlPlaneForUID returns the Konnect ID of the Konnect ControlPlane
// that matches the UID of the provided KonnectGatewayControlPlane.
func getControlPlaneForUID(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) (string, error) {
	reqList := sdkkonnectops.ListControlPlanesRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		FilterLabels: lo.ToPtr(UIDLabelForObject(cp)),
	}

	resp, err := sdk.ListControlPlanes(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), err)
	}

	if resp == nil || resp.ListControlPlanesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	id, err := getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.ListControlPlanesResponse.Data), cp)
	if err != nil {
		return "", err
	}

	if err := setGroupMembers(ctx, cl, cp, id, sdkGroups); err != nil {
		// If we failed to set group membership, we should return a specific error with a reason
		// so the downstream can handle it properly.
		return id, KonnectEntityCreatedButRelationsFailedError{
			KonnectID: id,
			Err:       err,
			Reason:    konnectv1alpha1.KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers,
		}
	}

	return id, nil
}

// GetControlPlaneByID returns the Konnect ControlPlane that matches the provided ID.
func GetControlPlaneByID(
	ctx context.Context,
	sdk sdkkonnectgo.ControlPlanesSDK,
	id string,
) (*sdkkonnectcomp.ControlPlane, error) {
	reqList := sdkkonnectops.ListControlPlanesRequest{
		Filter: &sdkkonnectcomp.ControlPlaneFilterParameters{
			ID: &sdkkonnectcomp.ID{
				Eq: lo.ToPtr(id),
			},
		},
	}

	resp, err := sdk.ListControlPlanes(ctx, reqList)
	if err != nil || resp == nil || resp.ListControlPlanesResponse == nil {
		return nil, fmt.Errorf("failed listing for controlPlane with id %s: %w", id, err)
	}

	if len(resp.ListControlPlanesResponse.Data) == 0 {
		return nil, fmt.Errorf("failed listing controlPlanes by id: %w", EntityWithMatchingIDNotFoundError{ID: id})
	}

	// This should never happen, as ID is unique.
	if len(resp.ListControlPlanesResponse.Data) > 1 {
		return nil, fmt.Errorf("failed listing controlPlanes by id: %w", MultipleEntitiesWithMatchingIDFoundError{ID: id})
	}

	return &resp.ListControlPlanesResponse.Data[0], nil
}
