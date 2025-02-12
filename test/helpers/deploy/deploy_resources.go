package deploy

import (
	"context"
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// TestIDLabel is the label key used to identify resources created by the test suite.
	TestIDLabel = "konghq.com/test-id"
)

// ObjOption is a function that modifies a  client.Object.
type ObjOption func(obj client.Object)

// WithAnnotation returns an ObjOption that sets the given key-value pair as an annotation on the object.
func WithAnnotation(key, value string) ObjOption {
	return func(obj client.Object) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		annotations[key] = value
		obj.SetAnnotations(annotations)
	}
}

// WithLabel returns an ObjOption that sets the given key-value pair as an label on the object.
func WithLabel(key, value string) ObjOption {
	return func(obj client.Object) {
		labels := obj.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[key] = value
		obj.SetLabels(labels)
	}
}

// WithTestIDLabel returns an ObjOption that sets the test ID label on the object.
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

// WithKonnectIDControlPlaneRef returns an ObjOption that sets the ControlPlaneRef on the object to a KonnectID.
func WithKonnectIDControlPlaneRef(cp *konnectv1alpha1.KonnectGatewayControlPlane) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(interface {
			GetControlPlaneRef() *configurationv1alpha1.ControlPlaneRef
		})
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement GetControlPlaneRef method", obj))
		}

		objCPRef := o.GetControlPlaneRef()
		*objCPRef = configurationv1alpha1.ControlPlaneRef{
			Type:      configurationv1alpha1.ControlPlaneRefKonnectID,
			KonnectID: lo.ToPtr(cp.GetKonnectStatus().GetKonnectID()),
		}
	}
}

// KonnectAPIAuthConfiguration deploys a KonnectAPIAuthConfiguration resource
// and returns the resource.
func KonnectAPIAuthConfiguration(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
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
	logObjectCreate(t, apiAuth)

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
			Type:               konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
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
	opts ...ObjOption,
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
	logObjectCreate(t, cp)

	return cp
}

// KonnectGatewayControlPlaneType returns an ObjOption that sets the cluster type on the CP.
func KonnectGatewayControlPlaneType(typ sdkkonnectcomp.CreateControlPlaneRequestClusterType) ObjOption {
	return func(obj client.Object) {
		cp, ok := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
		if !ok {
			panic(fmt.Errorf("%T does not implement KonnectGatewayControlPlane", obj))
		}
		cp.Spec.CreateControlPlaneRequest.ClusterType = &typ
	}
}

// KonnectGatewayControlPlaneWithID deploys a KonnectGatewayControlPlane resource and returns the resource.
// The Status ID and Programmed condition are set on the CP using status Update() call.
// It can be useful where the reconciler for KonnectGatewayControlPlane is not started
// and hence the status has to be filled manually.
func KonnectGatewayControlPlaneWithID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
	opts ...ObjOption,
) *konnectv1alpha1.KonnectGatewayControlPlane {
	t.Helper()

	cp := KonnectGatewayControlPlane(t, ctx, cl, apiAuth, opts...)
	cp.Status.Conditions = []metav1.Condition{
		{
			Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: cp.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	cp.Status.ID = uuid.NewString()[:8]
	require.NoError(t, cl.Status().Update(ctx, cp))
	return cp
}

// KongServiceAttachedToCPWithID deploys a KongService resource and returns the resource.
// The Status ID and Programmed condition are set on the Service using status Update() call.
// It can be useful where the reconciler for KonnectGatewayControlPlane is not started
// and hence the status has to be filled manually.
func KongServiceAttachedToCPWithID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
) *configurationv1alpha1.KongService {
	t.Helper()

	svc := KongServiceAttachedToCP(t, ctx, cl, cp, opts...)
	svc.Status.Conditions = []metav1.Condition{
		{
			Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: svc.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	svc.SetKonnectID(uuid.NewString()[:8])
	require.NoError(t, cl.Status().Update(ctx, svc))
	return svc
}

// KongServiceAttachedToCP deploys a KongService resource and returns the resource.
func KongServiceAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	logObjectCreate(t, &kongService)

	return &kongService
}

// KongRouteAttachedToService deploys a KongRoute resource and returns the resource.
func KongRouteAttachedToService(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kongService *configurationv1alpha1.KongService,
	opts ...ObjOption,
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
				NamespacedRef: &configurationv1alpha1.KongObjectRef{
					Name: kongService.Name,
				},
			},
		},
	}
	for _, opt := range opts {
		opt(&kongRoute)
	}
	require.NoError(t, cl.Create(ctx, &kongRoute))
	logObjectCreate(t, &kongRoute)

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
			Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
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
	opts ...ObjOption,
) *configurationv1alpha1.KongPluginBinding {
	t.Helper()

	kpb.GenerateName = "kongpluginbinding-"
	for _, opt := range opts {
		opt(kpb)
	}
	require.NoError(t, cl.Create(ctx, kpb))
	logObjectCreate(t, kpb)

	return kpb
}

