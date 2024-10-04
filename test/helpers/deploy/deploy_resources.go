package deploy

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// TestIDLabel is the label key used to identify resources created by the test suite.
	TestIDLabel = "konghq.com/test-id"
)

type objOption func(obj client.Object)

// WithAnnotation returns an objOption that sets the given key-value pair as an annotation on the object.
func WithAnnotation(key, value string) objOption {
	return func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[key] = value
		obj.SetAnnotations(annotations)
	}
}

// WithTestIDLabel returns an objOption that sets the test ID label on the object.
func WithTestIDLabel(testID string) func(obj client.Object) {
	return func(obj client.Object) {
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[TestIDLabel] = testID
		obj.SetLabels(labels)
	}
}

// WithLabels returns an objOption that sets the given key-value pairs as labels on the object.
func WithLabels[
	T client.Object,
](labels map[string]string) func(obj T) {
	return func(obj T) {
		for k, v := range labels {
			obj.GetLabels()[k] = v
		}
	}
}

// KonnectAPIAuthConfiguration deploys a KonnectAPIAuthConfiguration resource
// and returns the resource.
func KonnectAPIAuthConfiguration(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...objOption,
) *konnectv1alpha1.KonnectAPIAuthConfiguration {
	t.Helper()

	apiAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "api-auth-config-",
		},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     "kpat_xxxxxx",
			ServerURL: "https://api.us.konghq.com",
		},
	}
	for _, opt := range opts {
		opt(apiAuth)
	}
	require.NoError(t, cl.Create(ctx, apiAuth))
	t.Logf("deployed new %s KonnectAPIAuthConfiguration", client.ObjectKeyFromObject(apiAuth))

	return apiAuth
}

// KonnectAPIAuthConfigurationWithProgrammed deploys a KonnectAPIAuthConfiguration
// resource and returns the resource.
// The Programmed condition is set on the returned resource using status Update() call.
// It can be useful where the reconciler for KonnectAPIAuthConfiguration is not started
// and hence the status has to be filled manually.
func KonnectAPIAuthConfigurationWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *konnectv1alpha1.KonnectAPIAuthConfiguration {
	t.Helper()

	apiAuth := KonnectAPIAuthConfiguration(t, ctx, cl)
	apiAuth.Status.Conditions = []metav1.Condition{
		{
			Type:               conditions.KonnectEntityAPIAuthConfigurationValidConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.KonnectEntityAPIAuthConfigurationReasonValid,
			ObservedGeneration: apiAuth.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, cl.Status().Update(ctx, apiAuth))
	return apiAuth
}

// KonnectGatewayControlPlane deploys a KonnectGatewayControlPlane resource and returns the resource.
func KonnectGatewayControlPlane(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
	opts ...objOption,
) *konnectv1alpha1.KonnectGatewayControlPlane {
	t.Helper()

	name := "cp-" + uuid.NewString()[:8]
	cp := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
					Name: apiAuth.Name,
				},
			},
			CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
				Name: name,
			},
		},
	}
	for _, opt := range opts {
		opt(cp)
	}
	require.NoError(t, cl.Create(ctx, cp))
	t.Logf("deployed new %s KonnectGatewayControlPlane", client.ObjectKeyFromObject(cp))

	return cp
}

// deploy.KonnectGatewayControlPlaneWithID deploys a KonnectGatewayControlPlane resource and returns the resource.
// The Status ID and Programmed condition are set on the CP using status Update() call.
// It can be useful where the reconciler for KonnectGatewayControlPlane is not started
// and hence the status has to be filled manually.
func KonnectGatewayControlPlaneWithID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) *konnectv1alpha1.KonnectGatewayControlPlane {
	t.Helper()

	cp := KonnectGatewayControlPlane(t, ctx, cl, apiAuth)
	cp.Status.Conditions = []metav1.Condition{
		{
			Type:               conditions.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: cp.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	cp.Status.ID = uuid.NewString()[:8]
	require.NoError(t, cl.Status().Update(ctx, cp))
	return cp
}

// KongServiceAttachedToCP deploys a KongService resource and returns the resource.
func KongServiceAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...objOption,
) *configurationv1alpha1.KongService {
	t.Helper()

	name := "kongservice-" + uuid.NewString()[:8]
	kongService := configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr(name),
				URL:  lo.ToPtr("http://example.com"),
				Host: "example.com",
			},
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&kongService)
	}
	require.NoError(t, cl.Create(ctx, &kongService))
	t.Logf("deployed new %s KongService", client.ObjectKeyFromObject(&kongService))

	return &kongService
}

