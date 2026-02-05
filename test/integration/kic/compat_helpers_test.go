//go:build integration_tests

package integration

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	kongclient "github.com/kong/go-kong/kong"
	dpconf "github.com/kong/kong-operator/ingress-controller/test/dataplane/config"
	"github.com/kong/kong-operator/ingress-controller/test/gatewayapi"
	ic "github.com/kong/kong-operator/ingress-controller/test/helpers"
	ko "github.com/kong/kong-operator/test/helpers"
	ktfkong "github.com/kong/kubernetes-testing-framework/pkg/clusters/addons/kong"
)

// HTTPClientOption bridges to KIC
type HTTPClientOption = ic.HTTPClientOption

// ResponseMatcher bridges to KIC
type ResponseMatcher = ic.ResponseMatcher

// CountHTTPResponsesConfig bridges to KIC
type CountHTTPResponsesConfig = ic.CountHTTPResponsesConfig

// KongBuilder bridges to KIC
type KongBuilder = ktfkong.Builder

// KongAdapterBuilder bridges to KIC
type KongAdapterBuilder = []string

// CleanupFunc bridges to KIC
type CleanupFunc = ic.CleanupFunc

// KongVersion bridges to KIC
type KongVersion = kongclient.Version

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

// SetupTestEnv bridges to shared KO integration helper.
func SetupTestEnv(t *testing.T, ctx context.Context, env environments.Environment) (*corev1.Namespace, *clusters.Cleaner) {
	return ko.SetupTestEnv(t, ctx, env)
}

// RemoveCluster bridges to shared KO integration helper.
func RemoveCluster(ctx context.Context, cluster clusters.Cluster) error {
	return ic.RemoveCluster(ctx, cluster)
}

// TeardownCluster bridges to shared KO integration helper.
func TeardownCluster(ctx context.Context, t *testing.T, cluster clusters.Cluster) {
	ic.TeardownCluster(ctx, t, cluster)
}

// DumpDiagnosticsIfFailed bridges to shared KO integration helper.
func DumpDiagnosticsIfFailed(ctx context.Context, t *testing.T, cluster clusters.Cluster) string {
	return ic.DumpDiagnosticsIfFailed(ctx, t, cluster)
}

// CreateHTTPClient bridges to shared KO integration helper.
func CreateHTTPClient(tlsSecret *corev1.Secret, host string) (*http.Client, error) {
	return ko.CreateHTTPClient(tlsSecret, host)
}

// MustCreateHTTPClient bridges to shared KO integration helper.
func MustCreateHTTPClient(t *testing.T, tlsSecret *corev1.Secret, host string) *http.Client {
	return ko.MustCreateHTTPClient(t, tlsSecret, host)
}

// MustBuildRequest bridges to shared KO integration helper.
func MustBuildRequest(t *testing.T, ctx context.Context, method, url, host string) *http.Request {
	return ko.MustBuildRequest(t, ctx, method, url, host)
}

// Setup bridges to KIC helpers for namespace setup.
func Setup(ctx context.Context, t *testing.T, env environments.Environment) (*corev1.Namespace, *clusters.Cleaner) {
	return ic.Setup(ctx, t, env)
}

// DefaultHTTPClient bridges to KIC
func DefaultHTTPClient(opts ...HTTPClientOption) *http.Client {
	return ic.DefaultHTTPClient(opts...)
}

// MustHTTPRequest bridges to KIC
func MustHTTPRequest(t *testing.T, method string, host, path string, headers map[string]string) *http.Request {
	return ic.MustHTTPRequest(t, method, host, path, headers)
}

// EventuallyGETPath bridges to KIC
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

// EventuallyExpectHTTP404WithNoRoute bridges to KIC
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

// WithResolveHostTo bridges to KIC
func WithResolveHostTo(host string) HTTPClientOption {
	return ic.WithResolveHostTo(host)
}

// GenerateKongBuilder bridges to KIC
func GenerateKongBuilder(ctx context.Context) (*KongBuilder, KongAdapterBuilder, error) {
	return ic.GenerateKongBuilder(ctx)
}

// GenerateKongBuilderWithController bridges to KIC
func GenerateKongBuilderWithController() (*KongBuilder, error) {
	return ic.GenerateKongBuilderWithController()
}

// GetFreePort bridges to KIC
func GetFreePort(t *testing.T) int {
	return ic.GetFreePort(t)
}

// CreateIngressClass bridges to KIC
func CreateIngressClass(ctx context.Context, ingressClassName string, client *kubernetes.Clientset) error {
	return ic.CreateIngressClass(ctx, ingressClassName, client)
}

// DeployGatewayClass bridges to KIC
func DeployGatewayClass(ctx context.Context, client *gatewayclient.Clientset, gatewayClassName string, opts ...func(*gatewayapi.GatewayClass)) (*gatewayapi.GatewayClass, error) {
	return ic.DeployGatewayClass(ctx, client, gatewayClassName, opts...)
}

