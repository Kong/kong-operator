package e2e

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

const (
	// parallelGateways is the total number of gateways that are created and deleted one after the other
	parallelGateways = 3
	// concurrentGatewaysReadyTimeLimit is the maximum amount of time to wait for a
	// supported Gateway to be fully provisioned and marked as Ready by the
	// gateway controller. This applies in testing environment with many concurrent gateways to be reconciled
	concurrentGatewaysReadyTimeLimit = time.Minute * 3
)

// structuredLogLine is the struct to be used for unmarshaling log lines
type structuredLogLine struct {
	Level  string  `json:"level"`
	TS     float64 `json:"ts"`
	Logger string  `json:"logger"`
	Msg    string  `json:"msg"`
	Error  string  `json:"error"`
}

var (
	// allowedErrorMsgs is the list of error messages that can happen without making the test fail
	// these log lines have the failure reason in the Msg field of the log
	allowedErrorMsgs = map[string]struct{}{
		"failed setting up anonymous reports": {},
	}
	// allowedErrorMsgs is the list of the reconciler errors that can happen without making the test fail
	// these log lines have the failure reason in the Error field of the log
	allowedReconcilerErrors = map[string]struct{}{
		"number of deployments reduced":         {},
		"number of serviceAccounts reduced":     {},
		"number of clusterRoles reduced":        {},
		"number of clusterRoleBindings reduced": {},
		"number of services reduced":            {},
		"number of secrets reduced":             {},
		"number of networkPolicies reduced":     {},
	}
)

