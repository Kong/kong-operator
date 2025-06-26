package ops

import (
	"context"
	"encoding/json"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

// -----------------------------------------------------------------------------
// Konnect KongPlugin - ops functions
// -----------------------------------------------------------------------------

// createPlugin creates the Konnect Plugin entity.
func createPlugin(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.PluginSDK,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) error {
	controlPlaneID := pluginBinding.GetControlPlaneID()
	if controlPlaneID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: pluginBinding, Op: CreateOp}
	}
	pluginInput, err := kongPluginBindingToSDKPluginInput(ctx, cl, pluginBinding)
	if err != nil {
		return err
	}

	resp, err := sdk.CreatePlugin(ctx,
		controlPlaneID,
		*pluginInput,
	)

	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, pluginBinding); errWrapped != nil {
		return errWrapped
	}

	if resp == nil || resp.Plugin == nil || resp.Plugin.ID == nil {
		return fmt.Errorf("failed creating %s: %w", pluginBinding.GetTypeName(), ErrNilResponse)
	}

	pluginBinding.SetKonnectID(*resp.Plugin.ID)

	return nil
}

// updatePlugin updates the Konnect Plugin entity.
// It is assumed that provided KongPluginBinding has Konnect ID set in status.
// It returns an error if the KongPluginBinding does not have a ControlPlaneRef or
// if the operation fails.
func updatePlugin(
	ctx context.Context,
	sdk sdkops.PluginSDK,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) error {
	controlPlaneID := pluginBinding.GetControlPlaneID()
	if controlPlaneID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: pluginBinding, Op: UpdateOp}
	}

	pluginInput, err := kongPluginBindingToSDKPluginInput(ctx, cl, pluginBinding)
	if err != nil {
		return err
	}

	id := pluginBinding.GetKonnectID()
	_, err = sdk.UpsertPlugin(ctx,
		sdkkonnectops.UpsertPluginRequest{
			ControlPlaneID: controlPlaneID,
			PluginID:       id,
			Plugin:         *pluginInput,
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, pluginBinding); errWrap != nil {
		return errWrap
	}

	return nil
}

// deletePlugin deletes a plugin in Konnect.
// The KongPluginBinding is assumed to have a Konnect ID set in status.
// It returns an error if the operation fails.
func deletePlugin(
	ctx context.Context,
	sdk sdkops.PluginSDK,
	pb *configurationv1alpha1.KongPluginBinding,
) error {
	id := pb.GetKonnectID()
	_, err := sdk.DeletePlugin(ctx, pb.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, pb); errWrap != nil {
		return handleDeleteError(ctx, err, pb)
	}

	return nil
}

// getPluginForUID lists plugins in Konnect with given k8s uid as its tag.
func getPluginForUID(
	ctx context.Context,
	sdk sdkops.PluginSDK,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) (string, error) {
	cpID := pluginBinding.GetControlPlaneID()
	reqList := sdkkonnectops.ListPluginRequest{
		// NOTE: only filter on object's UID.
		// Other fields like name might have changed in the meantime but that's OK.
		// Those will be enforced via subsequent updates.
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(UIDLabelForObject(pluginBinding)),
	}
	resp, err := sdk.ListPlugin(ctx, reqList)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", pluginBinding.GetTypeName(), err)
	}
	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", pluginBinding.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), pluginBinding)
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
) (*sdkkonnectcomp.Plugin, error) {
	plugin, err := getReferencedPlugin(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	targets, err := getPluginBindingTargets(ctx, cl, pluginBinding)
	if err != nil {
		return nil, err
	}

	tags := GenerateTagsForObject(pluginBinding, metadata.ExtractTags(plugin)...)
	return kongPluginWithTargetsToKongPluginInput(pluginBinding, plugin, targets, tags)
}

// getPluginBindingTargets returns the list of client objects referenced
// by the kongPluginBInding.spec.targets field.
func getPluginBindingTargets(
	ctx context.Context,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) ([]pluginTarget, error) {
	targets := pluginBinding.Spec.Targets
	if targets == nil {
		return nil, nil
	}

	targetObjects := []pluginTarget{}
	if ref := targets.ServiceReference; ref != nil {
		ref := targets.ServiceReference
		if ref.Kind != "KongService" {
			return nil, fmt.Errorf("unsupported service target kind %q", ref.Kind)
		}

		kongService := configurationv1alpha1.KongService{}
		kongService.SetName(ref.Name)
		kongService.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongService), &kongService); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongService)
	}
	if ref := targets.RouteReference; ref != nil {
		if ref.Kind != "KongRoute" {
			return nil, fmt.Errorf("unsupported route target kind %q", ref.Kind)
		}

		kongRoute := configurationv1alpha1.KongRoute{}
		kongRoute.SetName(ref.Name)
		kongRoute.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongRoute), &kongRoute); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongRoute)
	}
	if ref := targets.ConsumerReference; ref != nil {
		kongConsumer := configurationv1.KongConsumer{}
		kongConsumer.SetName(ref.Name)
		kongConsumer.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongConsumer), &kongConsumer); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongConsumer)
	}
	if ref := targets.ConsumerGroupReference; ref != nil {
		kongConsumerGroup := configurationv1beta1.KongConsumerGroup{}
		kongConsumerGroup.SetName(ref.Name)
		kongConsumerGroup.SetNamespace(pluginBinding.GetNamespace())
		if err := cl.Get(ctx, client.ObjectKeyFromObject(&kongConsumerGroup), &kongConsumerGroup); err != nil {
			return nil, err
		}
		targetObjects = append(targetObjects, &kongConsumerGroup)
	}

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