// KongRouteAttachedToService deploys a KongRoute resource and returns the resource.
func KongRouteAttachedToService(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kongService *configurationv1alpha1.KongService,
	opts ...objOption,
) *configurationv1alpha1.KongRoute {
	t.Helper()

	name := "kongroute-" + uuid.NewString()[:8]
	kongRoute := configurationv1alpha1.KongRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Name: lo.ToPtr(name),
			},
			ServiceRef: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
					Name: kongService.Name,
				},
			},
		},
	}
	for _, opt := range opts {
		opt(&kongRoute)
	}
	require.NoError(t, cl.Create(ctx, &kongRoute))
	t.Logf("deployed new %s KongRoute", client.ObjectKeyFromObject(&kongRoute))

	return &kongRoute
}

// KongConsumerWithProgrammed deploys a KongConsumer resource and returns the resource.
func KongConsumerWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumer *configurationv1.KongConsumer,
) *configurationv1.KongConsumer {
	t.Helper()

	consumer.GenerateName = "kongconsumer-"
	require.NoError(t, cl.Create(ctx, consumer))
	t.Logf("deployed %s KongConsumer resource", client.ObjectKeyFromObject(consumer))

	consumer.Status.Conditions = []metav1.Condition{
		{
			Type:               conditions.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: consumer.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, cl.Status().Update(ctx, consumer))

	return consumer
}

// KongPluginBinding deploys a KongPluginBinding resource and returns the resource.
func KongPluginBinding(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kpb *configurationv1alpha1.KongPluginBinding,
	opts ...objOption,
) *configurationv1alpha1.KongPluginBinding {
	t.Helper()

	kpb.GenerateName = "kongpluginbinding-"
	for _, opt := range opts {
		opt(kpb)
	}
	require.NoError(t, cl.Create(ctx, kpb))
	t.Logf("deployed new unmanaged KongPluginBinding %s", client.ObjectKeyFromObject(kpb))

	return kpb
}

// KongCredentialBasicAuth deploys a KongCredentialBasicAuth resource and returns the resource.
func KongCredentialBasicAuth(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumerName string,
	username string,
	password string,
) *configurationv1alpha1.KongCredentialBasicAuth {
	t.Helper()

	c := &configurationv1alpha1.KongCredentialBasicAuth{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "basic-auth-",
		},
		Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: consumerName,
			},
			KongCredentialBasicAuthAPISpec: configurationv1alpha1.KongCredentialBasicAuthAPISpec{
				Password: password,
				Username: username,
			},
		},
	}

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new unmanaged KongCredentialBasicAuth %s", client.ObjectKeyFromObject(c))

	return c
}

// KongCredentialACL deploys a KongCredentialACL resource and returns the resource.
func KongCredentialACL(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumerName string,
	groupName string,
) *configurationv1alpha1.KongCredentialACL {
	t.Helper()

	c := &configurationv1alpha1.KongCredentialACL{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "acl-",
		},
		Spec: configurationv1alpha1.KongCredentialACLSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: consumerName,
			},
			KongCredentialACLAPISpec: configurationv1alpha1.KongCredentialACLAPISpec{
				Group: groupName,
			},
		},
	}

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new unmanaged KongCredentialACL %s", client.ObjectKeyFromObject(c))

	return c
}

// KongCredentialHMAC deploys a KongCredentialHMAC resource and returns the resource.
func KongCredentialHMAC(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumerName string,
) *configurationv1alpha1.KongCredentialHMAC {
	t.Helper()

	c := &configurationv1alpha1.KongCredentialHMAC{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "hmac-",
		},
		Spec: configurationv1alpha1.KongCredentialHMACSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: consumerName,
			},
			KongCredentialHMACAPISpec: configurationv1alpha1.KongCredentialHMACAPISpec{
				Username: lo.ToPtr("username"),
			},
		},
	}

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new unmanaged KongCredentialHMAC %s", client.ObjectKeyFromObject(c))

	return c
}

