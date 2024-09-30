package integration

import (
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// dummyValidCertPEM is a dummy valid certificate PEM to be used in tests.
	dummyValidCertPEM = `-----BEGIN CERTIFICATE-----
MIIDPTCCAiUCFG5IolqRiKPMfzTI8peXlaF6cZODMA0GCSqGSIb3DQEBCwUAMFsx
CzAJBgNVBAYTAlVTMQswCQYDVQQIDAJDQTEVMBMGA1UEBwwMRGVmYXVsdCBDaXR5
MRIwEAYDVQQKDAlLb25nIEluYy4xFDASBgNVBAMMC2tvbmdocS50ZWNoMB4XDTI0
MDkyNTA3MjIzOFoXDTM0MDkyMzA3MjIzOFowWzELMAkGA1UEBhMCVVMxCzAJBgNV
BAgMAkNBMRUwEwYDVQQHDAxEZWZhdWx0IENpdHkxEjAQBgNVBAoMCUtvbmcgSW5j
LjEUMBIGA1UEAwwLa29uZ2hxLnRlY2gwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAw
ggEKAoIBAQDXmNBzpWyJ0YUdfCamZpJiwRQMn5vVY8iKQrd3dD03DWyPHu/fXlrL
+QPTRip5d1SrxjzQ4S3fgme442BTlElF9d1w1rhg+DIg6NsW1jd+3IZaICnq7BZH
rJGlW+IWJSKHmNQ39nfVQwgL/QdylrYpbB7uwdEDMa78GfXteiXTcuNobCr7VWVz
rY6rQXo/dImWE1PtMp/EZEMsEbgbQpK5+fUnKTmFncVlDAZ2Q3s2MPikV5UhMVyQ
dKQydU0Ev0LRtpsjW8pQdshMG1ilMq6Yg6YU95gakLVjRXMoDlIJOu08mdped+2Y
VIUSXhRyRt1hbkFP0fXG0THfZ3DjH7jRAgMBAAEwDQYJKoZIhvcNAQELBQADggEB
ANEXlNaQKLrB+jsnNjybsTRkvRRmwjnXaQV0jHzjseGsTJoKY5ABBsSRDiHtqB+9
LPTpHhLYJWsHSLwawIJ3aWDDpF4MNTRsvO12v7wM8Q42OSgkP23O6a5ESkyHRBAb
dLVEp+0Z3kjYwPIglIK37PcgDci6Zim73GOfapDEASNbnCu8js2g/ucYPPXkGMxl
PSUER7MTNf9wRbXrroCE+tZw4kUyUh+6taNlU4ialAJLO1x6UGVRHvPgEx0fAAxA
seBH+A9QMvVl2cKcvrOgZ0CWY01aFRO9ROQ7PrYXqRFvOZu8K3QzLw7xYoK1DTp+
kkO/oPy+WIbqEvj7QrhUXpo=
-----END CERTIFICATE-----
`
	// dummyValidCertKeyPEM is a dummy valid certificate key PEM to be used in tests.
	dummyValidCertKeyPEM = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDXmNBzpWyJ0YUd
fCamZpJiwRQMn5vVY8iKQrd3dD03DWyPHu/fXlrL+QPTRip5d1SrxjzQ4S3fgme4
42BTlElF9d1w1rhg+DIg6NsW1jd+3IZaICnq7BZHrJGlW+IWJSKHmNQ39nfVQwgL
/QdylrYpbB7uwdEDMa78GfXteiXTcuNobCr7VWVzrY6rQXo/dImWE1PtMp/EZEMs
EbgbQpK5+fUnKTmFncVlDAZ2Q3s2MPikV5UhMVyQdKQydU0Ev0LRtpsjW8pQdshM
G1ilMq6Yg6YU95gakLVjRXMoDlIJOu08mdped+2YVIUSXhRyRt1hbkFP0fXG0THf
Z3DjH7jRAgMBAAECggEAOSZ4h1dgDK5+H2FEK5MAFe6BnpEGsYu4YrIpySAGhBvq
XYwBYRA1eGFjmrM8WiOATeKIR4SRcPC0BwY7CBzESafRkfJRQN86BpBDV2vknRve
/3AMPIplo41CtHdFWMJyQ0iHZOhQPrd8oBTsTvtVgWh4UKkO+05FyO0mzFM3SLPs
pqRwMZjLlKVZhbI1l0Ur787tzWpMQQHmd8csAvlak+GIciQWELbVK+5kr/FDpJbq
joIeHX7DCmIqrD/Okwa8SfJu1sutmRX+nrxkDi7trPYcpqriDoWs2jMcnS2GHq9M
lsy2XHn+qLjCpl3/XU61xenWs+Rmmj6J/oIs1zYXCwKBgQDywRS/MNgwMst703Wh
ERJO0rNSR0XVzzoKOqc/ghPkeA8mVNwfNkbgWks+83tuAb6IunMIeyQJ3g/jRhjz
KImsqJmO+DoZCioeaR3PeIWibi9I9Irg6dtoNMwxSmmOtCKD0rccxM1V9OnYkn5a
0Fb+irQSgJYiHrF2SLAT0NoWEwKBgQDjXGLHCu/WEy49vROdkTia133Wc7m71/D5
RDUqvSmAEHJyhTlzCbTO+JcNhC6cx3s102GNcVYHlAq3WoE5EV1YykUNJwUg4XPn
AggNkYOiXs6tf+uePmT8MddixFFgFxZ2bIqFhvnY+WqypHuxtwIepqKJjq5xZTiB
+lfp7SziCwKBgAivofdpXwLyfldy7I2T18zcOzBhfn01CgWdrahXFjEhnqEnfizb
u1OBx5l8CtmX1GJ+EWmnRlXYDUd7lZ71v19fNQdpmGKW+4TVDA0Fafqy6Jw6q9F6
bLBg20GUQQyrI2UGICk2XYaK2ec27rB/Le2zttfGpBiaco0h8rLy0SrjAoGBAM4/
UY/UOQsOrUTuT2wBf8LfNtUid9uSIZRNrplNrebxhJCkkB/uLyoN0iE9xncMcpW6
YmVH6c3IGwyHOnBFc1OHcapjukBApL5rVljQpwPVU1GKmHgdi8hHgmajRlqPtx3I
isRkVCPi5kqV8WueY3rgmNOGLnLJasBmE/gt4ihPAoGAG3v93R5tAeSrn7DMHaZt
p+udsNw9mDPYHAzlYtnw1OE/I0ceR5UyCFSzCd00Q8ZYBLf9jRvsO/GUA4F51isE
8/7xyqSxJqDwzv9N8EGkqf/SfMKA3kK3Sc8u+ovhzJu8OxcY+qrpo4+vYWYeW42n
5XBwvWV2ovRMx7Ntw7FUc24=
-----END PRIVATE KEY-----
`
)

func TestKonnectEntities(t *testing.T) {
	// A cleaner is created underneath anyway, and a whole namespace is deleted eventually.
	// We can't use a cleaner to delete objects because it handles deletes in FIFO order and that won't work in this
	// case: KonnectAPIAuthConfiguration shouldn't be deleted before any other object as that is required for others to
	// complete their finalizer which is deleting a reflecting entity in Konnect. That's why we're only cleaning up a
	// KonnectGatewayControlPlane and waiting for its deletion synchronously with deleteObjectAndWaitForDeletionFn to ensure it
	// was successfully deleted along with its children. The KonnectAPIAuthConfiguration is implicitly deleted along
	// with the namespace.
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect entities test with ID: %s", testID)

	t.Logf("Creating KonnectAPIAuthConfiguration")
	authCfg := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth-" + testID,
			Namespace: ns.Name,
		},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     test.KonnectAccessToken(),
			ServerURL: test.KonnectServerURL(),
		},
	}
	err := GetClients().MgrClient.Create(GetCtx(), authCfg)
	require.NoError(t, err)

	cpName := "cp-" + testID
	t.Logf("Creating KonnectGatewayControlPlane %s", cpName)
	cp := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cpName,
			Namespace: ns.Name,
		},
		Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
			CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
				Name:        cpName,
				ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
				Labels:      map[string]string{"test_id": testID},
			},
			KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
					Name: authCfg.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), cp)
	require.NoError(t, err)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp.GetConditions(), cp.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongService")
	ksName := "ks-" + testID
	ks := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ks-" + testID,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr(ksName),
				URL:  lo.ToPtr("http://example.com"),
				Host: "example.com",
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), ks)
	require.NoError(t, err)

	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, ks.GetConditions(), ks.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongRoute")
	krName := "kr-" + testID
	kr := configurationv1alpha1.KongRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      krName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			ServiceRef: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
					Name: ks.Name,
				},
			},
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Name:  lo.ToPtr(krName),
				Paths: []string{"/kr-" + testID},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kr)
	require.NoError(t, err)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, &kr))

	t.Logf("Waiting for KongRoute to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, &kr)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kr.GetConditions(), kr.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongConsumerGroup")
	kcgName := "kcg-" + testID
	kcg := configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcgName,
			Namespace: ns.Name,
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: kcgName,
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kcg)
	require.NoError(t, err)

	t.Logf("Waiting for KongConsumerGroup to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kcg.Name, Namespace: ns.Name}, &kcg)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kcg.GetConditions(), kcg.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongConsumer")
	kcName := "kc-" + testID
	kc := configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcName,
			Namespace: ns.Name,
		},
		Username: kcName,
		ConsumerGroups: []string{
			kcg.Name,
		},
		Spec: configurationv1.KongConsumerSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
		},
	}
	require.NoError(t, GetClients().MgrClient.Create(GetCtx(), &kc))

	t.Logf("Waiting for KongConsumer to be updated with Konnect ID and Programmed")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kc.Name, Namespace: ns.Name}, &kc)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kc.GetConditions(), kc.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongPlugin and KongPluginBinding")
	kpName := "kp-" + testID
	kp := configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kpName,
			Namespace: ns.Name,
		},
		PluginName: "key-auth",
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kp)
	require.NoError(t, err)

	kpbName := "kpb-" + testID
	kpb := configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kpbName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: kp.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Targets: configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name:  ks.Name,
					Kind:  "KongService",
					Group: "configuration.konghq.com",
				},
			},
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kpb)
	require.NoError(t, err)

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kpb.Name, Namespace: ns.Name}, &kpb)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kpb.GetConditions(), kpb.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("Creating KongUpstream")
	kupName := "kup-" + testID
	kup := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kupName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongUpstreamSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
				Name:      ks.Spec.Host,
				Slots:     lo.ToPtr(int64(16384)),
				Algorithm: sdkkonnectcomp.UpstreamAlgorithmConsistentHashing.ToPointer(),
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), kup)
	require.NoError(t, err)

	t.Log("Waiting for KongUpstream to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kup.Name, Namespace: ns.Name}, kup)
		require.NoError(t, err)

		if !assert.NotNil(t, kup.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("Creating KongTarget")
	ktName := "kt-" + testID
	kt := &configurationv1alpha1.KongTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ktName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongTargetSpec{
			UpstreamRef: configurationv1alpha1.TargetRef{
				Name: kupName,
			},
			KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
				Target: "example.com",
				Weight: 100,
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), kt)
	require.NoError(t, err)

	t.Log("Waiting for KongTarget to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kt.Name, Namespace: ns.Name}, kt)
		require.NoError(t, err)
		if !assert.NotNil(t, kt.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	// Should delete KongTarget because it will block deletion of KongUpstream owning it.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kt))

	t.Logf("Creating KongVault")
	kvName := "kv-" + testID
	kv := configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{
			Name: kvName,
		},
		Spec: configurationv1alpha1.KongVaultSpec{
			Config: apiextensionsv1.JSON{
				Raw: []byte(`{"prefix":"env-vault"}`),
			},
			Backend: "env",
			Prefix:  "env-vault",
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name:      cp.Name,
					Namespace: ns.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kv)
	require.NoError(t, err)

	t.Logf("Waiting for KongVault to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kv.Name}, &kv)
		require.NoError(t, err)

		if !assert.NotNil(t, kv.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kv.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kv.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kv.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongCertificate")
	kcertName := "kcert-" + testID
	kcert := configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcertName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name:      cp.Name,
					Namespace: ns.Name,
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: dummyValidCertPEM,
				Key:  dummyValidCertKeyPEM,
			},
		},
	}
	require.NoError(t, GetClients().MgrClient.Create(GetCtx(), &kcert))

	t.Logf("Waiting for KongCertificate to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      kcertName,
			Namespace: ns.Name,
		}, &kcert)
		require.NoError(t, err)

		if !assert.NotNil(t, kcert.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kcert.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kcert.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kcert.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("Creating a KongSNI attached to KongCertificate")
	ksniName := "ksni-" + testID
	ksni := configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ksniName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: configurationv1alpha1.KongObjectRef{
				Name: kcertName,
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: "test.kong-sni.example.com",
			},
		},
	}
	require.NoError(t, GetClients().MgrClient.Create(GetCtx(), &ksni))

	t.Logf("Waiting for KongSNI to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      ksniName,
			Namespace: ns.Name,
		}, &ksni)
		require.NoError(t, err)

		if !assert.NotNil(t, ksni.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, ksni.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, ksni.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, ksni.Status.Konnect.KonnectEntityStatus.GetServerURL())
		assert.Equal(t, kcert.GetKonnectID(), ksni.Status.Konnect.CertificateID)
	}, testutils.ObjectUpdateTimeout, time.Second)

}

// deleteObjectAndWaitForDeletionFn returns a function that deletes the given object and waits for it to be gone.
// It's designed to be used with t.Cleanup() to ensure the object is properly deleted (it's not stuck with finalizers, etc.).
func deleteObjectAndWaitForDeletionFn(t *testing.T, obj client.Object) func() {
	return func() {
		err := GetClients().MgrClient.Delete(GetCtx(), obj)
		require.NoError(t, err)

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
			assert.True(t, k8serrors.IsNotFound(err))
		}, testutils.ObjectUpdateTimeout, time.Second)
	}
}

// assertKonnectEntityProgrammed asserts that the KonnectEntityProgrammed condition is set to true and the Konnect
// status fields are populated.
func assertKonnectEntityProgrammed(t assert.TestingT, cs []metav1.Condition, konnectStatus *konnectv1alpha1.KonnectEntityStatus) {
	if !assert.NotNil(t, konnectStatus) {
		return
	}
	assert.NotEmpty(t, konnectStatus.GetKonnectID())
	assert.NotEmpty(t, konnectStatus.GetOrgID())
	assert.NotEmpty(t, konnectStatus.GetServerURL())

	assert.True(t, lo.ContainsBy(cs, func(condition metav1.Condition) bool {
		return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
			condition.Status == metav1.ConditionTrue
	}))
}
