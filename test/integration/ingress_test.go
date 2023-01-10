//go:build integration_tests
// +build integration_tests

package integration

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	netv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/annotations"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"
)

const (
	// ingressClass indicates the ingress class name which the tests will use for supported object reconciliation
	ingressClass = "kong"
)

func TestIngressEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("deploying a GatewayClass resource")
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err := clients.GatewayClient.GatewayV1beta1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewaySpec{
			GatewayClassName: gatewayv1beta1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1beta1.Listener{{
				Name:     "http",
				Protocol: gatewayv1beta1.HTTPProtocolType,
				Port:     gatewayv1beta1.PortNumber(80),
			}},
		},
	}
	gateway, err = clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying Gateway gets an IP address")
	var gatewayIP string
	require.Eventually(t, func() bool {
		gateway, err = clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Get(ctx, gateway.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(gateway.Status.Addresses) > 0 && *gateway.Status.Addresses[0].Type == gatewayv1beta1.IPAddressType {
			gatewayIP = gateway.Status.Addresses[0].Value
			return true
		}
		return false
	}, testutils.DefaultIngressWait, time.Second)

	t.Log("verifying that the DataPlane becomes provisioned")
	var dataplane *operatorv1alpha1.DataPlane
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) == 1 {
			for _, condition := range dataplanes[0].Status.Conditions {
				if condition.Type == string(controllers.DataPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
					dataplane = &dataplanes[0]
					return true
				}
			}
		}
		return false
	}, testutils.SubresourceReadinessWait, time.Second)
	require.NotNil(t, dataplane)

	t.Log("verifying that the ControlPlane becomes provisioned")
	var controlplane *operatorv1alpha1.ControlPlane
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) == 1 {
			for _, condition := range controlplanes[0].Status.Conditions {
				if condition.Type == string(controllers.ControlPlaneConditionTypeProvisioned) && condition.Status == metav1.ConditionTrue {
					controlplane = &controlplanes[0]
					return true
				}
			}
		}
		return false
	}, testutils.SubresourceReadinessWait, time.Second)
	require.NotNil(t, controlplane)

	t.Log("verifying connectivity to the Gateway")
	require.Eventually(t, func() bool {
		resp, err := httpc.Get("http://" + gatewayIP)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return strings.Contains(string(body), testutils.DefaultKongResponseBody)
	}, testutils.DefaultIngressWait, time.Second)

	t.Log("retrieving the kong-proxy url")
	services := testutils.MustListDataPlaneProxyServices(t, ctx, dataplane, clients.MgrClient)
	require.Len(t, services, 1)
	proxyURL, err := urlForService(ctx, env.Cluster(), types.NamespacedName{Namespace: services[0].Namespace, Name: services[0].Name}, testutils.DefaultHTTPPort)
	require.NoError(t, err)

	t.Log("deploying a minimal HTTP container deployment to test Ingress routes")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(deployment)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeLoadBalancer)
	_, err = env.Cluster().Client().CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(service)

	t.Logf("creating an ingress for service %s with ingress.class %s", service.Name, ingressClass)
	kubernetesVersion, err := env.Cluster().Version()
	require.NoError(t, err)
	ingress := generators.NewIngressForServiceWithClusterVersion(kubernetesVersion,
		fmt.Sprintf("/%s-httpbin", strings.ToLower(t.Name())),
		map[string]string{
			annotations.IngressClassKey: ingressClass,
			"konghq.com/strip-path":     "true",
		}, service)
	require.NoError(t, clusters.DeployIngress(ctx, env.Cluster(), namespace.Name, ingress))
	cleaner.Add(ingress.(client.Object))

	t.Log("waiting for updated ingress status to include IP")
	require.Eventually(t, func() bool {
		lbstatus, err := clusters.GetIngressLoadbalancerStatus(ctx, env.Cluster(), namespace.Name, ingress)
		if err != nil {
			t.Logf("failed to get ingress LoadBalancer status: %v", err)
			return false
		}
		return len(lbstatus.Ingress) > 0
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Log("waiting for routes from Ingress to be operational")
	require.Eventually(t, func() bool {
		resp, err := httpc.Get(fmt.Sprintf("%s/%s-httpbin", proxyURL, strings.ToLower(t.Name())))
		if err != nil {
			t.Logf("WARNING: error while waiting for %s: %v", proxyURL, err)
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// now that the ingress backend is routable, make sure the contents we're getting back are what we expect
			// Expected: "<title>httpbin.org</title>"
			b := new(bytes.Buffer)
			n, err := b.ReadFrom(resp.Body)
			require.NoError(t, err)
			require.True(t, n > 0)
			return strings.Contains(b.String(), "<title>httpbin.org</title>")
		}
		return false
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Logf("removing the ingress.class annotation %q from ingress", ingressClass)
	require.Eventually(t, func() bool {
		switch obj := ingress.(type) {
		case *netv1.Ingress:
			ingress, err := env.Cluster().Client().NetworkingV1().Ingresses(namespace.Name).Get(ctx, obj.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			delete(ingress.ObjectMeta.Annotations, annotations.IngressClassKey)
			_, err = env.Cluster().Client().NetworkingV1().Ingresses(namespace.Name).Update(ctx, ingress, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
		case *netv1beta1.Ingress:
			ingress, err := env.Cluster().Client().NetworkingV1beta1().Ingresses(namespace.Name).Get(ctx, obj.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			delete(ingress.ObjectMeta.Annotations, annotations.IngressClassKey)
			_, err = env.Cluster().Client().NetworkingV1beta1().Ingresses(namespace.Name).Update(ctx, ingress, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
		}
		return true
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Logf("verifying that removing the ingress.class annotation %q from ingress causes routes to disconnect", ingressClass)
	require.Eventually(t, func() bool {
		resp, err := httpc.Get(fmt.Sprintf("%s/%s-httpbin", proxyURL, strings.ToLower(t.Name())))
		if err != nil {
			t.Logf("WARNING: error while waiting for %s: %v", proxyURL, err)
			return false
		}
		defer resp.Body.Close()
		return expect404WithNoRoute(t, proxyURL.String(), resp)
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Logf("putting the ingress.class annotation %q back on ingress", ingressClass)
	require.Eventually(t, func() bool {
		switch obj := ingress.(type) {
		case *netv1.Ingress:
			ingress, err := env.Cluster().Client().NetworkingV1().Ingresses(namespace.Name).Get(ctx, obj.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			ingress.ObjectMeta.Annotations[annotations.IngressClassKey] = ingressClass
			_, err = env.Cluster().Client().NetworkingV1().Ingresses(namespace.Name).Update(ctx, ingress, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
		case *netv1beta1.Ingress:
			ingress, err := env.Cluster().Client().NetworkingV1beta1().Ingresses(namespace.Name).Get(ctx, obj.Name, metav1.GetOptions{})
			if err != nil {
				return false
			}
			ingress.ObjectMeta.Annotations[annotations.IngressClassKey] = ingressClass
			_, err = env.Cluster().Client().NetworkingV1beta1().Ingresses(namespace.Name).Update(ctx, ingress, metav1.UpdateOptions{})
			if err != nil {
				return false
			}
		}
		return true
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Log("waiting for routes from Ingress to be operational after reintroducing ingress class annotation")
	require.Eventually(t, func() bool {
		resp, err := httpc.Get(fmt.Sprintf("%s/%s-httpbin", proxyURL, strings.ToLower(t.Name())))
		if err != nil {
			t.Logf("WARNING: error while waiting for %s: %v", proxyURL, err)
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// now that the ingress backend is routable, make sure the contents we're getting back are what we expect
			// Expected: "<title>httpbin.org</title>"
			b := new(bytes.Buffer)
			n, err := b.ReadFrom(resp.Body)
			require.NoError(t, err)
			require.True(t, n > 0)
			return strings.Contains(b.String(), "<title>httpbin.org</title>")
		}
		return false
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)

	t.Log("deleting Ingress and waiting for routes to be torn down")
	require.NoError(t, clusters.DeleteIngress(ctx, env.Cluster(), namespace.Name, ingress))
	require.Eventually(t, func() bool {
		resp, err := httpc.Get(fmt.Sprintf("%s/%s-httpbin", proxyURL, strings.ToLower(t.Name())))
		if err != nil {
			t.Logf("WARNING: error while waiting for %s: %v", proxyURL, err)
			return false
		}
		defer resp.Body.Close()
		return expect404WithNoRoute(t, proxyURL.String(), resp)
	}, testutils.DefaultIngressWait, testutils.WaitIngressTick)
}
