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

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/consts"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

const (
	// TestIDLabel is the label key used to identify resources created by the test suite.
	TestIDLabel = "konghq.com/test-id"
	// KonnectTestIDLabel is the label key added in the Konnect entity used to identify them created by the test suite.
	// Since the label cannot start with `kong`, we use another key.
	KonnectTestIDLabel = "operator-test-id"
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

// WithName returns an ObjOption that sets the name of the object.
func WithName(name string) ObjOption {
	return func(obj client.Object) {
		obj.SetName(name)
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

// WithKonnectNamespacedRefControlPlaneRef returns an ObjOption that sets
// the ControlPlaneRef on the object to a namespaced ref.
//
// NOTE:
// resources requires additional handling ( to only set the namespace when the resource
// is cluster-scoped).
func WithKonnectNamespacedRefControlPlaneRef(cp *konnectv1alpha2.KonnectGatewayControlPlane, ns ...string) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(interface {
			GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
			SetControlPlaneRef(*commonv1alpha1.ControlPlaneRef)
		})
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement GetControlPlaneRef/SetControlPlaneRef method", obj))
		}

		cpRef := &commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: cp.GetName(),
			},
		}
		if len(ns) > 0 {
			cpRef.KonnectNamespacedRef.Namespace = ns[0]
		}

		o.SetControlPlaneRef(cpRef)
	}
}

// WithKonnectExtensionKonnectNamespacedRefControlPlaneRef returns an ObjOption that sets
// the ControlPlaneRef on the konnectExtension to a namespaced ref.
func WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(cp *konnectv1alpha2.KonnectGatewayControlPlane) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(*konnectv1alpha2.KonnectExtension)
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T is not a KonnectExtension resource", obj))
		}

		cpRef := &commonv1alpha1.KonnectExtensionControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: cp.GetName(),
			},
		}

		o.SetControlPlaneRef(cpRef)
	}
}

// WithNamespacedKongServiceRef returns an ObjOption that sets
// the ServiceRef on the object to a namespaced ref.
func WithNamespacedKongServiceRef(svc *configurationv1alpha1.KongService) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(interface {
			GetServiceRef() *configurationv1alpha1.ServiceRef
			SetServiceRef(*configurationv1alpha1.ServiceRef)
		})
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement GetServiceRef/SetServiceRef method", obj))
		}

		svcRef := &configurationv1alpha1.ServiceRef{
			Type: string(commonv1alpha1.ObjectRefTypeNamespacedRef),
			NamespacedRef: &commonv1alpha1.NameRef{
				Name: svc.GetName(),
			},
		}

		o.SetServiceRef(svcRef)
	}
}

// WithKonnectIDControlPlaneRef returns an ObjOption that sets the ControlPlaneRef on the object to a KonnectID.
func WithKonnectIDControlPlaneRef(cp *konnectv1alpha2.KonnectGatewayControlPlane) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(interface {
			GetControlPlaneRef() *commonv1alpha1.ControlPlaneRef
			SetControlPlaneRef(*commonv1alpha1.ControlPlaneRef)
		})
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement GetControlPlaneRef/SetControlPlaneRef method", obj))
		}

		o.SetControlPlaneRef(
			&commonv1alpha1.ControlPlaneRef{
				Type:      commonv1alpha1.ControlPlaneRefKonnectID,
				KonnectID: lo.ToPtr(commonv1alpha1.KonnectIDType(cp.GetKonnectStatus().GetKonnectID())),
			},
		)
	}
}

// WithMirrorSource returns an ObjOption that sets the Source as Mirror and Mirror fields on the object.
func WithMirrorSource(konnectID string) ObjOption {
	return func(obj client.Object) {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement GetServiceRef/SetServiceRef method", obj))
		}
		cp.Spec.Source = lo.ToPtr(commonv1alpha1.EntitySourceMirror)
		cp.Spec.Mirror = &konnectv1alpha2.MirrorSpec{
			Konnect: konnectv1alpha2.MirrorKonnect{
				ID: commonv1alpha1.KonnectIDType(konnectID),
			},
		}
		cp.Spec.CreateControlPlaneRequest = nil
	}
}

