package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

// -----------------------------------------------------------------------------
// Konnect KongPlugin - ops functions
// -----------------------------------------------------------------------------

// createPlugin creates the Konnect Plugin entity.
func createPlugin(
	ctx context.Context,
	cl client.Client,
	sdk PluginSDK,
	pb *configurationv1alpha1.KongPluginBinding,
) error {
	controlPlaneID := pb.GetControlPlaneID()
	if controlPlaneID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", pb, client.ObjectKeyFromObject(pb))
	}
	pluginInput, err := kongPluginBindingToSDKPluginInput(ctx, cl, pb)
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
	if errWrap := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, CreateOp, pb); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(pb, "FailedToCreate", errWrap.Error())
		return errWrap
	}

	pb.SetKonnectID(*resp.Plugin.ID)
	SetKonnectEntityProgrammedCondition(pb)

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

	pluginInput, err := kongPluginBindingToSDKPluginInput(ctx, cl, pb)
	if err != nil {
		return err
	}

	_, err = sdk.UpsertPlugin(ctx,
		sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: controlPlaneID,
			PluginID:       pb.GetKonnectID(),
			Plugin:         *pluginInput,
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, UpdateOp, pb); errWrap != nil {
		SetKonnectEntityProgrammedConditionFalse(pb, "FailedToUpdate", errWrap.Error())
		return errWrap
	}
	SetKonnectEntityProgrammedCondition(pb)

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
	if errWrap := wrapErrIfKonnectOpFailed[configurationv1alpha1.KongPluginBinding](err, DeleteOp, pb); errWrap != nil {
		// plugin delete operation returns an SDKError instead of a NotFoundError.
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrap, &sdkError) && sdkError.StatusCode == 404 {
			ctrllog.FromContext(ctx).
				Info("entity not found in Konnect, skipping delete",
					"op", DeleteOp, "type", pb.GetTypeName(), "id", id,
				)
			return nil
		}
		return FailedKonnectOpError[configurationv1alpha1.KongPluginBinding]{
			Op:  DeleteOp,
			Err: errWrap,
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Konnect KongPlugin - ops helpers
// -----------------------------------------------------------------------------

// kongPluginBindingToSDKPluginInput returns the SDK PluginInput for the KongPluginBinding.
// It uses the client.Client to fetch the KongPlugin and the targets referenced by the KongPluginBinding that are needed
// to create the SDK PluginInput.
func kongPluginBindingToSDKPluginInput(
	ctx context.Context,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) (*sdkkonnectcomp.PluginInput, error) {
	plugin, err := getReferencedPlugin(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	targets, err := getPluginBindingTargets(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	var (
		pluginBindingAnnotationTags = metadata.ExtractTags(pluginBinding)
		pluginAnnotationTags        = metadata.ExtractTags(plugin)
		pluginBindingK8sTags        = GenerateKubernetesMetadataTags(pluginBinding)
	)
	// Deduplicate tags to avoid rejection by Konnect.
	tags := lo.Uniq(slices.Concat(pluginBindingAnnotationTags, pluginAnnotationTags, pluginBindingK8sTags))

	return kongPluginWithTargetsToKongPluginInput(plugin, targets, tags)
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
	if targets.RouteReference != nil {
		if targets.RouteReference.Kind != "KongRoute" {
			return nil, fmt.Errorf("unsupported route target kind %q", targets.RouteReference.Kind)
		}

		kongRoute := configurationv1alpha1.KongRoute{}
		kongRoute.SetName(targets.RouteReference.Name)
		kongRoute.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongRoute), &kongRoute); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongRoute)
	}

	// TODO: https://github.com/Kong/gateway-operator/issues/526 add support for KongConsumer
	// TODO: https://github.com/Kong/gateway-operator/issues/527 add support for KongConsumerGroup

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

// kongPluginWithTargetsToKongPluginInput converts a KongPlugin configuration along with KongPluginBinding's targets and
// tags to an SKD PluginInput.
func kongPluginWithTargetsToKongPluginInput(
	plugin *configurationv1.KongPlugin,
	targets []client.Object,
	tags []string,
) (*sdkkonnectcomp.PluginInput, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no targets found for KongPluginBinding %s", client.ObjectKeyFromObject(plugin))
	}

	pluginConfig := map[string]any{}
	if rawConfig := plugin.Config.Raw; rawConfig != nil {
		// If the config is empty (a valid case), there's no need to unmarshal (as it would fail).
		if err := json.Unmarshal(rawConfig, &pluginConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal KongPlugin %s config: %w", client.ObjectKeyFromObject(plugin), err)
		}
	}

	pluginInput := &sdkkonnectcomp.PluginInput{
		Name:    plugin.PluginName,
		Config:  pluginConfig,
		Enabled: lo.ToPtr(!plugin.Disabled),
		Tags:    tags,
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
				ID: lo.ToPtr(id),
			}
		case *configurationv1alpha1.KongRoute:
			id := t.GetKonnectID()
			if id == "" {
				return nil, fmt.Errorf("KongRoute %s is not configured in Konnect yet", client.ObjectKeyFromObject(t))
			}
			pluginInput.Route = &sdkkonnectcomp.PluginRoute{
				ID: lo.ToPtr(id),
			}
		// TODO: https://github.com/Kong/gateway-operator/issues/526 add support for KongConsumer
		// TODO: https://github.com/Kong/gateway-operator/issues/527 add support for KongConsumerGroup
		default:
			return nil, fmt.Errorf("unsupported target type %T", t)
		}
	}

	return pluginInput, nil
}
