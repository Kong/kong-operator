package mcpserver

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// builtinPlugin describes a Kong plugin that is automatically provisioned for
// every MCPServer deployment.
type builtinPlugin struct {
	name   string
	config string
}

var builtinPlugins = []builtinPlugin{
	{
		name:   "ai-mcp-proxy",
		config: `{"mode":"passthrough-listener"}`,
	},
}

// kongPluginName returns the deterministic name for the KongPlugin CR
// corresponding to the given builtin plugin and MCPServer.
func kongPluginName(mcpDataPlane *mcpv1alpha1.MCPServerDataPlane, plg builtinPlugin) string {
	return fmt.Sprintf("%s-%s", generateWorkloadNN(mcpDataPlane).Name, plg.name)
}

// ensureKongPlugins creates KongPlugin CRs for every builtinPlugin and deletes
// any stale KongPlugins owned by the MCPServer that are no longer expected.
// It returns the set of plugin names that were ensured.
func (r *MCPServerDataPlaneReconciler) ensureKongPlugins(
	ctx context.Context,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
) (map[string]struct{}, error) {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	desiredPluginNames := make(map[string]struct{}, len(builtinPlugins))
	for _, plg := range builtinPlugins {
		res, nn, err := r.ensureKongPlugin(ctx, mcpDataPlane, plg)
		if err != nil {
			return nil, err
		}
		if res != op.Noop {
			log.Info(logger, fmt.Sprintf("%s KongPlugin for MCPServer", res),
				"namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name, "plugin", nn.Name)
		}
		desiredPluginNames[nn.Name] = struct{}{}
	}

	if err := r.deleteStaleResources(ctx, mcpDataPlane, &configurationv1.KongPluginList{}, desiredPluginNames); err != nil {
		return nil, err
	}

	return desiredPluginNames, nil
}

func (r *MCPServerDataPlaneReconciler) ensureKongPlugin(
	ctx context.Context,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	plg builtinPlugin,
) (op.Result, client.ObjectKey, error) {
	desired := generateKongPlugin(mcpDataPlane, plg)
	nn := client.ObjectKeyFromObject(desired)

	k8sutils.SetOwnerForObject(desired, mcpDataPlane)
	k8sresources.LabelObjectAsMCPServerManaged(desired)

	existing := &configurationv1.KongPlugin{}
	err := r.Get(ctx, nn, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nn, fmt.Errorf("failed to get KongPlugin %s: %w", nn, err)
		}

		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nn, fmt.Errorf("failed to create KongPlugin %s: %w", nn, err)
		}
		return op.Created, nn, nil
	}

	// TODO: enforce the KongPlugin Spec https://github.com/Kong/kong-operator/issues/3979

	return op.Noop, nn, nil
}

func generateKongPlugin(mcpDataPlane *mcpv1alpha1.MCPServerDataPlane, plg builtinPlugin) *configurationv1.KongPlugin {
	return &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kongPluginName(mcpDataPlane, plg),
			Namespace: mcpDataPlane.Namespace,
		},
		PluginName: plg.name,
		Config: apiextensionsv1.JSON{
			Raw: []byte(plg.config),
		},
	}
}

// ensureKongPluginBindings creates a KongPluginBinding CR for every
// (KongService, KongPlugin) pair, binding the plugin to the service. Stale
// bindings owned by the MCPServer are deleted.
func (r *MCPServerDataPlaneReconciler) ensureKongPluginBindings(
	ctx context.Context,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	serviceNames map[string]struct{},
	cpRef commonv1alpha1.ControlPlaneRef,
) error {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	desiredBindingNames := make(map[string]struct{}, len(builtinPlugins)*len(serviceNames))
	for _, plg := range builtinPlugins {
		pluginName := kongPluginName(mcpDataPlane, plg)
		for svcName := range serviceNames {
			res, nn, err := r.ensureKongPluginBinding(ctx, mcpDataPlane, pluginName, svcName, cpRef)
			if err != nil {
				return err
			}
			if res != op.Noop {
				log.Info(logger, fmt.Sprintf("%s KongPluginBinding for MCPServer", res),
					"namespace", mcpDataPlane.Namespace, "name", mcpDataPlane.Name,
					"binding", nn.Name, "plugin", pluginName, "service", svcName)
			}
			desiredBindingNames[nn.Name] = struct{}{}
		}
	}

	return r.deleteStaleResources(ctx, mcpDataPlane, &configurationv1alpha1.KongPluginBindingList{}, desiredBindingNames)
}

func (r *MCPServerDataPlaneReconciler) ensureKongPluginBinding(
	ctx context.Context,
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	pluginName, serviceName string,
	cpRef commonv1alpha1.ControlPlaneRef,
) (op.Result, client.ObjectKey, error) {
	desired := generateKongPluginBinding(mcpDataPlane, pluginName, serviceName, cpRef)
	nn := client.ObjectKeyFromObject(desired)

	k8sutils.SetOwnerForObject(desired, mcpDataPlane)
	k8sresources.LabelObjectAsMCPServerManaged(desired)

	existing := &configurationv1alpha1.KongPluginBinding{}
	err := r.Get(ctx, nn, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nn, fmt.Errorf("failed to get KongPluginBinding %s: %w", nn, err)
		}

		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nn, fmt.Errorf("failed to create KongPluginBinding %s: %w", nn, err)
		}
		return op.Created, nn, nil
	}

	// TODO: enforce the KongPluginBinding Spec https://github.com/Kong/kong-operator/issues/3979

	return op.Noop, nn, nil
}

func generateKongPluginBinding(
	mcpDataPlane *mcpv1alpha1.MCPServerDataPlane,
	pluginName, serviceName string,
	cpRef commonv1alpha1.ControlPlaneRef,
) *configurationv1alpha1.KongPluginBinding {
	return &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pluginName,
			Namespace: mcpDataPlane.Namespace,
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: pluginName,
			},
			Targets: &configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name:  serviceName,
					Group: configurationv1.GroupVersion.Group,
					Kind:  "KongService",
				},
			},
			ControlPlaneRef: cpRef,
		},
	}
}
