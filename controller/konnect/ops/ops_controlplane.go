package ops

import (
	"context"
	"errors"
	"fmt"
	"sort"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/iter"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func createControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	sdkGroups ControlPlaneGroupSDK,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	req := cp.Spec.CreateControlPlaneRequest
	req.Labels = WithKubernetesMetadataLabels(cp, req.Labels)
	resp, err := sdk.CreateControlPlane(ctx, req)
	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	// TODO: implement entity adoption https://github.com/Kong/gateway-operator/issues/460
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cp); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cp, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	cp.Status.SetKonnectID(*resp.ControlPlane.ID)

	if err := setGroupMembers(ctx, cl, cp, sdkGroups); err != nil {
		SetKonnectEntityProgrammedConditionFalse(cp, conditions.KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers, err.Error())
		return err
	}

	SetKonnectEntityProgrammedCondition(cp)

	return nil
}

// deleteControlPlane deletes a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
func deleteControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) error {
	id := cp.GetKonnectStatus().GetKonnectID()
	_, err := sdk.DeleteControlPlane(ctx, id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cp); errWrap != nil {
		var sdkNotFoundError *sdkkonnecterrs.NotFoundError
		if errors.As(err, &sdkNotFoundError) {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", cp.GetTypeName(), "id", id,
				)
			return nil
		}
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) {
			return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

// updateControlPlane updates a Konnect ControlPlane.
// It is assumed that the Konnect ControlPlane has a Konnect ID.
// It returns an error if the operation fails.
func updateControlPlane(
	ctx context.Context,
	sdk ControlPlaneSDK,
	sdkGroups ControlPlaneGroupSDK,
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
	var sdkError *sdkkonnecterrs.NotFoundError
	if errors.As(err, &sdkError) {
		ctrllog.FromContext(ctx).
			Info("entity not found in Konnect, trying to recreate",
				"type", cp.GetTypeName(), "id", id,
			)
		if err := createControlPlane(ctx, sdk, sdkGroups, cl, cp); err != nil {
			return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
				Op:  UpdateOp,
				Err: err,
			}
		}
		// Create succeeded, createControlPlane sets the status so no need to do this here.

		return nil
	}

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cp); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(cp, "FailedToUpdate", errWrap.Error())
		return FailedKonnectOpError[konnectv1alpha1.KonnectGatewayControlPlane]{
			Op:  UpdateOp,
			Err: errWrap,
		}
	}

	cp.Status.SetKonnectID(*resp.ControlPlane.ID)

	if err := setGroupMembers(ctx, cl, cp, sdkGroups); err != nil {
		SetKonnectEntityProgrammedConditionFalse(cp, conditions.KonnectGatewayControlPlaneProgrammedReasonFailedToSetControlPlaneGroupMembers, err.Error())
		return err
	}

	SetKonnectEntityProgrammedCondition(cp)

	return nil
}

func setGroupMembers(
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	sdkGroups ControlPlaneGroupSDK,
) error {
	if len(cp.Spec.Members) == 0 ||
		cp.Spec.ClusterType == nil ||
		*cp.Spec.ClusterType != sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup {
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
	_, err = sdkGroups.PutControlPlanesIDGroupMemberships(ctx, cp.GetKonnectID(), &gm)
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
