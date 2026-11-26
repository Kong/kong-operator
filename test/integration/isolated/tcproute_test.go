package isolated

import (
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	ktfkong "github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/kong/kong-operator/v2/ingress-controller/test"
	dpconf "github.com/kong/kong-operator/v2/ingress-controller/test/dataplane/config"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/testlabels"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/integration/kic/consts"
)

func TestTCPRouteEssentials(t *testing.T) {
	// Constants shared in many steps of this test that doesn't change.
	const gatewayTCPPortName = "tcp"

	const service1Port = ktfkong.DefaultTCPServicePort
	const service1Name = "tcpecho-1"
	test1UUID := uuid.NewString()

	const service2Name = "tcpecho-2"
	const service2Port = 8080
	test2UUID := uuid.NewString()

	f := features.
		New("essentials").
		WithLabel(testlabels.NetworkingFamily, testlabels.NetworkingFamilyGatewayAPI).
		WithLabel(testlabels.Kind, testlabels.KindTCPRoute).
		WithSetup("deploy Gateway managed by Kong Operator", featureSetup(
			withKongRouterFlavor(dpconf.RouterFlavorExpressions),
			withGatewayListeners(gatewayapi.Listener{
				Name:     gatewayTCPPortName,
				Protocol: gatewayapi.TCPProtocolType,
				Port:     gatewayapi.PortNumber(ktfkong.DefaultTCPServicePort),
			}),
		)).
		WithSetup("configure TCP Deployments with Services and TCPRoutes", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			gatewayClient, err := gatewayclient.NewForConfig(cfg.Client().RESTConfig())
			assert.NoError(t, err)
			ctx = SetInCtxForT(ctx, t, gatewayClient)

			cleaner := GetFromCtxForT[*clusters.Cleaner](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			cluster := GetClusterFromCtx(ctx)
			gateway := GetFromCtxForT[*gatewayapi.Gateway](ctx, t)

			t.Log("creating a tcpecho pod to test TCPRoute traffic routing")
			container1 := generators.NewContainer(service1Name, test.EchoImage, test.EchoTCPPort)
			// App go-echo sends a "Running on Pod <UUID>." immediately on connecting.
			container1.Env = []corev1.EnvVar{
				{
					Name:  "POD_NAME",
					Value: test1UUID,
				},
			}
			deployment1 := generators.NewDeploymentForContainer(container1)
			deployment1, err = cluster.Client().AppsV1().Deployments(namespace).Create(ctx, deployment1, metav1.CreateOptions{})
			assert.NoError(t, err)
			cleaner.Add(deployment1)

			t.Log("creating an additional tcpecho pod to test TCPRoute multiple backendRef loadbalancing")
			container2 := generators.NewContainer(service2Name, test.EchoImage, test.EchoTCPPort)
			// App go-echo sends a "Running on Pod <UUID>." immediately on connecting.
			container2.Env = []corev1.EnvVar{
				{
					Name:  "POD_NAME",
					Value: test2UUID,
				},
			}
			deployment2 := generators.NewDeploymentForContainer(container2)
			deployment2, err = cluster.Client().AppsV1().Deployments(namespace).Create(ctx, deployment2, metav1.CreateOptions{})
			assert.NoError(t, err)
			cleaner.Add(deployment2)

			t.Logf("exposing deployment %s/%s via service", deployment1.Namespace, deployment1.Name)
			service1 := generators.NewServiceForDeployment(deployment1, corev1.ServiceTypeLoadBalancer)
			service1.Name = service1Name
			// Use the same port as the default TCP port from the Kong Gateway deployment
			// to the tcpecho port, as this is what will be used to route the traffic at the Gateway.
			service1.Spec.Ports = []corev1.ServicePort{{
				Name:       gatewayTCPPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       service1Port,
				TargetPort: intstr.FromInt(test.EchoTCPPort),
			}}
			service1, err = cluster.Client().CoreV1().Services(namespace).Create(ctx, service1, metav1.CreateOptions{})
			assert.NoError(t, err)
			cleaner.Add(service1)

			t.Logf("exposing deployment %s/%s via service", deployment2.Namespace, deployment2.Name)
			service2 := generators.NewServiceForDeployment(deployment2, corev1.ServiceTypeLoadBalancer)
			service2.Name = service2Name
			// Configure service to expose a different port than Gateway's TCP listener port (ktfkong.DefaultTCPServicePort)
			// to check whether traffic will be routed correctly.
			service2.Spec.Ports = []corev1.ServicePort{{
				Name:       gatewayTCPPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       service2Port,
				TargetPort: intstr.FromInt(test.EchoTCPPort),
			}}
			service2, err = cluster.Client().CoreV1().Services(namespace).Create(ctx, service2, metav1.CreateOptions{})
			assert.NoError(t, err)
			cleaner.Add(service2)

			t.Logf("creating a TCPRoute to access deployment %s via kong", deployment1.Name)
			tcpRoute := &gatewayapi.TCPRoute{
				Name: uuid.NewString(),
				Spec: gatewayapi.TCPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{{
							Name:        gatewayapi.ObjectName(gateway.Name),
							SectionName: new(gatewayapi.SectionName(gatewayTCPPortName)),
						}},
					},
					Rules: []gatewayapi.TCPRouteRule{{
						BackendRefs: []gatewayapi.BackendRef{{
							Name: gatewayapi.ObjectName(service1.Name),
							Port: new(gatewayapi.PortNumber(service1Port)),
						}},
					}},
				},
			}
			tcpRoute, err = gatewayClient.GatewayV1().TCPRoutes(namespace).Create(ctx, tcpRoute, metav1.CreateOptions{})
			assert.NoError(t, err)
			cleaner.Add(tcpRoute)
			ctx = SetInCtxForT(ctx, t, tcpRoute)

			return ctx
		}).
		Assess("basic test - route status and connectivity", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			t.Log("verifying that the Gateway gets linked to the route via status")
			gatewayClient := GetFromCtxForT[*gatewayclient.Clientset](ctx, t)
			tcpRoute := GetFromCtxForT[*gatewayapi.TCPRoute](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			callback := helpers.GetGatewayIsLinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)
			t.Log("verifying that the tcproute contains 'Programmed' condition")
			assert.Eventually(t,
				helpers.GetVerifyProgrammedConditionCallback(t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name, metav1.ConditionTrue),
				consts.IngressWait, consts.WaitTick,
			)

			t.Log("verifying that the tcpecho is responding properly")
			tcpGatewayURL := GetTCPURLFromCtx(ctx)
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			return ctx
		}).
		Assess("verifying behavior when TCPRoute is modified", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			t.Log("removing the parentrefs from the TCPRoute")
			gatewayClient := GetFromCtxForT[*gatewayclient.Clientset](ctx, t)
			tcpRoute := GetFromCtxForT[*gatewayapi.TCPRoute](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			tcpGatewayURL := GetTCPURLFromCtx(ctx)

			oldParentRefs := tcpRoute.Spec.ParentRefs
			assert.Eventually(t, func() bool {
				tcpRoute, err := gatewayClient.GatewayV1().TCPRoutes(namespace).Get(ctx, tcpRoute.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				tcpRoute.Spec.ParentRefs = nil
				_, err = gatewayClient.GatewayV1().TCPRoutes(namespace).Update(ctx, tcpRoute, metav1.UpdateOptions{})
				return err == nil
			}, time.Minute, time.Second)

			t.Log("verifying that the Gateway gets unlinked from the route via status")
			callback := helpers.GetGatewayIsUnlinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that the tcpecho is no longer responding")
			defer func() {
				if t.Failed() {
					err := helpers.EchoResponds(t, helpers.ProtocolTCP, tcpGatewayURL, test1UUID)
					t.Logf("no longer responding check failure state: eof=%v, reset=%v, err=%v",
						errors.Is(err, io.EOF), errors.Is(err, syscall.ECONNRESET), err)
				}
			}()
			assertEventuallyNoResponseTCP(t, tcpGatewayURL)

			t.Log("putting the parentRefs back")
			assert.Eventually(t, func() bool {
				tcpRoute, err := gatewayClient.GatewayV1().TCPRoutes(namespace).Get(ctx, tcpRoute.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				tcpRoute.Spec.ParentRefs = oldParentRefs
				_, err = gatewayClient.GatewayV1().TCPRoutes(namespace).Update(ctx, tcpRoute, metav1.UpdateOptions{})
				return err == nil
			}, time.Minute, time.Second)

			t.Log("verifying that the Gateway gets linked to the route via status")
			callback = helpers.GetGatewayIsLinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that putting the parentRefs back results in the routes becoming available again")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			return ctx
		}).
		Assess("verifying behavior when Gateway is deleted and recreated", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			gatewayClient := GetFromCtxForT[*gatewayclient.Clientset](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			tcpRoute := GetFromCtxForT[*gatewayapi.TCPRoute](ctx, t)
			gatewayClass := GetFromCtxForT[*gatewayapi.GatewayClass](ctx, t)
			gateway := GetFromCtxForT[*gatewayapi.Gateway](ctx, t)
			tcpGatewayURL := GetTCPURLFromCtx(ctx)

			t.Log("deleting the GatewayClass")
			deleteGatewayClass(ctx, t, gatewayClass)

			t.Log("verifying that the Gateway gets unlinked from the route via status")
			callback := helpers.GetGatewayIsUnlinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that the data-plane configuration from the TCPRoute gets dropped with the GatewayClass now removed")
			assertEventuallyNoResponseTCP(t, tcpGatewayURL)

			t.Log("putting the GatewayClass back")
			recreateGatewayClass(ctx, t, gatewayClass)

			t.Log("verifying that the Gateway gets linked to the route via status")
			callback = helpers.GetGatewayIsLinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that creating the GatewayClass again triggers reconciliation of TCPRoutes and the route becomes available again")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			// The operator removes the Gateway's DataPlane and ControlPlane along with it, so there's neither
			// the proxy to send requests to nor the controller to update the TCPRoute's status until it's recreated.
			t.Log("deleting the Gateway")
			deleteGateway(ctx, t, gateway)

			t.Log("putting the Gateway back")
			ctx = recreateGateway(ctx, t, gateway)
			tcpGatewayURL = GetTCPURLFromCtx(ctx)

			t.Log("verifying that the Gateway gets linked to the route via status")
			callback = helpers.GetGatewayIsLinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that creating the Gateway again triggers reconciliation of TCPRoutes and the route becomes available again")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			t.Log("deleting both GatewayClass and Gateway rapidly")
			deleteGatewayClass(ctx, t, gatewayClass)
			deleteGateway(ctx, t, gateway)

			t.Log("putting the GatewayClass back")
			recreateGatewayClass(ctx, t, gatewayClass)

			t.Log("putting the Gateway back")
			ctx = recreateGateway(ctx, t, gateway)
			tcpGatewayURL = GetTCPURLFromCtx(ctx)

			t.Log("verifying that the Gateway gets linked to the route via status")
			callback = helpers.GetGatewayIsLinkedCallback(ctx, t, gatewayClient, gatewayapi.TCPProtocolType, namespace, tcpRoute.Name)
			assert.Eventually(t, callback, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that creating the Gateway again triggers reconciliation of TCPRoutes and the route becomes available again")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			return ctx
		}).
		Assess("verifying behavior with many backends", func(ctx context.Context, t *testing.T, _ *envconf.Config) context.Context {
			gatewayClient := GetFromCtxForT[*gatewayclient.Clientset](ctx, t)
			namespace := GetNamespaceForT(ctx, t)
			tcpRoute := GetFromCtxForT[*gatewayapi.TCPRoute](ctx, t)
			tcpGatewayURL := GetTCPURLFromCtx(ctx)

			t.Log("adding an additional backendRef to the TCPRoute")
			assert.Eventually(t, func() bool {
				tcpRoute, err := gatewayClient.GatewayV1().TCPRoutes(namespace).Get(ctx, tcpRoute.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				tcpRoute.Spec.Rules[0].BackendRefs = []gatewayapi.BackendRef{
					{
						Name: gatewayapi.ObjectName(service1Name),
						Port: new(gatewayapi.PortNumber(service1Port)),
					},
					{
						Name: gatewayapi.ObjectName(service2Name),
						Port: new(gatewayapi.PortNumber(service2Port)),
					},
				}

				_, err = gatewayClient.GatewayV1().TCPRoutes(namespace).Update(ctx, tcpRoute, metav1.UpdateOptions{})
				return err == nil
			}, consts.IngressWait, consts.WaitTick)

			t.Log("verifying that the TCPRoute is now load-balanced between two services")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)
			assertEventuallyResponseTCP(t, tcpGatewayURL, test2UUID)

			t.Log("testing port matching")
			t.Log("verifying that the TCPRoute responds before specifying a port not existent in Gateway")
			assertEventuallyResponseTCP(t, tcpGatewayURL, test1UUID)

			t.Log("setting the port in ParentRef which does not have a matching listener in Gateway")
			_, err := helpers.UpdateWithRetry(ctx, gatewayClient.GatewayV1().TCPRoutes(namespace), tcpRoute.Name, func(r *gatewayapi.TCPRoute) {
				notExistingPort := gatewayapi.PortNumber(81)
				r.Spec.ParentRefs[0].Port = &notExistingPort
				r.Spec.ParentRefs[0].Name = gatewayapi.ObjectName(service1Name)
			})
			assert.NoError(t, err)

			t.Log("verifying that the TCPRoute does not respond after specifying a port not existent in Gateway")
			assertEventuallyNoResponseTCP(t, tcpGatewayURL)
			return ctx
		}).
		Teardown(featureTeardown())

	tenv.Test(t, f.Feature())
}
