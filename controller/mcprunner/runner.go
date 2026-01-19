package mcprunner

import (
	"context"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/pkg/mcprunner"
)

func ensureMCPRunners(ctx context.Context, cl client.Client, runners []mcprunner.Runner) {
	logger := ctrllog.FromContext(ctx).WithName(ControllerName).WithName("ensure")

	for _, runner := range runners {
		mcpRunner := &configurationv1alpha1.KongMCPRunner{}
		mcpRunner.Name = runner.Name
		mcpRunner.Namespace = "default"
		mcpRunner.Spec = configurationv1alpha1.KongMCPRunnerSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name: "default-konnect-control-plane",
				},
			},
			Source: lo.ToPtr(commonv1alpha1.EntitySourceMirror),
			Mirror: &configurationv1alpha1.MirrorSpec{
				Konnect: configurationv1alpha1.MirrorKonnect{
					ID: commonv1alpha1.KonnectIDType(runner.ID),
				},
			},
		}

		// Create or update the KongMCPRunner
		if err := cl.Create(ctx, mcpRunner); err != nil {
			if client.IgnoreAlreadyExists(err) != nil {
				logger.Error(err, "Failed to create KongMCPRunner", "name", mcpRunner.Name)
				continue
			}
			logger.Info("KongMCPRunner already exists", "name", mcpRunner.Name)
		} else {
			logger.Info("Created KongMCPRunner", "name", mcpRunner.Name, "namespace", mcpRunner.Namespace)
		}
	}
}
