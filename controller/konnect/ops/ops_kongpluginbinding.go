package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// -----------------------------------------------------------------------------
// Konnect KongPlugin - ops functions
// -----------------------------------------------------------------------------

// createPlugin creates the Konnect Plugin entity.
func createPlugin(
	ctx context.Context,
	cl client.Client,
	sdk PluginSDK,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) error {
	controlPlaneID := pluginBinding.GetControlPlaneID()
	if controlPlaneID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", pluginBinding, client.ObjectKeyFromObject(pluginBinding))
	}
	pluginInput, err := getPluginInput(ctx, cl, pluginBinding)
	if err != nil {
		return err
	}

	resp, err := sdk.CreatePlugin(ctx,
		controlPlaneID,
		*pluginInput,
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, CreateOp, pluginBinding); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				pluginBinding.GetGeneration(),
			),
			pluginBinding,
		)
		return errWrapped
	}

	pluginBinding.SetKonnectID(*resp.Plugin.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			pluginBinding.GetGeneration(),
		),
		pluginBinding,
	)

	return nil
}

// updatePlugin updates the Konnect Plugin entity.
// It is assumed that provided KongPluginBinding has Konnect ID set in status.
// It returns an error if the KongPluginBinding does not have a ControlPlaneRef or
// if the operation fails.
func updatePlugin(
	ctx context.Context,
	sdk PluginSDK,
	cl client.Client,
	pb *configurationv1alpha1.KongPluginBinding,
) error {
	controlPlaneID := pb.GetControlPlaneID()
	if controlPlaneID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", pb, client.ObjectKeyFromObject(pb))
	}

	// TODO(pmalek) handle other types of CP ref
	// TODO(pmalek) handle cross namespace refs
	nnCP := types.NamespacedName{
		Namespace: pb.Namespace,
		Name:      pb.Spec.ControlPlaneRef.KonnectNamespacedRef.Name,
	}
	var cp konnectv1alpha1.KonnectGatewayControlPlane
	if err := cl.Get(ctx, nnCP, &cp); err != nil {
		return fmt.Errorf("failed to get KonnectGatewayControlPlane %s: for %T %s: %w",
			nnCP, pb, client.ObjectKeyFromObject(pb), err,
		)
	}

	if cp.Status.ID == "" {
		return fmt.Errorf(
			"can't update %T when referenced KonnectGatewayControlPlane %s does not have the Konnect ID",
			pb, nnCP,
		)
	}

	pluginInput, err := getPluginInput(ctx, cl, pb)
	if err != nil {
		return err
	}

	resp, err := sdk.UpsertPlugin(ctx,
		sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: controlPlaneID,
			PluginID:       pb.GetKonnectID(),
			Plugin:         *pluginInput,
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, UpdateOp, pb); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				pb.GetGeneration(),
			),
			pb,
		)
		return errWrapped
	}

	pb.Status.Konnect.SetKonnectID(*resp.Plugin.ID)
	pb.Status.Konnect.SetControlPlaneID(cp.Status.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			pb.GetGeneration(),
		),
		pb,
	)

	return nil
}

// deletePlugin deletes a plugin in Konnect.
// The KongPluginBinding is assumed to have a Konnect ID set in status.
// It returns an error if the operation fails.
func deletePlugin(
	ctx context.Context,
	sdk PluginSDK,
	pb *configurationv1alpha1.KongPluginBinding,
) error {
	id := pb.GetKonnectID()
	_, err := sdk.DeletePlugin(ctx, pb.GetControlPlaneID(), id)
	if errWrapped := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, DeleteOp, pb); errWrapped != nil {
		// plugin delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) && sdkError.StatusCode == 404 {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", pb.GetTypeName(), "id", id,
				)
			return nil
		}
		return FailedKonnectOpError[configurationv1alpha1.KongPluginBinding]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Konnect KongPlugin - ops helpers
// -----------------------------------------------------------------------------

// getPluginInput returns the SDK PluginInput for the KongPluginBinding.
func getPluginInput(ctx context.Context, cl client.Client, pluginBinding *configurationv1alpha1.KongPluginBinding) (*sdkkonnectcomp.PluginInput, error) {
	plugin, err := getReferencedPlugin(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	targets, err := getPluginBindingTargets(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	return kongPluginBindingToSDKPluginInput(plugin, targets)
}

// getPluginBindingTargets returns the list of client objects referenced
// by the kongPluginBInding.spec.targets field.
func getPluginBindingTargets(
	ctx context.Context,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) ([]client.Object, error) {
	targets := pluginBinding.Spec.Targets
	targetObjects := []client.Object{}
	if targets.ServiceReference != nil {
		if targets.ServiceReference.Kind != "KongService" {
			return nil, fmt.Errorf("unsupported service target kind %q", targets.ServiceReference.Kind)
		}

		kongService := configurationv1alpha1.KongService{}
		kongService.SetName(targets.ServiceReference.Name)
		kongService.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongService), &kongService); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongService)
	}

	// TODO(mlavacca): add support for KongRoute
	// TODO(mlavacca): add support for KongConsumer

	return targetObjects, nil
}

// getReferencedPlugin returns the KongPlugin referenced by the KongPluginBinding.spec.pluginRef field.
func getReferencedPlugin(ctx context.Context, cl client.Client, pluginBinding *configurationv1alpha1.KongPluginBinding) (*configurationv1.KongPlugin, error) {
	// TODO(mlavacca): add support for KongClusterPlugin
	plugin := configurationv1.KongPlugin{}
	plugin.SetName(pluginBinding.Spec.PluginReference.Name)
	plugin.SetNamespace(pluginBinding.GetNamespace())

	if err := cl.Get(ctx, client.ObjectKeyFromObject(&plugin), &plugin); err != nil {
		return nil, err
	}

	return &plugin, nil
}

// kongPluginBindingToSDKPluginInput converts a kongPluginConfiguration to an SKD PluginInput.
func kongPluginBindingToSDKPluginInput(
	plugin *configurationv1.KongPlugin,
	targets []client.Object,
) (*sdkkonnectcomp.PluginInput, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found for KongPluginBinding %s", client.ObjectKeyFromObject(plugin))
	}

	pluginConfig := map[string]any{}
	if err := json.Unmarshal(plugin.Config.Raw, &pluginConfig); err != nil {
		return nil, err
	}

	pluginInput := &sdkkonnectcomp.PluginInput{
		Name:    lo.ToPtr(plugin.PluginName),
		Config:  pluginConfig,
		Enabled: lo.ToPtr(!plugin.Disabled),
	}

	// TODO(mlavacca): check all the entities reference the same KonnectGatewayControlPlane

	for _, t := range targets {
		switch t := t.(type) {
		case *configurationv1alpha1.KongService:
			id := t.GetKonnectID()
			if id == "" {
				return nil, fmt.Errorf("KongService %s is not configured in Konnect yet", client.ObjectKeyFromObject(t))
			}
			pluginInput.Service = &sdkkonnectcomp.PluginService{
				ID: lo.ToPtr(t.GetKonnectStatus().ID),
			}
		// TODO(mlavacca): add support for KongRoute
		// TODO(mlavacca): add support for KongConsumer
		default:
			return nil, fmt.Errorf("unsupported target type %T", t)
		}
	}

	return pluginInput, nil
}