func TestOperatorLogs(t *testing.T) {
	t.Skip("Skip until https://github.com/Kong/kong-operator/issues/2208 is resolved")

	ctx := t.Context()
	if imageLoad == "" && imageOverride == "" {
		t.Skipf("No KONG_TEST_KONG_OPERATOR_IMAGE_OVERRIDE nor KONG_TEST_KONG_OPERATOR_IMAGE_LOAD" +
			" env specified. Please specify the image to use in order to run this test.")
	}
	var image string
	if imageLoad != "" {
		t.Logf("KONG_TEST_KONG_OPERATOR_IMAGE_LOAD set to %q, using it for this test", imageLoad)
		image = imageLoad
	} else {
		t.Logf("KONG_TEST_KONG_OPERATOR_IMAGE_OVERRIDE set to %q, using it for this test", imageOverride)
		image = imageOverride
	}

	// createEnvironment will queue up environment cleanup if necessary
	// and dump diagnostics if the test fails.
	e := CreateEnvironment(t, ctx, WithOperatorImage(image), WithInstallViaKustomize())
	clients, testNamespace, cleaner := e.Clients, e.Namespace, e.Cleaner

	t.Log("finding the Pod for the Kong Operator")
	podList := &corev1.PodList{}
	err := clients.MgrClient.List(ctx, podList, client.MatchingLabels{
		"control-plane": "controller-manager",
	})
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.Len(c, podList.Items, 1)
	}, time.Minute, time.Second)
	operatorPod := podList.Items[0]

	t.Log("opening a log stream with the Kong Operator pod")
	readCloser, err := clients.K8sClient.CoreV1().Pods(operatorPod.Namespace).GetLogs(operatorPod.Name, &corev1.PodLogOptions{
		Container:                    "manager",
		Follow:                       true,
		InsecureSkipTLSVerifyBackend: true,
	}).Stream(ctx)
	require.NoError(t, err)

	wg := sync.WaitGroup{}

	// defer close of the log stream and await for all the goroutines
	defer func() {
		readCloser.Close()
		wg.Wait()
	}()

	// start a new go routine that iterates over the log stream and performs some checks on the log lines
	wg.Go(func() {
		defer wg.Done()
		scanner := bufio.NewScanner(readCloser)
		for scanner.Scan() && !t.Failed() {
			message := scanner.Bytes()
			structuredLine := &structuredLogLine{}
			// we cannot assert that all the log lines respect the same format, hence we work on a best effort basis:
			// if the unmarshaling succeeds, the log line complies with the format we expect and we check the message severity,
			// otherwise, no reason to make the test failing when the unmarshaling fails
			if err := json.Unmarshal(message, structuredLine); err != nil {
				continue
			}
			// check if the message is in the list of the allowed error messages
			if _, isAllowed := allowedErrorMsgs[structuredLine.Msg]; strings.ToLower(structuredLine.Level) == "error" && isAllowed {
				continue
			}
			// check if the message is a reconciler error ...
			if strings.ToLower(structuredLine.Level) == "error" && structuredLine.Msg == "Reconciler error" {
				// ...and the error message is in the list of the allowedReconcilerErrors ...
				_, isAllowed := allowedReconcilerErrors[structuredLine.Error]
				if isAllowed {
					continue
				}

				// ...or if it matches a known regex.
				if isReconcilerErrorAllowedByRegexMatch(structuredLine.Error) {
					continue
				}

				continue
			}
			// if not, assert that no error occurred
			assert.NotEqualf(t, "error", strings.ToLower(structuredLine.Level), "an error has occurred in the operator: %s", message)
		}
		if !scanner.Scan() {
			t.Log("log stream closed")
		}
	})

	t.Log("deploying a GatewayClass resource")
	gatewayClass := helpers.MustGenerateGatewayClass(t)
	gatewayClass, err = clients.GatewayClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Logf("deploying %d Gateway resources", parallelGateways)
	for i := range parallelGateways {
		// Since we're deploying Gateway which is included in the validating webhook configuration
		// by default we need to use require.EventuallyWithT to ensure we don't fail
		// on the first error(s) from webhook.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			gatewayNN := types.NamespacedName{
				Name:      uuid.NewString(),
				Namespace: testNamespace.Name,
			}
			gateway := helpers.GenerateGateway(gatewayNN, gatewayClass)
			gateway, err = clients.GatewayClient.GatewayV1().Gateways(testNamespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
			require.NoError(c, err)
			cleaner.Add(gateway)
			t.Logf("deployed gateway#%d, name: %q", i, gateway.Name)
		}, concurrentGatewaysReadyTimeLimit, time.Second)
	}

	gateways, err := clients.GatewayClient.GatewayV1().Gateways(testNamespace.Name).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	t.Log("verifying all the Gateways get marked as Programmed")
	for _, gateway := range gateways.Items {
		t.Logf("verifying gateway %q is ready", gateway.Name)
		require.Eventually(t, testutils.GatewayIsProgrammed(t, ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name}, *clients), concurrentGatewaysReadyTimeLimit, time.Second)
		require.Eventually(t, testutils.GatewayListenersAreProgrammed(t, ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name}, *clients), concurrentGatewaysReadyTimeLimit, time.Second)
	}

	t.Log("deleting all the Gateways")
	for _, gateway := range gateways.Items {
		t.Logf("deleting gateway %q", gateway.Name)
		require.NoError(t, clients.GatewayClient.GatewayV1().Gateways(testNamespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))
	}

	t.Log("checking that all the subresources have been deleted")
	for _, gateway := range gateways.Items {
		dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, &gateway, *clients)
		assert.LessOrEqual(t, len(dataplanes), 1)
		controlplanes := testutils.MustListControlPlanesForGateway(t, ctx, &gateway, *clients)
		assert.LessOrEqual(t, len(controlplanes), 1)

		t.Log("verifying the DataPlane sub-resource is deleted")
		if len(dataplanes) != 0 {
			assert.Eventually(t, func() bool {
				_, err := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(testNamespace.Name).Get(ctx, dataplanes[0].Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second)
		}

		t.Log("verifying the ControlPlane sub-resource is deleted")
		if len(controlplanes) != 0 {
			assert.Eventually(t, func() bool {
				_, err := clients.OperatorClient.GatewayOperatorV2beta1().ControlPlanes(testNamespace.Name).Get(ctx, controlplanes[0].Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second)
		}

		t.Log("verifying the networkpolicy is deleted")
		require.Eventually(t, testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, &gateway, *clients)), time.Minute, time.Second)
	}
}

func isReconcilerErrorAllowedByRegexMatch(errorMsg string) bool {
	// For some reason this sometimes happen on CI. While this might
	// be an actual issue, this should not fail the test on its own.
	//
	// Possibly related upstream issue:
	// - https://github.com/kubernetes/kubernetes/issues/124347
	allowedReconcilerErrorRegexes := []string{
		`Operation cannot be fulfilled on dataplanes.gateway-operator.konghq.com \"[a-z0-9-]*\": StorageError: invalid object, Code: 4.*`,
	}

	for _, pattern := range allowedReconcilerErrorRegexes {
		matched, err := regexp.MatchString(pattern, errorMsg)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}

	return false
}