// WithKonnectID returns an ObjOption that sets the Konnect ID on the object.
func WithKonnectID(id string) ObjOption {
	return func(obj client.Object) {
		o, ok := obj.(interface {
			SetKonnectID(id string)
		})
		if !ok {
			// As it's only used in tests, we can panic here - it will mean test code is incorrect.
			panic(fmt.Errorf("%T does not implement SetKonnectID method", obj))
		}

		o.SetKonnectID(id)
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
			ServerURL: sdkmocks.SDKServerURL,
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
) *konnectv1alpha2.KonnectGatewayControlPlane {
	t.Helper()

	name := "cp-" + uuid.NewString()[:8]
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
			KonnectConfiguration: konnectv1alpha2.ControlPlaneKonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name: apiAuth.Name,
				},
			},
			CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
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
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			panic(fmt.Errorf("%T does not implement KonnectGatewayControlPlane", obj))
		}
		cp.SetKonnectClusterType(lo.ToPtr(typ))
	}
}

// KonnectGatewayControlPlaneTypeWithCloudGatewaysEnabled returns an ObjOption
// that enabled cloud gateways on the CP.
func KonnectGatewayControlPlaneTypeWithCloudGatewaysEnabled() ObjOption {
	return func(obj client.Object) {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			panic(fmt.Errorf("%T does not implement KonnectGatewayControlPlane", obj))
		}
		cp.SetKonnectCloudGateway(lo.ToPtr(true))
	}
}

// KonnectGatewayControlPlaneLabel returns an ObjOption that adds the given label to the `spec.createControlPlaneRequest.labels`
// of the KonnectGatewayControlPlane.
// This adds the given label on the created control plane in Konnect (instead of the label in the k8s metadata).
func KonnectGatewayControlPlaneLabel(key, value string) ObjOption {
	return func(obj client.Object) {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			panic(fmt.Errorf("%T does not implement KonnectGatewayControlPlane", obj))
		}
		if cp.Spec.CreateControlPlaneRequest == nil {
			cp.Spec.CreateControlPlaneRequest = &sdkkonnectcomp.CreateControlPlaneRequest{}
		}
		if cp.Spec.CreateControlPlaneRequest.Labels == nil {
			cp.Spec.CreateControlPlaneRequest.Labels = map[string]string{}
		}
		cp.Spec.CreateControlPlaneRequest.Labels[key] = value
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
) *konnectv1alpha2.KonnectGatewayControlPlane {
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
	cp.Status.Endpoints = &konnectv1alpha2.KonnectEndpoints{
		ControlPlaneEndpoint: "cp.endpoint",
		TelemetryEndpoint:    "tp.endpoint",
	}
	for _, opt := range opts {
		opt(cp)
	}
	require.NoError(t, cl.Status().Update(ctx, cp))
	return cp
}

// KonnectCloudGatewayDataPlaneGroupConfiguration deploys a
// KonnectCloudGatewayDataPlaneGroupConfiguration resource and returns the resource.
func KonnectCloudGatewayDataPlaneGroupConfiguration(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	dataplaneGroups []konnectv1alpha1.KonnectConfigurationDataPlaneGroup,
	opts ...ObjOption,
) *konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration {
	t.Helper()
	obj := konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "data-plane-group-configuration-" + uuid.NewString()[:8],
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationSpec{
			Version:         consts.DefaultDataPlaneTag,
			APIAccess:       lo.ToPtr(sdkkonnectcomp.APIAccessPrivatePlusPublic),
			DataplaneGroups: dataplaneGroups,
		},
	}

	for _, opt := range opts {
		opt(&obj)
	}

	require.NoError(t, cl.Create(ctx, &obj))
	return &obj
}

// KonnectCloudGatewayNetwork deploys a KonnectCloudGatewayNetwork resource and returns it.
func KonnectCloudGatewayNetwork(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
	opts ...ObjOption,
) *konnectv1alpha1.KonnectCloudGatewayNetwork {
	t.Helper()
	name := "network-" + uuid.NewString()[:8]
	obj := konnectv1alpha1.KonnectCloudGatewayNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: konnectv1alpha1.KonnectCloudGatewayNetworkSpec{
			Name:                          name,
			CloudGatewayProviderAccountID: "1111111111111111111",
			Region:                        "us-east-1",
			AvailabilityZones: []string{
				"us-east-1a",
			},
			CidrBlock: "10.0.0.1/8",
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: apiAuth.Name,
				},
			},
		},
	}

	for _, opt := range opts {
		opt(&obj)
	}

	require.NoError(t, cl.Create(ctx, &obj))
	return &obj
}

