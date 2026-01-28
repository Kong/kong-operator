package helpers

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/kong/go-kong/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	ktfkong "github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/kong"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	dpconf "github.com/kong/kong-operator/ingress-controller/test/dataplane/config"
	"github.com/kong/kong-operator/ingress-controller/test/gatewayapi"
	ic "github.com/kong/kong-operator/ingress-controller/test/helpers"
)

type HTTPClientOption = ic.HTTPClientOption

type ResponseMatcher = ic.ResponseMatcher

type CountHTTPResponsesConfig = ic.CountHTTPResponsesConfig

type ControllerManagerOpt = ic.ControllerManagerOpt

type CleanupFunc = ic.CleanupFunc

type KongBuilder = ktfkong.Builder

type KongVersion = kong.Version

type TooOldKongGatewayError = ic.TooOldKongGatewayError

const DefaultGatewayName = ic.DefaultGatewayName

// KongWithProxyEnvVar sets an env var on the kong builder.
func KongWithProxyEnvVar(builder *ktfkong.Builder, name, value string) {
	builder.WithProxyEnvVar(name, value)
}

// KongWithHelmChartVersion pins helm chart version on the kong builder.
func KongWithHelmChartVersion(builder *ktfkong.Builder, version string) {
	if version != "" {
		builder.WithHelmChartVersion(version)
	}
}

// Setup bridges to ingress-controller test helpers for namespace setup.
func Setup(ctx context.Context, t *testing.T, env environments.Environment) (*corev1.Namespace, *clusters.Cleaner) {
	return ic.Setup(ctx, t, env)
}

// DefaultHTTPClient bridges to ingress-controller test helpers.
func DefaultHTTPClient(opts ...HTTPClientOption) *http.Client {
	return ic.DefaultHTTPClient(opts...)
}

// WithResolveHostTo bridges to ingress-controller test helpers.
func WithResolveHostTo(host string) HTTPClientOption {
	return ic.WithResolveHostTo(host)
}

// MustHTTPRequest bridges to ingress-controller test helpers.
func MustHTTPRequest(t *testing.T, method string, host, path string, headers map[string]string) *http.Request {
	return ic.MustHTTPRequest(t, method, host, path, headers)
}

// EventuallyGETPath bridges to ingress-controller test helpers.
func EventuallyGETPath(
	t *testing.T,
	proxyURL *url.URL,
	host string,
	path string,
	certPool *x509.CertPool,
	statusCode int,
	bodyContent string,
	requestHeaders map[string]string,
	waitDuration time.Duration,
	waitTick time.Duration,
	responseMatchers ...ResponseMatcher,
) {
	ic.EventuallyGETPath(t, proxyURL, host, path, certPool, statusCode, bodyContent, requestHeaders, waitDuration, waitTick, responseMatchers...)
}

// EventuallyExpectHTTP404WithNoRoute bridges to ingress-controller test helpers.
func EventuallyExpectHTTP404WithNoRoute(
	t *testing.T,
	proxyURL *url.URL,
	host string,
	path string,
	waitDuration time.Duration,
	waitTick time.Duration,
	headers map[string]string,
) {
	ic.EventuallyExpectHTTP404WithNoRoute(t, proxyURL, host, path, waitDuration, waitTick, headers)
}

// MatchRespByStatusAndContent bridges to ingress-controller test helpers.
func MatchRespByStatusAndContent(responseName string, expectedStatusCode int, expectedBodyContents string) ResponseMatcher {
	return ic.MatchRespByStatusAndContent(responseName, expectedStatusCode, expectedBodyContents)
}

// CountHTTPGetResponses bridges to ingress-controller test helpers.
func CountHTTPGetResponses(t *testing.T, proxyURL *url.URL, cfg CountHTTPResponsesConfig, matchers ...ResponseMatcher) map[string]int {
	return ic.CountHTTPGetResponses(t, proxyURL, cfg, matchers...)
}

// DistributionOfMapValues bridges to ingress-controller test helpers.
func DistributionOfMapValues(counter map[string]int) map[string]float64 {
	return ic.DistributionOfMapValues(counter)
}

// GenerateKongBuilder bridges to ingress-controller test helpers.
func GenerateKongBuilder(ctx context.Context) (*KongBuilder, []string, error) {
	return ic.GenerateKongBuilder(ctx)
}

// GenerateKongBuilderWithController bridges to ingress-controller test helpers.
func GenerateKongBuilderWithController() (*KongBuilder, error) {
	return ic.GenerateKongBuilderWithController()
}

// GetFreePort bridges to ingress-controller test helpers.
func GetFreePort(t *testing.T) int {
	return ic.GetFreePort(t)
}

// CreateIngressClass bridges to ingress-controller test helpers.
func CreateIngressClass(ctx context.Context, ingressClassName string, client *kubernetes.Clientset) error {
	return ic.CreateIngressClass(ctx, ingressClassName, client)
}

