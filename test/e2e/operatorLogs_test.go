//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testutils "github.com/kong/gateway-operator/internal/utils/test"
)

const (
	// parallelGateways is the total number of gateways that are created and deleted one after the other
	parallelGateways = 5
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
	allowedErrorMsgs = map[string]struct{}{
		"failed setting up anonymous reports": {},
	}
)

func TestOperatorLogs(t *testing.T) {
	var env environments.Environment
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer require.NoError(t, cleanupEnvironment(ctx, env))

	var clients *testutils.K8sClients
	env, clients = createEnvironment(t, ctx)

	testNamespace, cleaner := setup(t, ctx, env)
	defer func() {
		if t.Failed() {
			output, err := cleaner.DumpDiagnostics(ctx, t.Name())
			t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
			assert.NoError(t, err)
		}
		assert.NoError(t, cleaner.Cleanup(ctx))
	}()

	t.Log("finding the Pod for the Gateway Operator")
	podList := &corev1.PodList{}
	err := clients.MgrClient.List(ctx, podList, client.MatchingLabels{
		"control-plane": "controller-manager",
	})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1)
	operatorPod := podList.Items[0]

	t.Log("opening a log stream with the gateway operator pod")
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(readCloser)
		for scanner.Scan() && !t.Failed() {
			message := scanner.Bytes()
			structuredLine := &structuredLogLine{}
			// we cannot assert that all the log lines respect the same format, hence we work on a best effort basis:
			// if the unmarshaling succeeds, the log line complies with the format we expect and we check the message severity,
			// otherwise, no reason to make the test failing when the unmershaling fails
			if err := json.Unmarshal(message, structuredLine); err != nil {
				continue
			}
			// check if the message is in the list of the allowed error messages
			if _, isAllowed := allowedErrorMsgs[structuredLine.Msg]; strings.ToLower(structuredLine.Level) == "error" && isAllowed {
				continue
			}
			// if not, assert that no error occurred
			assert.NotEqualf(t, strings.ToLower(structuredLine.Level), "error", "an error has occurred in the operator: %s", message)
		}
		if !scanner.Scan() {
			t.Log("log stream closed")
		}
	}()

	t.Log("deploying a GatewayClass resource")
	gatewayClass := testutils.GenerateGatewayClass()
	gatewayClass, err = clients.GatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Logf("deploying %d Gateway resourcess", parallelGateways)
	for i := 0; i < parallelGateways; i++ {
		gatewayNN := types.NamespacedName{
			Name:      uuid.NewString(),
			Namespace: testNamespace.Name,
		}
		gateway := testutils.GenerateGateway(gatewayNN, gatewayClass)
		gateway, err = clients.GatewayClient.GatewayV1alpha2().Gateways(testNamespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
		require.NoError(t, err)
		cleaner.Add(gateway)
	}

	gateways, err := clients.GatewayClient.GatewayV1alpha2().Gateways(testNamespace.Name).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)

	t.Log("verifying all the Gateways get marked as Ready")
	for _, gateway := range gateways.Items {
		require.Eventually(t, testutils.GatewayIsReady(t, ctx, types.NamespacedName{Namespace: gateway.Namespace, Name: gateway.Name}, *clients), concurrentGatewaysReadyTimeLimit, time.Second)
	}

	t.Log("deleting all the Gateways")
	for _, gateway := range gateways.Items {
		require.NoError(t, clients.GatewayClient.GatewayV1alpha2().Gateways(testNamespace.Name).Delete(ctx, gateway.Name, metav1.DeleteOptions{}))
	}

	t.Log("checking that all the subresources have been deleted")
	for _, gateway := range gateways.Items {
		gateway := gateway
		dataplanes := testutils.MustListDataPlanesForGateway(t, ctx, &gateway, *clients)
		assert.LessOrEqual(t, len(dataplanes), 1)
		controlplanes := testutils.MustListControlPlanesForGateway(t, ctx, &gateway, *clients)
		assert.LessOrEqual(t, len(controlplanes), 1)

		t.Log("verifying the DataPlane sub-resource is deleted")
		if len(dataplanes) != 0 {
			assert.Eventually(t, func() bool {
				_, err := clients.OperatorClient.ApisV1alpha1().DataPlanes(testNamespace.Name).Get(ctx, dataplanes[0].Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second)
		}

		t.Log("verifying the ControlPlane sub-resource is deleted")
		if len(controlplanes) != 0 {
			assert.Eventually(t, func() bool {
				_, err := clients.OperatorClient.ApisV1alpha1().ControlPlanes(testNamespace.Name).Get(ctx, controlplanes[0].Name, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second)
		}

		t.Log("verifying the networkpolicy is deleted")
		require.Eventually(t, testutils.Not(testutils.GatewayNetworkPoliciesExist(t, ctx, &gateway, *clients)), time.Minute, time.Second)
	}
}