// KonnectCloudGatewayNetworkWithProgrammed deploys a KonnectNetwork resource and returns it.
// The Programmed condition is set on the returned resource using status Update() call.
// It can be useful where the reconciler for KonnectNetwork is not started
// and hence the status has to be filled manually.
func KonnectCloudGatewayNetworkWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
	opts ...ObjOption,
) *konnectv1alpha1.KonnectCloudGatewayNetwork {
	t.Helper()

	obj := KonnectCloudGatewayNetwork(t, ctx, cl, apiAuth)
	obj.Status.Conditions = []metav1.Condition{
		{
			Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: obj.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}

	for _, opt := range opts {
		opt(obj)
	}
	require.NoError(t, cl.Status().Update(ctx, obj))
	return obj
}

// KongServiceWithID deploys a KongService resource and returns the resource.
// The Status ID and Programmed condition are set on the Service using status Update() call.
// It can be useful where the reconciler for KonnectGatewayControlPlane is not started
// and hence the status has to be filled manually.
func KongServiceWithID(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *configurationv1alpha1.KongService {
	t.Helper()

	svc := KongService(t, ctx, cl, opts...)
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

// KongService deploys a KongService resource and returns the resource.
func KongService(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
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
		},
	}

	for _, opt := range opts {
		opt(&kongService)
	}
	require.NoError(t, cl.Create(ctx, &kongService))
	logObjectCreate(t, &kongService)

	return &kongService
}

// KongRoute deploys a KongRoute resource and returns the resource.
func KongRoute(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
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
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	opts ...ObjOption,
) *configurationv1alpha1.KongCACertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cacert-",
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
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
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	opts ...ObjOption,
) *configurationv1alpha1.KongCertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongCertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cert-",
		},
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
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

// KongCertificateAttachedToCPWithProgrammed deploys a KongCertificate resource attached to CP
// with the "programmed" condition and the given Konnect ID in the status.
func KongCertificateAttachedToCPWithProgrammed(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	konnectID string,
	opts ...ObjOption,
) *configurationv1alpha1.KongCertificate {
	t.Helper()

	cert := KongCertificateAttachedToCP(t, ctx, cl, cp, opts...)

	if konnectID != "" {
		cert.SetKonnectID(konnectID)
	}
	cert.Status.Conditions = []metav1.Condition{
		{
			Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             konnectv1alpha1.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: cert.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, cl.Status().Update(ctx, cert))
	return cert
}

// KongUpstream deploys a KongUpstream resource and returns it.
func KongUpstream(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *configurationv1alpha1.KongUpstream {
	t.Helper()

	u := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "upstream-",
		},
		Spec: configurationv1alpha1.KongUpstreamSpec{},
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
			UpstreamRef: commonv1alpha1.NameRef{
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

// KongConsumer deploys a KongConsumer resource and returns it.
func KongConsumer(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	username string,
	opts ...ObjOption,
) *configurationv1.KongConsumer {
	t.Helper()

	c := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "consumer-",
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
	opts ...ObjOption,
) *configurationv1beta1.KongConsumerGroup {
	t.Helper()

	name := "consumer-group-" + uuid.NewString()[:8]
	cg := configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
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
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	opts ...ObjOption,
) *configurationv1alpha1.KongVault {
	t.Helper()

	vault := &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vault-",
		},
		Spec: configurationv1alpha1.KongVaultSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
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

// KongKey deploys a KongKey resource and returns the resource.
func KongKey(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kid, name string,
	opts ...ObjOption,
) *configurationv1alpha1.KongKey {
	t.Helper()

	key := &configurationv1alpha1.KongKey{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "key-",
		},
		Spec: configurationv1alpha1.KongKeySpec{
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
	logObjectCreate(t, plugin, "plugin name: %s", plugin.PluginName)
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
	logObjectCreate(t, plugin, "plugin name: %s", plugin.PluginName)
	return plugin
}

// RequestTransformerPlugin deploys the request-transformer KongPlugin resource and returns the resource.
// The provided client should be namespaced, i.e. created with `client.NewNamespacedClient(client, ns)`
func RequestTransformerPlugin(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *configurationv1.KongPlugin {
	t.Helper()

	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "request-transformer-kp-",
		},
		PluginName: "request-transformer",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"add":{"headers":["X-Kong-Test:test"]}}`),
		},
	}
	require.NoError(t, cl.Create(ctx, plugin))
	logObjectCreate(t, plugin, "plugin name: %s", plugin.PluginName)
	return plugin
}

// ResponseTransformerPlugin deploys the response-transformer KongPlugin resource and returns the resource.
// The provided client should be namespaced, i.e. created with `client.NewNamespacedClient(client, ns)`
func ResponseTransformerPlugin(t *testing.T,
	ctx context.Context,
	cl client.Client,
) *configurationv1.KongPlugin {
	t.Helper()

	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "response-transformer-kp-",
		},
		PluginName: "response-transformer",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"add":{"headers":["X-Kong-Test:test"]}}`),
		},
	}
	require.NoError(t, cl.Create(ctx, plugin))
	logObjectCreate(t, plugin, "plugin name: %s", plugin.PluginName)
	return plugin
}