// DeployGatewayClass bridges to ingress-controller test helpers.
func DeployGatewayClass(ctx context.Context, client *gatewayclient.Clientset, gatewayClassName string, opts ...func(*gatewayapi.GatewayClass)) (*gatewayapi.GatewayClass, error) {
	return ic.DeployGatewayClass(ctx, client, gatewayClassName, opts...)
}

// DeployGateway bridges to ingress-controller test helpers.
func DeployGateway(ctx context.Context, client *gatewayclient.Clientset, namespace, gatewayClassName string, opts ...func(*gatewayapi.Gateway)) (*gatewayapi.Gateway, error) {
	return ic.DeployGateway(ctx, client, namespace, gatewayClassName, opts...)
}

// GetGatewayIsLinkedCallback bridges to ingress-controller test helpers.
func GetGatewayIsLinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsLinkedCallback(ctx, t, c, protocolType, namespace, name)
}

// GetGatewayIsUnlinkedCallback bridges to ingress-controller test helpers.
func GetGatewayIsUnlinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsUnlinkedCallback(ctx, t, c, protocolType, namespace, name)
}

// GetVerifyProgrammedConditionCallback bridges to ingress-controller test helpers.
func GetVerifyProgrammedConditionCallback(t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string, expectedStatus metav1.ConditionStatus) func() bool {
	return ic.GetVerifyProgrammedConditionCallback(t, c, protocolType, namespace, name, expectedStatus)
}

// WaitForDeploymentRollout bridges to ingress-controller test helpers.
func WaitForDeploymentRollout(ctx context.Context, t *testing.T, cluster clusters.Cluster, namespace, name string) {
	ic.WaitForDeploymentRollout(ctx, t, cluster, namespace, name)
}

// GetKongVersion bridges to ingress-controller test helpers.
func GetKongVersion(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (KongVersion, error) {
	return ic.GetKongVersion(ctx, proxyAdminURL, kongTestPassword)
}

// GetKongDBMode bridges to ingress-controller test helpers.
func GetKongDBMode(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (dpconf.DBMode, error) {
	return ic.GetKongDBMode(ctx, proxyAdminURL, kongTestPassword)
}

// GetKongRouterFlavor bridges to ingress-controller test helpers.
func GetKongRouterFlavor(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (dpconf.RouterFlavor, error) {
	return ic.GetKongRouterFlavor(ctx, proxyAdminURL, kongTestPassword)
}

// ValidateMinimalSupportedKongVersion bridges to ingress-controller test helpers.
func ValidateMinimalSupportedKongVersion(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (KongVersion, error) {
	return ic.ValidateMinimalSupportedKongVersion(ctx, proxyAdminURL, kongTestPassword)
}

// NewKongAdminClient bridges to ingress-controller test helpers.
func NewKongAdminClient(proxyAdminURL *url.URL, kongTestPassword string) (*kong.Client, error) {
	return ic.NewKongAdminClient(proxyAdminURL, kongTestPassword)
}

// GetKongLicenses bridges to ingress-controller test helpers.
func GetKongLicenses(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) ([]*kong.License, error) {
	return ic.GetKongLicenses(ctx, proxyAdminURL, kongTestPassword)
}

// ControllerManagerOptAdditionalWatchNamespace bridges to ingress-controller test helpers.
func ControllerManagerOptAdditionalWatchNamespace(namespace string) ControllerManagerOpt {
	return ic.ControllerManagerOptAdditionalWatchNamespace(namespace)
}

// ControllerManagerOptFlagUseLastValidConfigForFallback bridges to ingress-controller test helpers.
func ControllerManagerOptFlagUseLastValidConfigForFallback() ControllerManagerOpt {
	return ic.ControllerManagerOptFlagUseLastValidConfigForFallback()
}

// LabelValueForTest bridges to ingress-controller test helpers.
func LabelValueForTest(t *testing.T) string {
	return ic.LabelValueForTest(t)
}

// ExitOnErr bridges to ingress-controller test helpers.
func ExitOnErr(ctx context.Context, err error) {
	ic.ExitOnErr(ctx, err)
}

// ExitOnErrWithCode bridges to ingress-controller test helpers.
func ExitOnErrWithCode(ctx context.Context, err error, exitCode int, fns ...CleanupFunc) {
	ic.ExitOnErrWithCode(ctx, err, exitCode, fns...)
}

// RemoveCluster bridges to ingress-controller test helpers.
func RemoveCluster(ctx context.Context, cluster clusters.Cluster) error {
	return ic.RemoveCluster(ctx, cluster)
}

// TeardownCluster bridges to ingress-controller test helpers.
func TeardownCluster(ctx context.Context, t *testing.T, cluster clusters.Cluster) {
	ic.TeardownCluster(ctx, t, cluster)
}

// DumpDiagnosticsIfFailed bridges to ingress-controller test helpers.
func DumpDiagnosticsIfFailed(ctx context.Context, t *testing.T, cluster clusters.Cluster) string {
	return ic.DumpDiagnosticsIfFailed(ctx, t, cluster)
}
