package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/pkg/metadata"
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

func adoptPluginBinding(
	ctx context.Context,
	sdk sdkops.PluginSDK,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) error {
	cpID := pluginBinding.GetControlPlaneID()
	adoptOptions := pluginBinding.Spec.Adopt
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}
	if adoptOptions == nil || adoptOptions.Konnect == nil {
		return errors.New("Konnect adopt options must be provided")
	}
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetPlugin(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.Plugin == nil {
		return fmt.Errorf("failed getting %s: %w", pluginBinding.GetTypeName(), ErrNilResponse)
	}

	if uidTag, hasUIDTag := findUIDTag(resp.Plugin.Tags); hasUIDTag && extractUIDFromTag(uidTag) != string(pluginBinding.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}

	switch adoptMode {
	case commonv1alpha1.AdoptModeOverride:
		// Update the remote plugin to match the spec.
		pluginBindingCopy := pluginBinding.DeepCopy()
		pluginBindingCopy.SetKonnectID(konnectID)
		if err := updatePlugin(ctx, sdk, cl, pluginBindingCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		matches, err := pluginBindingMatches(ctx, cl, pluginBinding, resp.Plugin)
		if err != nil {
			return err
		}
		if !matches {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	pluginBinding.SetKonnectID(konnectID)
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

func pluginBindingMatches(
	ctx context.Context,
	cl client.Client,
	binding *configurationv1alpha1.KongPluginBinding,
	konnectPlugin *sdkkonnectcomp.Plugin,
) (bool, error) {
	desired, err := kongPluginBindingToSDKPluginInput(ctx, cl, binding)
	if err != nil {
		return false, err
	}
	if desired == nil {
		return false, fmt.Errorf("failed to render desired plugin input for %s", client.ObjectKeyFromObject(binding))
	}

	if desired.Name != konnectPlugin.Name {
		return false, nil
	}
	if boolValueOrDefault(desired.Enabled, true) != boolValueOrDefault(konnectPlugin.Enabled, true) {
		return false, nil
	}
	if stringValueOrEmpty(desired.InstanceName) != stringValueOrEmpty(konnectPlugin.InstanceName) {
		return false, nil
	}
	if !protocolsEqual(desired.Protocols, konnectPlugin.Protocols) {
		return false, nil
	}
	if !configsEqual(desired.Config, konnectPlugin.Config) {
		return false, nil
	}
	if pluginServiceID(desired.Service) != pluginServiceID(konnectPlugin.Service) {
		return false, nil
	}
	if pluginRouteID(desired.Route) != pluginRouteID(konnectPlugin.Route) {
		return false, nil
	}
	if pluginConsumerID(desired.Consumer) != pluginConsumerID(konnectPlugin.Consumer) {
		return false, nil
	}
	if pluginConsumerGroupID(desired.ConsumerGroup) != pluginConsumerGroupID(konnectPlugin.ConsumerGroup) {
		return false, nil
	}

	return true, nil
}

func boolValueOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func stringValueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func protocolsEqual(expected, actual []sdkkonnectcomp.Protocols) bool {
	if len(expected) == 0 && len(actual) == 0 {
		return true
	}

	exp := make([]string, len(expected))
	for i, p := range expected {
		exp[i] = string(p)
	}
	act := make([]string, len(actual))
	for i, p := range actual {
		act[i] = string(p)
	}

	slices.Sort(exp)
	slices.Sort(act)

	return reflect.DeepEqual(exp, act)
}

func configsEqual(expected, actual map[string]any) bool {
	if len(expected) == 0 && len(actual) == 0 {
		return true
	}

	return reflect.DeepEqual(expected, actual)
}

func pluginServiceID(service *sdkkonnectcomp.PluginService) string {
	if service == nil || service.ID == nil {
		return ""
	}
	return *service.ID
}

func pluginRouteID(route *sdkkonnectcomp.PluginRoute) string {
	if route == nil || route.ID == nil {
		return ""
	}
	return *route.ID
}

func pluginConsumerID(consumer *sdkkonnectcomp.PluginConsumer) string {
	if consumer == nil || consumer.ID == nil {
		return ""
	}
	return *consumer.ID
}

func pluginConsumerGroupID(group *sdkkonnectcomp.PluginConsumerGroup) string {
	if group == nil || group.ID == nil {
		return ""
	}
	return *group.ID
}