// KongKeySet deploys a KongKeySet resource and returns the resource.
func KongKeySet(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	name string,
	opts ...ObjOption,
) *configurationv1alpha1.KongKeySet {
	t.Helper()

	keySet := &configurationv1alpha1.KongKeySet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongKeySetSpec{
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
			CertificateRef: commonv1alpha1.NameRef{
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

// KongDataPlaneClientCertificateAttachedToCP deploys a KongDataPlaneClientCertificate resource and returns the resource.
func KongDataPlaneClientCertificateAttachedToCP(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *configurationv1alpha1.KongDataPlaneClientCertificate {
	t.Helper()

	cert := &configurationv1alpha1.KongDataPlaneClientCertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dp-cert-",
		},
		Spec: configurationv1alpha1.KongDataPlaneClientCertificateSpec{
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

// KonnectExtension deploys a KonnectExtension.
func KonnectExtension(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *konnectv1alpha2.KonnectExtension {
	t.Helper()

	ke := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "konnect-extension-",
		},
	}
	for _, opt := range opts {
		opt(ke)
	}

	require.NoError(t, cl.Create(ctx, ke))
	logObjectCreate(t, ke)

	return ke
}

// KonnectExtensionReferencingKonnectGatewayControlPlane deploys a KonnectExtension attached to a Konnect CP represented by the given KonnectGatewayControlPlane.
func KonnectExtensionReferencingKonnectGatewayControlPlane(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
) *konnectv1alpha2.KonnectExtension {
	return KonnectExtension(
		t, ctx, cl,
		func(obj client.Object) {
			ke, ok := obj.(*konnectv1alpha2.KonnectExtension)
			require.Truef(t, ok, "Expect object %s/%s to be a KonnectExtension, actual type %T",
				obj.GetNamespace(), obj.GetName(), obj)
			ke.Spec.Konnect.ControlPlane = konnectv1alpha2.KonnectExtensionControlPlane{
				Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name:      cp.Name,
						Namespace: cp.Namespace,
					},
				},
			}
		},
	)
}

// ObjectSupportingKonnectConfiguration defines the interface of types supporting setting `KonnectConfiguration`.
type ObjectSupportingKonnectConfiguration interface {
	*konnectv1alpha2.KonnectGatewayControlPlane |
		*konnectv1alpha1.KonnectCloudGatewayNetwork
}

// ObjectSupportingAdoption defines the interface of types supporting adoption.
type ObjectSupportingAdoption interface {
	client.Object
	SetAdoptOptions(*commonv1alpha1.AdoptOptions)
}

// WithKonnectAdoptOptions returns an option function that sets the adopt options to adopt from Konnect.
func WithKonnectAdoptOptions[T ObjectSupportingAdoption](mode commonv1alpha1.AdoptMode, id string) ObjOption {
	return func(obj client.Object) {
		ent, ok := obj.(T)
		if ok {
			opts := &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: mode,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: id,
				},
			}
			ent.SetAdoptOptions(opts)
		}
	}
}