type pluginTarget interface {
	client.Object
	GetKonnectID() string
	GetTypeName() string
}

// kongPluginWithTargetsToKongPluginInput converts a KongPlugin configuration along with KongPluginBinding's targets and
// tags to an SKD PluginInput.
func kongPluginWithTargetsToKongPluginInput(binding *configurationv1alpha1.KongPluginBinding, plugin *configurationv1.KongPlugin, targets []pluginTarget, tags []string) (*sdkkonnectcomp.Plugin, error) {
	if binding.Spec.Scope == configurationv1alpha1.KongPluginBindingScopeOnlyTargets && len(targets) == 0 {
		return nil, fmt.Errorf("no targets found for KongPluginBinding %s", client.ObjectKeyFromObject(plugin))
	}

	pluginConfig := map[string]any{}
	if rawConfig := plugin.Config.Raw; rawConfig != nil {
		// If the config is empty (a valid case), there's no need to unmarshal (as it would fail).
		if err := json.Unmarshal(rawConfig, &pluginConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal KongPlugin %s config: %w", client.ObjectKeyFromObject(plugin), err)
		}
	}

	pluginInput := &sdkkonnectcomp.Plugin{
		Name:    plugin.PluginName,
		Config:  pluginConfig,
		Enabled: lo.ToPtr(!plugin.Disabled),
		Tags:    tags,
	}
	if plugin.InstanceName != "" {
		pluginInput.InstanceName = lo.ToPtr(plugin.InstanceName)
	}
	if len(plugin.Protocols) > 0 {
		pluginInput.Protocols = lo.Map(
			plugin.Protocols,
			func(p configurationv1.KongProtocol, _ int) sdkkonnectcomp.Protocols {
				return sdkkonnectcomp.Protocols(p)
			},
		)
	}
	// TODO: add support for ordering https://github.com/kong/kong-operator/issues/1682

	// TODO(mlavacca): check all the entities reference the same KonnectGatewayControlPlane

	for _, t := range targets {
		id := t.GetKonnectID()
		if id == "" {
			return nil, fmt.Errorf("%s %s is not configured in Konnect yet", constraints.EntityTypeNameForObj(t), client.ObjectKeyFromObject(t))
		}

		switch t := t.(type) {
		case *configurationv1alpha1.KongService:
			pluginInput.Service = &sdkkonnectcomp.PluginService{
				ID: lo.ToPtr(id),
			}
		case *configurationv1alpha1.KongRoute:
			pluginInput.Route = &sdkkonnectcomp.PluginRoute{
				ID: lo.ToPtr(id),
			}
		case *configurationv1.KongConsumer:
			pluginInput.Consumer = &sdkkonnectcomp.PluginConsumer{
				ID: lo.ToPtr(id),
			}
		case *configurationv1beta1.KongConsumerGroup:
			pluginInput.ConsumerGroup = &sdkkonnectcomp.PluginConsumerGroup{
				ID: lo.ToPtr(id),
			}
		default:
			return nil, fmt.Errorf("unsupported target type %T", t)
		}
	}

	return pluginInput, nil
}
