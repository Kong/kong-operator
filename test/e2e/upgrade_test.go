package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	ktypes "sigs.k8s.io/kustomize/api/types"

	"github.com/kong/kong-operator/v2/test"
)

type upgradeTestParams struct {
	// fromImage is the image to start the upgrade test with.
	fromImage string
	// toImage is the image to upgrade to in the upgrade test.
	toImage string
}

// TestDeployAndUpgradeFromLatestTagToOverride tests an upgrade path from latest
// released tag to an override version provided by environment variables.
// The reason we need and override is to not test against latest main build because
// that would not provide accurate test results because when one changes code in
// a PR, the intention is to test this very code through CI, not a prior version
// without this change.
//
// CI already has this override provided in
// https://github.com/kong/kong-operator/blob/0f3b726c33/.github/workflows/tests.yaml#L180-L190
// so anyone pushing changes can expect those changes to be tested in this test.
func TestDeployAndUpgradeFromLatestTagToOverride(t *testing.T) {
	t.Skip("Skip until https://github.com/Kong/kong-operator/issues/2203 is resolved")

	if imageLoad == "" && imageOverride == "" {
		t.Skipf("No KONG_TEST_KONG_OPERATOR_IMAGE_OVERRIDE nor KONG_TEST_KONG_OPERATOR_IMAGE_LOAD" +
			" env specified. Please specify the image to upgrade to in order to run this test.")
	}
	var image string
	if imageLoad != "" {
		t.Logf("KONG_TEST_KONG_OPERATOR_IMAGE_LOAD set to %q, using it to upgrade", imageLoad)
		image = imageLoad
	} else {
		t.Logf("KONG_TEST_KONG_OPERATOR_IMAGE_OVERRIDE set to %q, using it to upgrade", imageOverride)
		image = imageOverride
	}

	// Read the last tag that was released, that's present in manager's kustomization.yaml.
	var k ktypes.Kustomization
	kustomizeDir := PrepareKustomizeDir(t, image)
	kustomizeManagerKustomizationFilePath := kustomizeDir.ManagerKustomizationYAML()
	kbytes, err := os.ReadFile(kustomizeManagerKustomizationFilePath)
	require.NoError(t, err)
	require.NoError(t, k.Unmarshal(kbytes))

	// NOTE: We now use a component to set the image so it's not readily available from manager's manifest.
	var ktests ktypes.Kustomization
	kustomizeTests := kustomizeDir.TestsKustomization()
	kTestsBytes, err := os.ReadFile(kustomizeTests)
	require.NoError(t, err)
	require.NoError(t, ktests.Unmarshal(kTestsBytes))
	require.Len(t, ktests.Images, 1)
	fromImage := fmt.Sprintf("%s:%s", ktests.Images[0].NewName, ktests.Images[0].NewTag)

	t.Logf("got latest tag %q from %q", fromImage, kustomizeTests)
	testManifestsUpgrade(t, t.Context(), upgradeTestParams{
		fromImage: fromImage,
		toImage:   image,
	})
}

func testManifestsUpgrade(
	t *testing.T,
	ctx context.Context,
	testParams upgradeTestParams,
) {
	const (
		waitTime = 3 * time.Minute
	)

	e := CreateEnvironment(t, ctx, WithOperatorImage(testParams.fromImage), WithInstallViaKustomize())

	kustomizationDir := PrepareKustomizeDir(t, testParams.toImage)
	t.Logf("deploying operator %q to test cluster %q via kustomize", testParams.toImage, e.Environment.Name())
	require.NoError(t, clusters.KustomizeDeployForCluster(ctx, e.Environment.Cluster(), kustomizationDir.Tests(), "--server-side", "-v5"))
	t.Log("waiting for operator deployment to complete")
	require.NoError(t, waitForOperatorDeployment(t, ctx, "kong-system", e.Clients.K8sClient, waitTime,
		deploymentAssertConditions(t,
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

	if test.IsWebhookEnabled() {
		require.Eventually(t, waitForOperatorWebhookEventually(t, ctx, e.Clients.K8sClient),
			webhookReadinessTimeout, webhookReadinessTick)
	}
}
