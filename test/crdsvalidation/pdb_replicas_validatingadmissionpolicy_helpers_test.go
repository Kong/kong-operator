package crdsvalidation

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
)

// pdbReplicasWarning is part of every message emitted by the PDB replicas policies.
const pdbReplicasWarning = "is set but the replica floor is"

type pdbReplicasCase struct {
	name            string
	replicas        *int32
	hpa             bool
	minReplicas     *int32
	withPDB         bool
	expectedWarning string
}

var pdbReplicasCases = []pdbReplicasCase{
	{name: "no PDB with 1 replica", replicas: new(int32(1))},
	{name: "PDB with 2 replicas", replicas: new(int32(2)), withPDB: true},
	{name: "PDB with HPA minReplicas 2", hpa: true, minReplicas: new(int32(2)), withPDB: true},
	{name: "PDB with 1 replica", replicas: new(int32(1)), withPDB: true, expectedWarning: "replica floor is 1"},
	{name: "PDB with 0 replicas", replicas: new(int32(0)), withPDB: true, expectedWarning: "replica floor is 0"},
	{name: "PDB with HPA minReplicas 1", hpa: true, minReplicas: new(int32(1)), withPDB: true, expectedWarning: "replica floor is 1"},
	{name: "PDB with HPA without minReplicas", hpa: true, withPDB: true, expectedWarning: "replica floor is 1"},
	{name: "PDB without replica fields", withPDB: true, expectedWarning: "replica floor is 1"},
}

func (c pdbReplicasCase) horizontalScaling() *commonv1alpha1.HorizontalScaling {
	if !c.hpa {
		return nil
	}
	return &commonv1alpha1.HorizontalScaling{MinReplicas: c.minReplicas, MaxReplicas: 5}
}

func (c pdbReplicasCase) deploymentOptionsV1beta1() operatorv1beta1.DeploymentOptions {
	opts := operatorv1beta1.DeploymentOptions{Replicas: c.replicas}
	if hs := c.horizontalScaling(); hs != nil {
		opts.Scaling = &operatorv1beta1.Scaling{HorizontalScaling: hs}
	}
	return opts
}

//nolint:unused // used by the GatewayConfiguration policy test.
func (c pdbReplicasCase) deploymentOptionsV2beta1() operatorv2beta1.DeploymentOptions {
	opts := operatorv2beta1.DeploymentOptions{Replicas: c.replicas}
	if hs := c.horizontalScaling(); hs != nil {
		opts.Scaling = &operatorv2beta1.Scaling{HorizontalScaling: hs}
	}
	return opts
}

func pdbReplicasDataPlane(ns string, c pdbReplicasCase) *operatorv1beta1.DataPlane {
	deployment := c.deploymentOptionsV1beta1()
	deployment.PodTemplateSpec = &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "proxy", Image: "kong:3.9"}},
		},
	}
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: common.CommonObjectMeta(ns),
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{DeploymentOptions: deployment},
			},
		},
	}
	if c.withPDB {
		dp.Spec.Resources.PodDisruptionBudget = &operatorv1beta1.PodDisruptionBudget{
			Spec: operatorv1beta1.PodDisruptionBudgetSpec{MaxUnavailable: new(intstr.FromInt32(1))},
		}
	}
	return dp
}

// newWarningClient returns a client recording its own API server warnings,
// so that assertions don't see warnings caused by other test cases.
func newWarningClient(cfg *rest.Config, scheme *runtime.Scheme) (client.Client, *common.WarningCollector, error) {
	wc := &common.WarningCollector{}
	c := rest.CopyConfig(cfg)
	c.WarningHandler = wc
	cl, err := client.New(c, client.Options{Scheme: scheme})
	return cl, wc, err
}

func hasPDBReplicasWarning(warnings []string) bool {
	return slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, pdbReplicasWarning) })
}

// waitForPDBReplicasPolicy waits until the API server enforces the policy,
// either warning about or denying nonCompliant.
func waitForPDBReplicasPolicy(ctx context.Context, t *testing.T, cfg *rest.Config, scheme *runtime.Scheme, nonCompliant client.Object) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		cl, wc, err := newWarningClient(cfg, scheme)
		if !assert.NoError(c, err) {
			return
		}
		obj := nonCompliant.DeepCopyObject().(client.Object)
		err = cl.Create(ctx, obj)
		if err != nil {
			assert.Contains(c, err.Error(), pdbReplicasWarning)
			return
		}
		_ = cl.Delete(ctx, obj)
		assert.True(c, hasPDBReplicasWarning(wc.GetWarnings()), "warnings: %v", wc.GetWarnings())
	}, common.SharedEventuallyConfig.Timeout, common.SharedEventuallyConfig.Period)
}

func runPDBReplicasCases(ctx context.Context, t *testing.T, cfg *rest.Config, scheme *runtime.Scheme, build func(pdbReplicasCase) client.Object) {
	t.Helper()
	for _, tc := range pdbReplicasCases {
		t.Run(tc.name, func(t *testing.T) {
			cl, wc, err := newWarningClient(cfg, scheme)
			require.NoError(t, err)
			obj := build(tc)
			require.NoError(t, cl.Create(ctx, obj))
			t.Cleanup(func() { assert.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), obj))) })

			warnings := wc.GetWarnings()
			if tc.expectedWarning == "" {
				assert.False(t, hasPDBReplicasWarning(warnings), "warnings: %v", warnings)
				return
			}
			assert.True(t,
				slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, tc.expectedWarning) }),
				"expected warning %q, got: %v", tc.expectedWarning, warnings,
			)
		})
	}
}

// assertPDBReplicasRatchet checks that a metadata-only update of a non-compliant object is not validated.
func assertPDBReplicasRatchet(ctx context.Context, t *testing.T, cfg *rest.Config, scheme *runtime.Scheme, nonCompliant client.Object) {
	t.Helper()
	cl, wc, err := newWarningClient(cfg, scheme)
	require.NoError(t, err)
	obj := nonCompliant.DeepCopyObject().(client.Object)
	require.NoError(t, cl.Create(ctx, obj))
	t.Cleanup(func() { assert.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), obj))) })
	require.True(t, hasPDBReplicasWarning(wc.GetWarnings()), "warnings: %v", wc.GetWarnings())

	updateCl, updateWC, err := newWarningClient(cfg, scheme)
	require.NoError(t, err)
	obj.SetLabels(map[string]string{"ratchet": "true"})
	require.NoError(t, updateCl.Update(ctx, obj))
	assert.False(t, hasPDBReplicasWarning(updateWC.GetWarnings()), "warnings: %v", updateWC.GetWarnings())
}