// DeployGateway bridges to KIC
func DeployGateway(ctx context.Context, client *gatewayclient.Clientset, namespace, gatewayClassName string, opts ...func(*gatewayapi.Gateway)) (*gatewayapi.Gateway, error) {
	return ic.DeployGateway(ctx, client, namespace, gatewayClassName, opts...)
}

// GetGatewayIsLinkedCallback bridges to KIC
func GetGatewayIsLinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsLinkedCallback(ctx, t, c, protocolType, namespace, name)
}

// GetGatewayIsUnlinkedCallback bridges to KIC
func GetGatewayIsUnlinkedCallback(ctx context.Context, t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string) func() bool {
	return ic.GetGatewayIsUnlinkedCallback(ctx, t, c, protocolType, namespace, name)
}

// GetVerifyProgrammedConditionCallback bridges to KIC
func GetVerifyProgrammedConditionCallback(t *testing.T, c *gatewayclient.Clientset, protocolType gatewayapi.ProtocolType, namespace, name string, expectedStatus metav1.ConditionStatus) func() bool {
	return ic.GetVerifyProgrammedConditionCallback(t, c, protocolType, namespace, name, expectedStatus)
}

// MatchRespByStatusAndContent bridges to KIC
func MatchRespByStatusAndContent(responseName string, expectedStatusCode int, expectedBodyContents string) ResponseMatcher {
	return ic.MatchRespByStatusAndContent(responseName, expectedStatusCode, expectedBodyContents)
}

// CountHTTPGetResponses bridges to KIC
func CountHTTPGetResponses(t *testing.T, proxyURL *url.URL, cfg CountHTTPResponsesConfig, matchers ...ResponseMatcher) map[string]int {
	return ic.CountHTTPGetResponses(t, proxyURL, cfg, matchers...)
}

// DistributionOfMapValues bridges to KIC
func DistributionOfMapValues(counter map[string]int) map[string]float64 {
	return ic.DistributionOfMapValues(counter)
}

// WaitForDeploymentRollout bridges to KIC
func WaitForDeploymentRollout(ctx context.Context, t *testing.T, cluster clusters.Cluster, namespace, name string) {
	ic.WaitForDeploymentRollout(ctx, t, cluster, namespace, name)
}

// GetKongVersion bridges to KIC
func GetKongVersion(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (KongVersion, error) {
	return ic.GetKongVersion(ctx, proxyAdminURL, kongTestPassword)
}

// GetKongDBMode bridges to KIC
func GetKongDBMode(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (dpconf.DBMode, error) {
	return ic.GetKongDBMode(ctx, proxyAdminURL, kongTestPassword)
}

// GetKongRouterFlavor bridges to KIC
func GetKongRouterFlavor(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (dpconf.RouterFlavor, error) {
	return ic.GetKongRouterFlavor(ctx, proxyAdminURL, kongTestPassword)
}

// ValidateMinimalSupportedKongVersion bridges to KIC
func ValidateMinimalSupportedKongVersion(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) (KongVersion, error) {
	return ic.ValidateMinimalSupportedKongVersion(ctx, proxyAdminURL, kongTestPassword)
}

// NewKongAdminClient bridges to KIC
func NewKongAdminClient(proxyAdminURL *url.URL, kongTestPassword string) (*kongclient.Client, error) {
	return ic.NewKongAdminClient(proxyAdminURL, kongTestPassword)
}

// GetKongLicenses bridges to KIC
func GetKongLicenses(ctx context.Context, proxyAdminURL *url.URL, kongTestPassword string) ([]*kongclient.License, error) {
	return ic.GetKongLicenses(ctx, proxyAdminURL, kongTestPassword)
}

// ControllerManagerOpt bridges to KIC
type ControllerManagerOpt = ic.ControllerManagerOpt

// ControllerManagerOptAdditionalWatchNamespace bridges to KIC
func ControllerManagerOptAdditionalWatchNamespace(namespace string) ControllerManagerOpt {
	return ic.ControllerManagerOptAdditionalWatchNamespace(namespace)
}

// ControllerManagerOptFlagUseLastValidConfigForFallback bridges to KIC
func ControllerManagerOptFlagUseLastValidConfigForFallback() ControllerManagerOpt {
	return ic.ControllerManagerOptFlagUseLastValidConfigForFallback()
}

// LabelValueForTest bridges to KIC
func LabelValueForTest(t *testing.T) string {
	return ic.LabelValueForTest(t)
}

// DefaultGatewayName bridges to KIC
const DefaultGatewayName = ic.DefaultGatewayName

// TooOldKongGatewayError bridges to KIC
type TooOldKongGatewayError = ic.TooOldKongGatewayError

// ExitOnErr bridges to KIC
func ExitOnErr(ctx context.Context, err error) {
	ic.ExitOnErr(ctx, err)
}

// ExitOnErrWithCode bridges to KIC
func ExitOnErrWithCode(ctx context.Context, err error, exitCode int, fns ...CleanupFunc) {
	ic.ExitOnErrWithCode(ctx, err, exitCode, fns...)
}
