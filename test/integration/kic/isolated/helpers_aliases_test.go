//go:build integration_tests

package isolated

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/kong/go-kong/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	ktfkong "github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/kong"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/kong/kong-operator/ingress-controller/test/gatewayapi"
	ic "github.com/kong/kong-operator/ingress-controller/test/helpers"
)

type (
	ControllerManagerOpt     = ic.ControllerManagerOpt
	ResponseMatcher          = ic.ResponseMatcher
	CountHTTPResponsesConfig = ic.CountHTTPResponsesConfig
)

func ControllerManagerOptAdditionalWatchNamespace(namespace string) ControllerManagerOpt {
	return ic.ControllerManagerOptAdditionalWatchNamespace(namespace)
}

func ControllerManagerOptFlagUseLastValidConfigForFallback() ControllerManagerOpt {
	return ic.ControllerManagerOptFlagUseLastValidConfigForFallback()
}

func DefaultHTTPClient(opts ...ic.HTTPClientOption) *http.Client {
	return ic.DefaultHTTPClient(opts...)
}

func WithResolveHostTo(host string) ic.HTTPClientOption {
	return ic.WithResolveHostTo(host)
}

func MustHTTPRequest(t *testing.T, method string, host, path string, headers map[string]string) *http.Request {
	return ic.MustHTTPRequest(t, method, host, path, headers)
}

func EventuallyGETPath(t *testing.T, proxyURL *url.URL, host, path string, certPool *x509.CertPool, statusCode int, bodyContent string, requestHeaders map[string]string, waitDuration time.Duration, waitTick time.Duration, responseMatchers ...ResponseMatcher) {
	ic.EventuallyGETPath(t, proxyURL, host, path, certPool, statusCode, bodyContent, requestHeaders, waitDuration, waitTick, responseMatchers...)
}

func DeployGatewayClass(ctx context.Context, client *gatewayclient.Clientset, gatewayClassName string, opts ...func(*gatewayapi.GatewayClass)) (*gatewayapi.GatewayClass, error) {
	return ic.DeployGatewayClass(ctx, client, gatewayClassName, opts...)
}

func DeployGateway(ctx context.Context, client *gatewayclient.Clientset, namespace, gatewayClassName string, opts ...func(*gatewayapi.Gateway)) (*gatewayapi.Gateway, error) {
	return ic.DeployGateway(ctx, client, namespace, gatewayClassName, opts...)
}

func GetGatewayIsLinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsLinkedCallback(ctx, t, c, protocolType, namespace, name)
}

func GetGatewayIsUnlinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsUnlinkedCallback(ctx, t, c, protocolType, namespace, name)
}

func GetVerifyProgrammedConditionCallback(t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string, expectedStatus metav1.ConditionStatus) func() bool {
	return ic.GetVerifyProgrammedConditionCallback(t, c, protocolType, namespace, name, expectedStatus)
}

func MatchRespByStatusAndContent(responseName string, expectedStatusCode int, expectedBodyContents string) ResponseMatcher {
	return ic.MatchRespByStatusAndContent(responseName, expectedStatusCode, expectedBodyContents)
}

func CountHTTPGetResponses(t *testing.T, proxyURL *url.URL, cfg CountHTTPResponsesConfig, matchers ...ResponseMatcher) map[string]int {
	return ic.CountHTTPGetResponses(t, proxyURL, cfg, matchers...)
}

func DistributionOfMapValues(counter map[string]int) map[string]float64 {
	return ic.DistributionOfMapValues(counter)
}

func WaitForDeploymentRollout(ctx context.Context, t *testing.T, cluster clusters.Cluster, namespace, name string) {
	ic.WaitForDeploymentRollout(ctx, t, cluster, namespace, name)
}

func GetKongVersion(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (kong.Version, error) {
	return ic.GetKongVersion(ctx, proxyAdminURL, kongTestPassword)
}

func GetKongLicenses(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) ([]*kong.License, error) {
	return ic.GetKongLicenses(ctx, proxyAdminURL, kongTestPassword)
}

func ExitOnErr(ctx context.Context, err error) {
	ic.ExitOnErr(ctx, err)
}

func ExitOnErrWithCode(ctx context.Context, err error, exitCode int, fns ...ic.CleanupFunc) {
	ic.ExitOnErrWithCode(ctx, err, exitCode, fns...)
}

func RemoveCluster(ctx context.Context, cluster clusters.Cluster) error {
	return ic.RemoveCluster(ctx, cluster)
}

func DumpDiagnosticsIfFailed(ctx context.Context, t *testing.T, cluster clusters.Cluster) string {
	return ic.DumpDiagnosticsIfFailed(ctx, t, cluster)
}

func GenerateKongBuilder(ctx context.Context) (*ktfkong.Builder, []string, error) {
	return ic.GenerateKongBuilder(ctx)
}

func GenerateKongBuilderWithController() (*ktfkong.Builder, error) {
	return ic.GenerateKongBuilderWithController()
}

func GetFreePort(t *testing.T) int {
	return ic.GetFreePort(t)
}

func CreateIngressClass(ctx context.Context, ingressClassName string, client *kubernetes.Clientset) error {
	return ic.CreateIngressClass(ctx, ingressClassName, client)
}
