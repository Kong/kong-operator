package crdsvalidation_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestGWAdmin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	gwAdmin := func(mutate func(*operatorv1alpha1.GWAdmin)) *operatorv1alpha1.GWAdmin {
		obj := &operatorv1alpha1.GWAdmin{ObjectMeta: common.CommonObjectMeta(ns.Name)}
		if mutate != nil {
			mutate(obj)
		}
		return obj
	}
	named := func(name string) *operatorv1alpha1.GWAdmin {
		return gwAdmin(func(obj *operatorv1alpha1.GWAdmin) {
			obj.ObjectMeta = metav1.ObjectMeta{Name: name, Namespace: ns.Name}
		})
	}
	withTLS := func(tls operatorv1alpha1.GWAdminTLS) *operatorv1alpha1.GWAdmin {
		return gwAdmin(func(obj *operatorv1alpha1.GWAdmin) { obj.Spec.TLS = &tls })
	}
	manual := operatorv1alpha1.GWAdminCertificateProvisioningManual
	automatic := operatorv1alpha1.GWAdminCertificateProvisioningAutomatic
	ref := &operatorv1alpha1.GWAdminSecretRef{Name: "user-secret"}

	t.Run("name", func(t *testing.T) {
		const msg = "name must be a valid DNS-1035 label of at most 59 characters"
		common.TestCasesGroup[*operatorv1alpha1.GWAdmin]{
			{Name: "empty spec - valid", TestObject: gwAdmin(nil)},
			{Name: "59 characters - valid", TestObject: named("a" + strings.Repeat("0", 58))},
			{Name: "60 characters - invalid", TestObject: named("a" + strings.Repeat("0", 59)), ExpectedErrorMessage: new(msg)},
			{Name: "contains a dot - invalid", TestObject: named("gw.admin"), ExpectedErrorMessage: new(msg)},
			{Name: "starts with a digit - invalid", TestObject: named("1gw"), ExpectedErrorMessage: new(msg)},
			{Name: "ends with '-' - invalid", TestObject: named("gw-"), ExpectedErrorMessage: new(msg)},
			{Name: "uppercase - invalid", TestObject: named("GW"), ExpectedErrorMessage: new("a lowercase RFC 1123 subdomain")},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("deployment", func(t *testing.T) {
		common.TestCasesGroup[*operatorv1alpha1.GWAdmin]{
			{
				Name: "replicas - valid",
				TestObject: gwAdmin(func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{Replicas: new(int32(2))}
				}),
			},
			{
				Name: "negative replicas - invalid",
				TestObject: gwAdmin(func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{Replicas: new(int32(-1))}
				}),
				ExpectedErrorMessage: new("should be greater than or equal to 0"),
			},
			{
				Name: "replicas and scaling - invalid",
				TestObject: gwAdmin(func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{
						Replicas: new(int32(2)),
						Scaling: &operatorv1alpha1.GWAdminScaling{
							HorizontalScaling: &commonv1alpha1.HorizontalScaling{MaxReplicas: 3},
						},
					}
				}),
				ExpectedErrorMessage: new("Using both replicas and scaling fields is not allowed."),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("tls", func(t *testing.T) {
		const secretRefMsg = "secretRef must be set when provisioning is Manual and only then"
		common.TestCasesGroup[*operatorv1alpha1.GWAdmin]{
			{
				Name:       "manual CA with secretRef - valid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: manual, SecretRef: ref}}),
			},
			{
				Name:                 "manual CA without secretRef - invalid",
				TestObject:           withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: manual}}),
				ExpectedErrorMessage: new(secretRefMsg),
			},
			{
				Name:                 "automatic CA with secretRef - invalid",
				TestObject:           withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: automatic, SecretRef: ref}}),
				ExpectedErrorMessage: new(secretRefMsg),
			},
			{
				Name: "manual cp with secretRef - valid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{CP: &operatorv1alpha1.GWAdminServerCertificate{
					Provisioning: manual, SecretRef: ref,
				}}),
			},
			{
				Name: "manual cp without secretRef - invalid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{CP: &operatorv1alpha1.GWAdminServerCertificate{
					Provisioning: manual,
				}}),
				ExpectedErrorMessage: new(secretRefMsg),
			},
			{
				Name:                 "invalid provisioning - invalid",
				TestObject:           withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: "Bogus"}}),
				ExpectedErrorMessage: new(`Unsupported value: "Bogus"`),
			},
			{
				Name:       "empty CA defaults to Automatic - valid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{}, CP: &operatorv1alpha1.GWAdminServerCertificate{}}),
				Assert: func(t *testing.T, obj *operatorv1alpha1.GWAdmin) {
					require.NotNil(t, obj.Spec.TLS)
					require.NotNil(t, obj.Spec.TLS.ClientCA)
					require.NotNil(t, obj.Spec.TLS.CP)
					assert.Equal(t, automatic, obj.Spec.TLS.ClientCA.Provisioning)
					assert.Equal(t, automatic, obj.Spec.TLS.CP.Provisioning)
				},
			},
			{
				Name:                 "cp secretRef without provisioning (defaults to Automatic) - invalid",
				TestObject:           withTLS(operatorv1alpha1.GWAdminTLS{CP: &operatorv1alpha1.GWAdminServerCertificate{SecretRef: ref}}),
				ExpectedErrorMessage: new(secretRefMsg),
			},
			{
				Name:                 "CA secretRef without provisioning (defaults to Automatic) - invalid",
				TestObject:           withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{SecretRef: ref}}),
				ExpectedErrorMessage: new(secretRefMsg),
			},
			{
				Name: "empty secretRef name - invalid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{
					Provisioning: manual, SecretRef: &operatorv1alpha1.GWAdminSecretRef{},
				}}),
				ExpectedErrorMessage: new("spec.tls.clientCA.secretRef.name"),
			},
			{
				Name:       "Automatic to Manual CA on update - valid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: automatic}}),
				Update: func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.TLS.ClientCA = &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: manual, SecretRef: ref}
				},
			},
			{
				Name:       "Manual to Automatic CA on update - valid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: manual, SecretRef: ref}}),
				Update: func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.TLS.ClientCA = &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: automatic}
				},
			},
			{
				Name:       "Manual to Automatic CA keeping the secretRef on update - invalid",
				TestObject: withTLS(operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{Provisioning: manual, SecretRef: ref}}),
				Update: func(obj *operatorv1alpha1.GWAdmin) {
					obj.Spec.TLS.ClientCA.Provisioning = automatic
				},
				ExpectedUpdateErrorMessage: new(secretRefMsg),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("network", func(t *testing.T) {
		withPorts := func(ports ...operatorv1alpha1.GWAdminServicePort) *operatorv1alpha1.GWAdmin {
			return gwAdmin(func(obj *operatorv1alpha1.GWAdmin) {
				obj.Spec.Network = &operatorv1alpha1.GWAdminNetworkOptions{CP: &operatorv1alpha1.GWAdminServiceOptions{
					Ports: ports,
				}}
			})
		}
		common.TestCasesGroup[*operatorv1alpha1.GWAdmin]{
			{
				Name:                 "port 0 - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new("cp"), Port: 0}),
				ExpectedErrorMessage: new("spec.network.cp.ports[0].port"),
			},
			{Name: "port 65535 - valid", TestObject: withPorts(operatorv1alpha1.GWAdminServicePort{Name: new("cp"), Port: 65535})},
			{
				Name:                 "port 65536 - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new("cp"), Port: 65536}),
				ExpectedErrorMessage: new("should be less than or equal to 65535"),
			},
			{
				Name:                 "unnamed port - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Port: 8099}),
				ExpectedErrorMessage: new("Every port must be named"),
			},
			{
				Name: "one unnamed port among named ones - invalid",
				TestObject: withPorts(
					operatorv1alpha1.GWAdminServicePort{Name: new("cp"), Port: 8099},
					operatorv1alpha1.GWAdminServicePort{Port: 8100},
				),
				ExpectedErrorMessage: new("Every port must be named"),
			},
			{
				Name: "several named ports - valid",
				TestObject: withPorts(
					operatorv1alpha1.GWAdminServicePort{Name: new("cp"), Port: 8099},
					operatorv1alpha1.GWAdminServicePort{Name: new("cp-alt"), Port: 8100},
				),
			},
			{
				Name:       "63 characters port name - valid",
				TestObject: withPorts(operatorv1alpha1.GWAdminServicePort{Name: new(strings.Repeat("a", 63)), Port: 8099}),
			},
			{
				Name:                 "64 characters port name - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new(strings.Repeat("a", 64)), Port: 8099}),
				ExpectedErrorMessage: new("spec.network.cp.ports[0].name: Too long"),
			},
			{
				Name:                 "empty port name - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new(""), Port: 8099}),
				ExpectedErrorMessage: new("spec.network.cp.ports[0].name"),
			},
			{
				Name:                 "uppercase port name - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new("CP"), Port: 8099}),
				ExpectedErrorMessage: new("should match"),
			},
			{
				Name:                 "port name ending in '-' - invalid",
				TestObject:           withPorts(operatorv1alpha1.GWAdminServicePort{Name: new("cp-"), Port: 8099}),
				ExpectedErrorMessage: new("should match"),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("status", func(t *testing.T) {
		const msg = "serverURL must be an https URL with a host"
		setServerURL := func(url string) func(*operatorv1alpha1.GWAdmin) {
			return func(obj *operatorv1alpha1.GWAdmin) {
				obj.Status.CP = &operatorv1alpha1.GWAdminCPStatus{ServerURL: url}
			}
		}
		common.TestCasesGroup[*operatorv1alpha1.GWAdmin]{
			{Name: "https serverURL - valid", TestObject: gwAdmin(nil), StatusUpdate: setServerURL("https://gw-cp.ns.svc:8099")},
			{
				Name: "http serverURL - invalid", TestObject: gwAdmin(nil), StatusUpdate: setServerURL("http://gw-cp.ns.svc:8099"),
				ExpectedStatusUpdateErrorMessage: new(msg),
			},
			{
				Name: "serverURL without a host - invalid", TestObject: gwAdmin(nil), StatusUpdate: setServerURL("https://"),
				ExpectedStatusUpdateErrorMessage: new(msg),
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
