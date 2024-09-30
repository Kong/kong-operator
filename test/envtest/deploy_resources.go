package envtest

import (
	"context"
	"testing"

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

// deployKonnectAPIAuthConfiguration deploys a KonnectAPIAuthConfiguration resource
// and returns the resource.
func deployKonnectAPIAuthConfiguration(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
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
	require.NoError(t, cl.Create(ctx, apiAuth))
	t.Logf("deployed new %s KonnectAPIAuthConfiguration", client.ObjectKeyFromObject(apiAuth))

	return apiAuth
}

// deployKonnectAPIAuthConfigurationWithProgrammed deploys a KonnectAPIAuthConfiguration
// resource and returns the resource.
// The Programmed condition is set on the returned resource using status Update() call.
// It can be useful where the reconciler for KonnectAPIAuthConfiguration is not started
// and hence the status has to be filled manually.
func deployKonnectAPIAuthConfigurationWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *konnectv1alpha1.KonnectAPIAuthConfiguration {
	t.Helper()

	apiAuth := deployKonnectAPIAuthConfiguration(t, ctx, cl)
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

// deployKonnectGatewayControlPlane deploys a KonnectGatewayControlPlane resource and returns the resource.
func deployKonnectGatewayControlPlane(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) *konnectv1alpha1.KonnectGatewayControlPlane {
	t.Helper()

	cp := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cp-",
		},
		Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
					Name: apiAuth.Name,
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cp))
	t.Logf("deployed new %s KonnectGatewayControlPlane", client.ObjectKeyFromObject(cp))

	return cp
}

// deployKonnectGatewayControlPlaneWithID deploys a KonnectGatewayControlPlane resource and returns the resource.
// The Status ID and Programmed condition are set on the CP using status Update() call.
// It can be useful where the reconciler for KonnectGatewayControlPlane is not started
// and hence the status has to be filled manually.
func deployKonnectGatewayControlPlaneWithID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) *konnectv1alpha1.KonnectGatewayControlPlane {
	t.Helper()

	cp := deployKonnectGatewayControlPlane(t, ctx, cl, apiAuth)
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

// deployKongServiceAttachedToCP deploys a KongService resource and returns the resource.
func deployKongServiceAttachedToCP(
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

// deployKongRouteAttachedToService deploys a KongRoute resource and returns the resource.
func deployKongRouteAttachedToService(
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

// deployKongConsumerWithProgrammed deploys a KongConsumer resource and returns the resource.
func deployKongConsumerWithProgrammed(
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

// deployKongPluginBinding deploys a KongPluginBinding resource and returns the resource.
func deployKongPluginBinding(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kpb *configurationv1alpha1.KongPluginBinding,
) *configurationv1alpha1.KongPluginBinding {
	t.Helper()

	kpb.GenerateName = "kongpluginbinding-"
	require.NoError(t, cl.Create(ctx, kpb))
	t.Logf("deployed new unmanaged KongPluginBinding %s", client.ObjectKeyFromObject(kpb))

	return kpb
}

// deployKongCredentialBasicAuth deploys a KongCredentialBasicAuth resource and returns the resource.
func deployKongCredentialBasicAuth(
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

// deployKongCredentialACL deploys a KongCredentialACL resource and returns the resource.
func deployKongCredentialACL(
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

// deployKongCACertificateAttachedToCP deploys a KongCACertificate resource attached to a CP and returns the resource.
func deployKongCACertificateAttachedToCP(
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
				Cert: dummyValidCACertPEM,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cert))
	t.Logf("deployed new KongCACertificate %s", client.ObjectKeyFromObject(cert))

	return cert
}

// deployKongCertificateAttachedToCP deploys a KongCertificate resource attached to a CP and returns the resource.
func deployKongCertificateAttachedToCP(
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
				Cert: dummyValidCertPEM,
				Key:  dummyValidCertKeyPEM,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cert))
	t.Logf("deployed new KongCertificate %s", client.ObjectKeyFromObject(cert))

	return cert
}

// deployKongConsumer deploys a KongConsumer resource attached to a Control Plane and returns the resource.
func deployKongConsumerAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	username string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
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

	require.NoError(t, cl.Create(ctx, c))
	t.Logf("deployed new KongConsumer %s", client.ObjectKeyFromObject(c))

	return c
}

// deployKongConsumerGroupAttachedToCP deploys a KongConsumerGroup resource attached to a Control Plane and returns the resource.
func deployKongConsumerGroupAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cgName string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1beta1.KongConsumerGroup {
	t.Helper()

	cg := configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "consumer-group-",
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			Name: cgName,
		},
	}

	require.NoError(t, cl.Create(ctx, &cg))
	t.Logf("deployed new KongConsumerGroup %s", client.ObjectKeyFromObject(&cg))

	return &cg
}

func deployKongVaultAttachedToCP(
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

// deployKongKeyAttachedToCP deploys a KongKey resource attached to a CP and returns the resource.
func deployKongKeyAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kid, name string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
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
	require.NoError(t, cl.Create(ctx, key))
	t.Logf("deployed new KongKey %s", client.ObjectKeyFromObject(key))
	return key
}

// deployProxyCachePlugin deploys the proxy-cache KongPlugin resource and returns the resource.
// The provided client should be namespaced, i.e. created with `client.NewNamespacedClient(client, ns)`
func deployProxyCachePlugin(
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

// deployKongKeySetAttachedToCP deploys a KongKeySet resource attached to a CP and returns the resource.
func deployKongKeySetAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	name string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) *configurationv1alpha1.KongKeySet {
	t.Helper()

	keySet := &configurationv1alpha1.KongKeySet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "key-set-",
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

func deploySNIAttachedToCertificate(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	name string, tags []string,
	cert *configurationv1alpha1.KongCertificate,
) *configurationv1alpha1.KongSNI {
	t.Helper()

	sni := &configurationv1alpha1.KongSNI{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "sni-",
		},
		Spec: configurationv1alpha1.KongSNISpec{
			CertificateRef: configurationv1alpha1.KongObjectRef{
				Name: cert.Name,
			},
			KongSNIAPISpec: configurationv1alpha1.KongSNIAPISpec{
				Name: name,
				Tags: tags,
			},
		},
	}
	require.NoError(t, cl.Create(ctx, sni))
	t.Logf("deployed KongSNI %s/%s", sni.Namespace, sni.Name)
	return sni
}