// KongCredentialAPIKey deploys a KongCredentialAPIKey resource and returns the resource.
func KongCredentialAPIKey(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	consumerName string,
) *configurationv1alpha1.KongCredentialAPIKey {
	t.Helper()

	c := &configurationv1alpha1.KongCredentialAPIKey{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "api-key-",
		},
		Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: consumerName,
			},
			KongCredentialAPIKeyAPISpec: configurationv1alpha1.KongCredentialAPIKeyAPISpec{
				Key: "key",
			},
		},
	}

	require.NoError(t, cl.Create(ctx, c))
	logObjectCreate(t, c)

	return c
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
	logObjectCreate(t, c)

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
	logObjectCreate(t, c)

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
	logObjectCreate(t, c)

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
	logObjectCreate(t, c)

	return c
}

// KongCACertificateAttachedToCP deploys a KongCACertificate resource attached to a CP and returns the resource.
func KongCACertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	for _, opt := range opts {
		opt(cert)
	}
	require.NoError(t, cl.Create(ctx, cert))
	logObjectCreate(t, cert)

	return cert
}

// KongCertificateAttachedToCP deploys a KongCertificate resource attached to a CP and returns the resource.
func KongCertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	for _, opt := range opts {
		opt(cert)
	}
	require.NoError(t, cl.Create(ctx, cert))
	logObjectCreate(t, cert)

	return cert
}

// KongUpstreamAttachedToCP deploys a KongUpstream resource attached to a Control Plane and returns the resource.
func KongUpstreamAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	logObjectCreate(t, u)

	return u
}

// KongTargetAttachedToUpstream deploys a KongTarget resource attached to a Control Plane and returns the resource.
func KongTargetAttachedToUpstream(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	upstream *configurationv1alpha1.KongUpstream,
	opts ...ObjOption,
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
	logObjectCreate(t, u)

	return u
}

// Secret deploys a Secret.
func Secret(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	data map[string][]byte,
	opts ...ObjOption,
) *corev1.Secret {
	t.Helper()

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "secret-",
		},
		Data: data,
	}
	for _, opt := range opts {
		opt(s)
	}

	require.NoError(t, cl.Create(ctx, s))

	return s
}

// KongConsumerAttachedToCP deploys a KongConsumer resource attached to a Control Plane and returns the resource.
func KongConsumerAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	username string,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	logObjectCreate(t, c)

	return c
}

// KongConsumerGroupAttachedToCP deploys a KongConsumerGroup resource attached to a Control Plane and returns the resource.
func KongConsumerGroupAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
	opts ...ObjOption,
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
	logObjectCreate(t, &cg)

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
	opts ...ObjOption,
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

	for _, opt := range opts {
		opt(vault)
	}

	require.NoError(t, cl.Create(ctx, vault))
	logObjectCreate(t, vault)

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
	logObjectCreate(t, key)
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
	opts ...ObjOption,
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

	for _, opt := range opts {
		opt(keySet)
	}

	require.NoError(t, cl.Create(ctx, keySet))
	logObjectCreate(t, keySet)

	return keySet
}

// KongSNIAttachedToCertificate deploys a KongSNI resource attached to a KongCertificate and returns the resource.
func KongSNIAttachedToCertificate(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cert *configurationv1alpha1.KongCertificate,
	opts ...ObjOption,
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
	opts ...ObjOption,
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

	for _, opt := range opts {
		opt(cert)
	}

	require.NoError(t, cl.Create(ctx, cert))
	logObjectCreate(t, cert)

	return cert
}

func logObjectCreate[
	T interface {
		client.Object
		GetTypeName() string
	},
](t *testing.T, obj T) {
	t.Helper()

	t.Logf("deployed new %s %s resource", obj.GetTypeName(), client.ObjectKeyFromObject(obj))
}
