package envtest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

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
	cp.Status.Conditions = []metav1.Condition{
		{
			Type:               conditions.KonnectEntityProgrammedConditionType,
			Status:             metav1.ConditionTrue,
			Reason:             conditions.KonnectEntityProgrammedReasonProgrammed,
			ObservedGeneration: cp.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		},
	}
	cp.Status.ID = uuid.NewString()
	require.NoError(t, cl.Status().Update(ctx, cp))
	return cp
}

func deployKongPluginBinding(
	t *testing.T,
	ctx context.Context,
	cl client.Client,
	spec configurationv1alpha1.KongPluginBindingSpec,
) *configurationv1alpha1.KongPluginBinding {
	t.Helper()

	kpb := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kongpluginbinding-",
		},
		Spec: spec,
	}

	require.NoError(t, cl.Create(ctx, kpb))
	return kpb
}
