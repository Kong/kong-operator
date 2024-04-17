package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/kong/gateway-operator/pkg/utils/test"
)

func init() {
	addTestsToTestSuite(TestUpgrade)
}

func TestUpgrade(t *testing.T) {
	const (
		// Rel: https://github.com/Kong/charts/tree/main/charts/gateway-operator
		chart = "kong/gateway-operator"
		image = "docker.io/kong/gateway-operator-oss"
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// createEnvironment will queue up environment cleanup if necessary
	// and dumping diagnostics if the test fails.
	e := CreateEnvironment(t, ctx)

	testcases := []struct {
		name             string
		fromVersion      string
		toVersion        string
		upgradeToCurrent bool
		assertions       func(*testing.T, *test.K8sClients)
	}{
		// NOTE: We do not support versions earlier than 1.2 with the helm chart.
		// The initial version of the chart contained CRDs from KGO 1.2. which
		// introduced a breaking change which makes it impossible to upgrade from
		// automatically (without manually deleting the CRDs).
		{
			name:        "upgrade from 1.2.0 to 1.2.3",
			fromVersion: "1.2.1",
			toVersion:   "1.2.3",
			assertions: func(t *testing.T, c *test.K8sClients) {
				// TODO
			},
		},
		{
			name:             "upgrade from 1.2.3 to current",
			fromVersion:      "1.2.3",
			upgradeToCurrent: true,
		},
	}

	var currentTag string
	if imageLoad != "" {
		t.Logf("KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD set to %q", imageLoad)
		currentTag = vFromImage(t, imageLoad)
	} else if imageOverride != "" {
		t.Logf("KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE set to %q", imageOverride)
		currentTag = vFromImage(t, imageOverride)
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			var tag string
			if tc.upgradeToCurrent {
				if currentTag == "" {
					t.Skipf("No KONG_TEST_GATEWAY_OPERATOR_IMAGE_OVERRIDE nor KONG_TEST_GATEWAY_OPERATOR_IMAGE_LOAD" +
						" env specified. Please specify the image to upgrade to in order to run this test.")
				}
				tag = currentTag
			} else {
				tag = tc.toVersion
			}

			tagInReleaseName := tag
			if len(tag) > 8 {
				tagInReleaseName = tag[:8]
			}
			releaseName := strings.ReplaceAll(fmt.Sprintf("kgo-%s-to-%s", tc.fromVersion, tagInReleaseName), ".", "-")
			values := map[string]string{
				"image.tag":        tc.fromVersion,
				"image.repository": image,
			}

			opts := &helm.Options{
				KubectlOptions: &k8s.KubectlOptions{
					Namespace:  e.Namespace.Name,
					RestConfig: e.Environment.Cluster().Config(),
				},
				SetValues: values,
			}

			require.NoError(t, helm.AddRepoE(t, opts, "kong", "https://charts.konghq.com"))
			require.NoError(t, helm.InstallE(t, opts, chart, releaseName))
			t.Cleanup(func() {
				out, err := helm.RunHelmCommandAndGetOutputE(t, opts, "uninstall", releaseName)
				if !assert.NoError(t, err) {
					t.Logf("output: %s", out)
				}
			})

			require.NoError(t, waitForOperatorDeployment(ctx, e.Namespace.Name, e.Clients.K8sClient,
				deploymentAssertConditions(deploymentReadyConditions()...),
			))

			opts.SetValues["image.tag"] = tag

			require.NoError(t, helm.UpgradeE(t, opts, chart, releaseName))
			require.NoError(t, waitForOperatorDeployment(ctx, e.Namespace.Name, e.Clients.K8sClient,
				deploymentAssertConditions(deploymentReadyConditions()...),
			))
		})
	}
}

func deploymentReadyConditions() []appsv1.DeploymentCondition {
	return []appsv1.DeploymentCondition{
		{
			Reason: "NewReplicaSetAvailable",
			Status: "True",
			Type:   "Progressing",
		},
		{
			Reason: "MinimumReplicasAvailable",
			Status: "True",
			Type:   "Available",
		},
	}
}

func vFromImage(t *testing.T, image string) string {
	splitImage := strings.Split(image, ":")
	if len(splitImage) != 2 {
		t.Fatalf("image %q does not contain a tag", image)
	}
	return splitImage[1]
}
