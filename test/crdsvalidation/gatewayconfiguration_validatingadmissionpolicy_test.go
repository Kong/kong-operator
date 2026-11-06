package crdsvalidation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/helm"
	"github.com/kong/kong-operator/v2/test/helpers/kcfg"
)

func pdbReplicasGatewayConfigurationV1beta1(ns string, c pdbReplicasCase) *operatorv1beta1.GatewayConfiguration { //nolint:forbidigo // v1beta1 is still served and matched by the policy.
	gc := &operatorv1beta1.GatewayConfiguration{ //nolint:forbidigo // see above.
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: operatorv1beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &operatorv1beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{DeploymentOptions: c.deploymentOptionsV1beta1()},
			},
		},
	}
	if c.withPDB {
		gc.Spec.DataPlaneOptions.Resources = &operatorv1beta1.GatewayConfigDataPlaneResources{
			PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
				Spec: operatorv1beta1.PodDisruptionBudgetSpec{MaxUnavailable: new(intstr.FromInt32(1))},
			},
		}
	}
	return gc
}

func pdbReplicasGatewayConfigurationV2beta1(ns string, c pdbReplicasCase) *operatorv2beta1.GatewayConfiguration {
	gc := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: operatorv2beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{DeploymentOptions: c.deploymentOptionsV2beta1()},
			},
		},
	}
	if c.withPDB {
		gc.Spec.DataPlaneOptions.Resources = &operatorv2beta1.GatewayConfigDataPlaneResources{
			PodDisruptionBudget: &operatorv2beta1.PodDisruptionBudget{
				Spec: operatorv2beta1.PodDisruptionBudgetSpec{MaxUnavailable: new(intstr.FromInt32(1))},
			},
		}
	}
	return gc
}

func TestGatewayConfigurationPDBReplicasValidatingAdmissionPolicy(t *testing.T) {
	t.Parallel()

	var (
		ctx     = t.Context()
		scheme  = scheme.Get()
		cfg, ns = envtest.Setup(t, ctx, scheme)
	)

	helm.ApplyTemplate(ctx, t, cfg, kcfg.ChartPath(), []string{ValidationPolicyGatewayConfiguration})

	nonCompliant := pdbReplicasCase{replicas: new(int32(1)), withPDB: true}
	waitForPDBReplicasPolicy(ctx, t, cfg, scheme, pdbReplicasGatewayConfigurationV2beta1(ns.Name, nonCompliant))

	t.Run("v1beta1", func(t *testing.T) {
		runPDBReplicasCases(ctx, t, cfg, scheme, func(c pdbReplicasCase) client.Object {
			return pdbReplicasGatewayConfigurationV1beta1(ns.Name, c)
		})
		t.Run("metadata-only update is not validated", func(t *testing.T) {
			assertPDBReplicasRatchet(ctx, t, cfg, scheme, pdbReplicasGatewayConfigurationV1beta1(ns.Name, nonCompliant))
		})
	})

	t.Run("v2beta1", func(t *testing.T) {
		runPDBReplicasCases(ctx, t, cfg, scheme, func(c pdbReplicasCase) client.Object {
			return pdbReplicasGatewayConfigurationV2beta1(ns.Name, c)
		})
		t.Run("metadata-only update is not validated", func(t *testing.T) {
			assertPDBReplicasRatchet(ctx, t, cfg, scheme, pdbReplicasGatewayConfigurationV2beta1(ns.Name, nonCompliant))
		})
	})

	t.Run("no dataPlaneOptions", func(t *testing.T) {
		cl, wc, err := newWarningClient(cfg, scheme)
		require.NoError(t, err)
		gc := &operatorv2beta1.GatewayConfiguration{ObjectMeta: common.CommonObjectMeta(ns.Name)}
		require.NoError(t, cl.Create(ctx, gc))
		assert.False(t, hasPDBReplicasWarning(wc.GetWarnings()), "warnings: %v", wc.GetWarnings())
	})
}