// KongCredentialJWT deploys a KongCredentialJWT resource and returns the resource.
func KongCredentialJWT(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumerName string,
) *configurationv1alpha1.KongCredentialJWT {
	t.Helper()

	c := &configurationv1alpha1.KongCredentialJWT{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "jwt-",
		},
		Spec: configurationv1alpha1.KongCredentialJWTSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: consumerName,
			},
			KongCredentialJWTAPISpec: configurationv1alpha1.KongCredentialJWTAPISpec{
				Key: lo.ToPtr("key"),
			},
		},
	}

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new unmanaged KongCredentialJWT %s", client.ObjectKeyFromObject(c))

	return c
}

// KongCACertificateAttachedToCP deploys a KongCACertificate resource attached to a CP and returns the resource.
func KongCACertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongCACertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cacert-",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: TestValidCACertPEM,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cert))
	t.Logf("deployed new KongCACertificate %s", client.ObjectKeyFromObject(cert))

	return cert
}

// KongCertificateAttachedToCP deploys a KongCertificate resource attached to a CP and returns the resource.
func KongCertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongCertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cert-",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: TestValidCertPEM,
				Key:  TestValidCertKeyPEM,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cert))
	t.Logf("deployed new KongCertificate %s", client.ObjectKeyFromObject(cert))

	return cert
}

// KongUpstreamAttachedToCP deploys a KongUpstream resource attached to a Control Plane and returns the resource.
func KongUpstreamAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...objOption,
) *configurationv1alpha1.KongUpstream {
	t.Helper()

	u := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "upstream-",
		},
		Spec: configurationv1alpha1.KongUpstreamSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}
	for _, opt := range opts {
		opt(u)
	}

	require.NoError(t, cl.Create(ctx, u))
	t.Logf("deployed new KongUpstream %s", client.ObjectKeyFromObject(u))

	return u
}

// KongTargetAttachedToUpstream deploys a KongTarget resource attached to a Control Plane and returns the resource.
func KongTargetAttachedToUpstream(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	upstream *configurationv1alpha1.KongUpstream,
	opts ...objOption,
) *configurationv1alpha1.KongTarget {
	t.Helper()

	u := &configurationv1alpha1.KongTarget{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "upstream-",
		},
		Spec: configurationv1alpha1.KongTargetSpec{
			UpstreamRef: configurationv1alpha1.TargetRef{
				Name: upstream.Name,
			},
		},
	}
	for _, opt := range opts {
		opt(u)
	}

	require.NoError(t, cl.Create(ctx, u))
	t.Logf("deployed new KongTarget %s", client.ObjectKeyFromObject(u))

	return u
}

// KongConsumerAttachedToCP deploys a KongConsumer resource attached to a Control Plane and returns the resource.
func KongConsumerAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	username string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...objOption,
) *configurationv1.KongConsumer {
	t.Helper()

	c := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "consumer-",
		},
		Spec: configurationv1.KongConsumerSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
		Username: username,
	}
	for _, opt := range opts {
		opt(c)
	}

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new KongConsumer %s", client.ObjectKeyFromObject(c))

	return c
}

// KongConsumerGroupAttachedToCP deploys a KongConsumerGroup resource attached to a Control Plane and returns the resource.
func KongConsumerGroupAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...objOption,
) *configurationv1beta1.KongConsumerGroup {
	t.Helper()

	name := "consumer-group-" + uuid.NewString()[:8]
	cg := configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			Name: name,
		},
	}
	for _, opt := range opts {
		opt(&cg)
	}

	require.NoError(t, cl.Create(ctx, &cg))
	t.Logf("deployed new KongConsumerGroup %s", client.ObjectKeyFromObject(&cg))

	return &cg
}

// KongVaultAttachedToCP deploys a KongVault resource attached to a CP and returns the resource.
func KongVaultAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	backend string,
	prefix string,
	rawConfig []byte,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongVault {
	t.Helper()

	vault := &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vault-",
		},
		Spec: configurationv1alpha1.KongVaultSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name:      cp.Name,
					Namespace: cp.Namespace,
				},
			},
			Config: apiextensionsv1.JSON{
				Raw: rawConfig,
			},
			Prefix:  prefix,
			Backend: backend,
		},
	}

	require.NoError(t, cl.Create(ctx, vault))
	t.Logf("deployed new KongVault %s", client.ObjectKeyFromObject(vault))

	return vault
}

