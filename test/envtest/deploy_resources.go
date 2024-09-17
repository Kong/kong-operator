package envtest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

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
	t.Logf("deployed %s KonnectAPIAuthConfiguration resource", client.ObjectKeyFromObject(apiAuth))

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
	t.Logf("deployed %s KonnectGatewayControlPlane resource", client.ObjectKeyFromObject(cp))

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

// deployKongService deploys a KongService resource and returns the resource.
// The caller can also specify the status which will be updated on the resource.
func deployKongService(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	kongService *configurationv1alpha1.KongService,
) *configurationv1alpha1.KongService {
	t.Helper()

	name := "kongservice-" + uuid.NewString()[:8]
	kongService.Name = name
	kongService.Spec.Name = lo.ToPtr(name)
	require.NoError(t, cl.Create(ctx, kongService))
	t.Logf("deployed %s KongService resource", client.ObjectKeyFromObject(kongService))

	require.NoError(t, cl.Status().Update(ctx, kongService))

	return kongService
}

// deployKongPluginBinding deploys a KongPluginBinding resource and returns the resource.
// The caller can also specify the status which will be updated on the resource.
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

	require.NoError(t, cl.Status().Update(ctx, kpb))
	return kpb
}
