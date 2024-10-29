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

	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// createControlPlane creates the ControlPlane as specified in provided ControlPlane's
// spec. Besides creating the ControlPlane, it also creates the group membership if the
// ControlPlane is a group. If the group membership creation fails, KonnectEntityCreatedButRelationsFailedError
// is returned so it can be handled properly downstream.
func createControlPlane(
	ctx context.Context,
	sdk sdkops.ControlPlaneSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	req := cp.Spec.CreateControlPlaneRequest
	req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)

	resp, err := sdk.CreateControlPlane(ctx, req)

	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.ControlPlane == nil || resp.ControlPlane.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	// At this point, the ControlPlane has been created in Konnect.
	id := resp.ControlPlane.ID
	cp.SetKonnectID(id)

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
	sdk sdkops.ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
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
	sdk sdkops.ControlPlaneSDK,
	sdkGroups sdkops.ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	req := sdkkonnectcomp.UpdateControlPlaneRequest{
		Name:        sdkkonnectgo.String(cp.Spec.Name),
		Description: cp.Spec.Description,
		AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
		ProxyUrls:   cp.Spec.ProxyUrls,
		Labels:      WithKubernetesMetadataLabels(cp, cp.Spec.Labels),
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
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	id string,
	sdkGroups sdkops.ControlPlaneGroupSDK,
) error {
	if len(cp.Spec.Members) == 0 ||
		cp.Spec.ClusterType == nil ||
		*cp.Spec.ClusterType != sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup {
		return nil
	}

	members, err := iter.MapErr(cp.Spec.Members,
		func(member *corev1.LocalObjectReference) (sdkkonnectcomp.Members, error) {
			var (
				memberCP konnectv1alpha1.KonnectGatewayControlPlane
				nn       = client.ObjectKey{
					Namespace: cp.Namespace,
					Name:      member.Name,
				}
			)
			if err := cl.Get(ctx, nn, &memberCP); err != nil {
				return sdkkonnectcomp.Members{},
					fmt.Errorf("failed to get control plane group member %s: %w", member.Name, err)
			}
			if memberCP.GetKonnectID() == "" {
				return sdkkonnectcomp.Members{},
					fmt.Errorf("control plane group %s member %s has no Konnect ID", cp.Name, member.Name)
			}
			return sdkkonnectcomp.Members{
				ID: lo.ToPtr(memberCP.GetKonnectID()),
			}, nil
		})
	if err != nil {
		return fmt.Errorf("failed to set group members, some members couldn't be found: %w", err)
	}

	sort.Sort(membersByID(members))
	gm := sdkkonnectcomp.GroupMembership{
		Members: members,
	}
	_, err = sdkGroups.PutControlPlanesIDGroupMemberships(ctx, id, &gm)
	if err != nil {
		return fmt.Errorf("failed to set members on control plane group %s: %w",
			client.ObjectKeyFromObject(cp), err,
		)
	}

	return nil
}

type membersByID []sdkkonnectcomp.Members

func (m membersByID) Len() int           { return len(m) }
func (m membersByID) Less(i, j int) bool { return *m[i].ID < *m[j].ID }
func (m membersByID) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

// getControlPlaneForUID returns the Konnect ID of the Konnect ControlPlane
// that matches the UID of the provided KonnectGatewayControlPlane.
func getControlPlaneForUID(
	ctx context.Context,
	sdk sdkops.ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) (string, error) {
	reqList := sdkkonnectops.ListControlPlanesRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		Labels: lo.ToPtr(UIDLabelForObject(cp)),
	}

	resp, err := sdk.ListControlPlanes(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), err)
	}

	if resp == nil || resp.ListControlPlanesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", cp.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDSlice(resp.ListControlPlanesResponse.Data), cp)
}
