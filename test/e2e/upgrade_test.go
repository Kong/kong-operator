//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

type upgradeTestParams struct {
	// fromImage is the image to start the upgrade test with.
	fromImage string
	// toImage is the image to upgrade to in the upgrade test.
	toImage string
}

const (
	kustomizeManagerKustomizationPath     = "../../config/manager"
	kustomizeManagerKustomizationFilePath = kustomizeManagerKustomizationPath + string(filepath.Separator) + "kustomization.yaml"
)

// TestDeployAndUpgradeFromLatestTagToOverride tests an upgrade path from latest
// released tag to an override version provided by environment variables.
// The reason we need and override is to not test against latest main build because
// that would not provide accurate test results because when one changes code in
// a PR, the intention is to test this very code through CI, not a prior version
// without this change.
//
// CI already has this override provided in
// https://github.com/Kong/gateway-operator/blob/0f3b726c33/.github/workflows/tests.yaml#L180-L190
// so anyone pushing changes can expect those changes to be tested in this test.
func TestDeployAndUpgradeFromLatestTagToOverride(t *testing.T) {
	if imageLoad == "" && imageOverride == "" {
		t.Skipf("No KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE nor KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD" +
			" env specified. Please specify the image to upgrade to in order to run this test.")
	}
	var image string
	if imageLoad != "" {
		image = imageLoad
	} else {
		image = imageOverride
	}

	// Read the last tag that was released, that's present in manager's kustomization.yaml.
	var k ktypes.Kustomization
	kbytes, err := os.ReadFile(kustomizeManagerKustomizationFilePath)
	require.NoError(t, err)
	require.NoError(t, k.Unmarshal(kbytes))
	require.Len(t, k.Images, 1)
	fromImage := fmt.Sprintf("%s:%s", k.Images[0].NewName, k.Images[0].NewTag)

	t.Logf("got latest tag %q from %q", fromImage, kustomizeManagerKustomizationFilePath)
	testManifestsUpgrade(t, context.Background(), upgradeTestParams{
		fromImage: fromImage,
		toImage:   image,
	})
}

func testManifestsUpgrade(
	t *testing.T,
	ctx context.Context,
	testParams upgradeTestParams,
) {
	e := createEnvironment(t, ctx, WithOperatorImage(testParams.fromImage))

	kustomizationDir := prepareKustomizeDir(t, testParams.toImage)
	t.Logf("deploying operator %q to test cluster %q via kustomize", testParams.toImage, e.Environment.Name())
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, e.Environment.Cluster(), kustomizationDir))
	t.Log("waiting for operator deployment to complete")
	require.NoError(t, waitForOperatorDeployment(ctx, e.Clients.K8sClient,
		DeploymentAssertConditions(
			appsv1.DeploymentCondition{
				Reason: "NewReplicaSetAvailable",
				Status: "True",
				Type:   "Progressing",
			},
			appsv1.DeploymentCondition{
				Reason: "MinimumReplicasAvailable",
				Status: "True",
				Type:   "Available",
			},
		),
	))
	t.Log("waiting for operator webhook service to be connective")
	require.Eventually(t, waitForOperatorWebhookEventually(t, ctx, e.Clients.K8sClient),
		webhookReadinessTimeout, webhookReadinessTick)
}