type kongKeyOption func(*configurationv1alpha1.KongKey)

// KongKeyAttachedToCP deploys a KongKey resource attached to a CP and returns the resource.
func KongKeyAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kid, name string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...kongKeyOption,
) *configurationv1alpha1.KongKey {
	t.Helper()

	key := &configurationv1alpha1.KongKey{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "key-",
		},
		Spec: configurationv1alpha1.KongKeySpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
				KID:  kid,
				Name: lo.ToPtr(name),
				JWK:  lo.ToPtr("{}"),
			},
		},
	}
	for _, opt := range opts {
		opt(key)
	}
	require.NoError(t, cl.Create(ctx, key))
	t.Logf("deployed new KongKey %s", client.ObjectKeyFromObject(key))
	return key
}

// ProxyCachePlugin deploys the proxy-cache KongPlugin resource and returns the resource.
// The provided client should be namespaced, i.e. created with `client.NewNamespacedClient(client, ns)`
func ProxyCachePlugin(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *configurationv1.KongPlugin {
	t.Helper()

	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "proxy-cache-kp-",
		},
		PluginName: "proxy-cache",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"response_code": [200], "request_method": ["GET", "HEAD"], "content_type": ["text/plain; charset=utf-8"], "cache_ttl": 300, "strategy": "memory"}`),
		},
	}
	require.NoError(t, cl.Create(ctx, plugin))
	t.Logf("deployed new %s KongPlugin (%s)", client.ObjectKeyFromObject(plugin), plugin.PluginName)
	return plugin
}

// RateLimitingPlugin deploys the rate-limiting KongPlugin resource and returns the resource.
// The provided client should be namespaced, i.e. created with `client.NewNamespacedClient(client, ns)`
func RateLimitingPlugin(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *configurationv1.KongPlugin {
	t.Helper()

	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "rate-limiting-kp-",
		},
		PluginName: "rate-limiting",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"minute": 5, "policy": "local"}`),
		},
	}
	require.NoError(t, cl.Create(ctx, plugin))
	t.Logf("deployed new %s KongPlugin (%s)", client.ObjectKeyFromObject(plugin), plugin.PluginName)
	return plugin
}

// KongKeySetAttachedToCP deploys a KongKeySet resource attached to a CP and returns the resource.
func KongKeySetAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	name string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongKeySet {
	t.Helper()

	keySet := &configurationv1alpha1.KongKeySet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongKeySetSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
				Name: name,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, keySet))
	t.Logf("deployed new KongKeySet %s", client.ObjectKeyFromObject(keySet))

	return keySet
}

// KongSNIAttachedToCertificate deploys a KongSNI resource attached to a KongCertificate and returns the resource.
func KongSNIAttachedToCertificate(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cert *configurationv1alpha1.KongCertificate,
	opts ...objOption,
) *configurationv1alpha1.KongSNI {
	t.Helper()

	name := "sni-" + uuid.NewString()[:8]
	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: configurationv1alpha1.KongObjectRef{
				Name: cert.Name,
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: name,
			},
		},
	}

	for _, opt := range opts {
		opt(sni)
	}

	require.NoError(t, cl.Create(ctx, sni))
	t.Logf("deployed KongSNI %s/%s", sni.Namespace, sni.Name)
	return sni
}

// KongDataPlaneClientCertificateAttachedToCP deploys a KongDataPlaneClientCertificate resource attached to a CP and returns the resource.
func KongDataPlaneClientCertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongDataPlaneClientCertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongDataPlaneClientCertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dp-cert-",
		},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongDataPlaneClientCertificateAPISpec: configurationv1alpha1.KongDataPlaneClientCertificateAPISpec{
				Cert: TestValidCACertPEM,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cert))
	t.Logf("deployed new KongDataPlaneClientCertificate %s", client.ObjectKeyFromObject(cert))

	return cert
}