// ObjectSupportingBindingPlugins defines the interface of types supporting to be set as the target of KongPluginBinding.
type ObjectSupportingBindingPlugins interface {
	*configurationv1alpha1.KongService |
		*configurationv1alpha1.KongRoute |
		*configurationv1.KongConsumer |
		*configurationv1beta1.KongConsumerGroup
	GetName() string
}

// WithKongPluginBindingTarget returns an option function that sets the binding target of the KongPluginBinding.
// The option function also sets the scope of the KongPluginBinding to "OnlyTargets".
func WithKongPluginBindingTarget[T ObjectSupportingBindingPlugins](
	bindTarget T,
) ObjOption {
	return func(obj client.Object) {
		kpb, ok := obj.(*configurationv1alpha1.KongPluginBinding)
		if !ok {
			return
		}
		kpb.Spec.Scope = configurationv1alpha1.KongPluginBindingScopeOnlyTargets
		if kpb.Spec.Targets == nil {
			kpb.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
		}
		// Set the target into the spec.targets of the KongPluginBinding.
		switch any(bindTarget).(type) {
		case *configurationv1alpha1.KongService:
			kpb.Spec.Targets.ServiceReference = &configurationv1alpha1.TargetRefWithGroupKind{
				Group: configurationv1alpha1.GroupVersion.Group,
				Kind:  "KongService",
				Name:  bindTarget.GetName(),
			}
		case *configurationv1alpha1.KongRoute:
			kpb.Spec.Targets.RouteReference = &configurationv1alpha1.TargetRefWithGroupKind{
				Group: configurationv1alpha1.GroupVersion.Group,
				Kind:  "KongRoute",
				Name:  bindTarget.GetName(),
			}
		case *configurationv1.KongConsumer:
			kpb.Spec.Targets.ConsumerReference = &configurationv1alpha1.TargetRef{
				Name: bindTarget.GetName(),
			}
		case *configurationv1beta1.KongConsumerGroup:
			kpb.Spec.Targets.ConsumerGroupReference = &configurationv1alpha1.TargetRef{
				Name: bindTarget.GetName(),
			}
		}
	}
}

// KongReferenceGrant deploys a KongReferenceGrant resource and returns the resource.
func KongReferenceGrant(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	opts ...ObjOption,
) *configurationv1alpha1.KongReferenceGrant {
	t.Helper()

	name := "kongreferencegrant-" + uuid.NewString()[:8]
	krg := configurationv1alpha1.KongReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: configurationv1alpha1.KongReferenceGrantSpec{},
	}

	for _, opt := range opts {
		opt(&krg)
	}
	require.NoError(t, cl.Create(ctx, &krg))
	logObjectCreate(t, &krg)

	return &krg
}

// KongReferenceGrantTos returns an option function that appends the given ReferenceGrantTo entries.
func KongReferenceGrantTos(tos ...configurationv1alpha1.ReferenceGrantTo) ObjOption {
	return func(obj client.Object) {
		krg, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
		if !ok {
			return
		}
		krg.Spec.To = append(krg.Spec.To, tos...)
	}
}

// KongReferenceGrantFroms returns an option function that appends the given ReferenceGrantFrom entries.
func KongReferenceGrantFroms(froms ...configurationv1alpha1.ReferenceGrantFrom) ObjOption {
	return func(obj client.Object) {
		krg, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
		if !ok {
			return
		}
		krg.Spec.From = append(krg.Spec.From, froms...)
	}
}

// Namespace deploys a Namespace and returns it.
func Namespace(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
) *corev1.Namespace {
	t.Helper()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ns-",
		},
	}

	require.NoError(t, cl.Create(ctx, ns))

	return ns
}

func logObjectCreate[
	T interface {
		client.Object
		GetTypeName() string
	},
](t *testing.T, obj T, msgAndArgs ...string) {
	t.Helper()

	if len(msgAndArgs) == 0 {
		t.Logf("deployed new %s %s resource", obj.GetTypeName(), client.ObjectKeyFromObject(obj))
		return
	}

	msg := fmt.Sprintf(msgAndArgs[0], lo.ToAnySlice(msgAndArgs[1:])...)
	t.Logf("deployed new %s %s resource: %s ", obj.GetTypeName(), client.ObjectKeyFromObject(obj), msg)
}
