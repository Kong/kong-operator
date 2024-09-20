package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/pkg/consts"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestKongPluginInstallationEssentials(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	const registryUrl = "northamerica-northeast1-docker.pkg.dev/k8s-team-playground/"
	t.Log("deploying an invalid KongPluginInstallation resource")
	kpiNN := k8stypes.NamespacedName{
		Name:      "test-kpi",
		Namespace: namespace.Name,
	}
	kpi := &v1alpha1.KongPluginInstallation{
		ObjectMeta: metav1.ObjectMeta{
			Name: kpiNN.Name,
		},
		Spec: v1alpha1.KongPluginInstallationSpec{
			Image: registryUrl + "plugin-example/invalid-layers",
		},
	}
	kpi, err := GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Create(GetCtx(), kpi, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(kpi)
	t.Log("waiting for the KongPluginInstallation resource to be rejected, because of the invalid image")
	checkKongPluginInstallationConditions(
		t,
		kpiNN,
		metav1.ConditionFalse,
		`problem with the image: "northamerica-northeast1-docker.pkg.dev/k8s-team-playground/plugin-example/invalid-layers" error: expected exactly one layer with plugin, found 2 layers`)

	t.Log("updating KongPluginInstallation resource to a valid image")
	kpi, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Get(GetCtx(), kpiNN.Name, metav1.GetOptions{})
	kpi.Spec.Image = registryUrl + "plugin-example/valid:0.1.0"
	require.NoError(t, err)
	_, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Update(GetCtx(), kpi, metav1.UpdateOptions{})
	require.NoError(t, err)
	t.Log("waiting for the KongPluginInstallation resource to be accepted")
	checkKongPluginInstallationConditions(t, kpiNN, metav1.ConditionTrue, "plugin successfully saved in cluster as ConfigMap")

	var respectiveCM corev1.ConfigMap
	t.Log("check creation and content of respective ConfigMap")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		configMaps, err := GetClients().K8sClient.CoreV1().ConfigMaps(namespace.Name).List(GetCtx(), metav1.ListOptions{})
		if !assert.NoError(c, err) {
			return
		}
		var found bool
		respectiveCM, found = lo.Find(configMaps.Items, func(cm corev1.ConfigMap) bool {
			return cm.Labels[consts.GatewayOperatorManagedByLabel] == consts.KongPluginInstallationManagedLabelValue &&
				cm.Annotations[consts.AnnotationKongPluginInstallationMappedKongPluginInstallation] == kpiNN.String() &&
				strings.HasPrefix(cm.Name, kpiNN.Name)
		})
		if !assert.True(c, found) {
			return
		}
	}, 15*time.Second, time.Second)
	require.Equal(t, pluginExpectedContent(), respectiveCM.Data)

	t.Log("delete respective ConfigMap to check if it will be recreated")
	var respectiveCMName = respectiveCM.Name
	err = GetClients().K8sClient.CoreV1().ConfigMaps(namespace.Name).Delete(GetCtx(), respectiveCMName, metav1.DeleteOptions{})
	require.NoError(t, err)
	t.Log("check recreation of respective ConfigMap")
	var recreatedCM *corev1.ConfigMap
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		recreatedCM, err = GetClients().K8sClient.CoreV1().ConfigMaps(kpiNN.Namespace).Get(GetCtx(), respectiveCMName, metav1.GetOptions{})
		assert.NoError(c, err)
	}, 15*time.Second, time.Second)
	require.Equal(t, pluginExpectedContent(), recreatedCM.Data)

	if registryCreds := GetKongPluginImageRegistryCredentialsForTests(); registryCreds != "" {
		// Create secondNamespace with K8s client to check cross-namespace capabilities.
		secondNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: uuid.NewString(),
			},
		}
		_, err := GetClients().K8sClient.CoreV1().Namespaces().Create(GetCtx(), secondNamespace, metav1.CreateOptions{})
		require.NoError(t, err)
		cleaner.Add(secondNamespace)

		t.Log("update KongPluginInstallation resource to a private image")
		kpi, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Get(GetCtx(), kpiNN.Name, metav1.GetOptions{})
		kpi.Spec.Image = registryUrl + "plugin-example-private/valid:0.1.0"
		require.NoError(t, err)
		_, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Update(GetCtx(), kpi, metav1.UpdateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled and report unauthenticated request")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionFalse, "response status code 403: denied: Unauthenticated request. Unauthenticated requests do not have permission",
		)

		t.Log("update KongPluginInstallation resource with credentials reference in other namespace")
		kpi, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Get(GetCtx(), kpiNN.Name, metav1.GetOptions{})
		require.NoError(t, err)
		secretRef := gatewayv1.SecretObjectReference{
			Kind:      lo.ToPtr(gatewayv1.Kind("Secret")),
			Namespace: lo.ToPtr(gatewayv1.Namespace(secondNamespace.Name)),
			Name:      "kong-plugin-image-registry-credentials",
		}
		kpi.Spec.ImagePullSecretRef = &secretRef
		_, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Update(GetCtx(), kpi, metav1.UpdateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled and report missing ReferenceGrant for the Secret with credentials")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionFalse, fmt.Sprintf("Secret %s/%s reference not allowed by any ReferenceGrant", *secretRef.Namespace, secretRef.Name),
		)
		t.Log("add missing ReferenceGrant for the Secret with credentials")
		refGrant := &gatewayv1beta1.ReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kong-plugin-image-registry-credentials",
				Namespace: secondNamespace.Name,
			},
			Spec: gatewayv1beta1.ReferenceGrantSpec{
				To: []gatewayv1beta1.ReferenceGrantTo{
					{
						Kind: gatewayv1.Kind("Secret"),
						Name: lo.ToPtr(secretRef.Name),
					},
				},
				From: []gatewayv1beta1.ReferenceGrantFrom{
					{
						Group:     gatewayv1.Group(v1alpha1.SchemeGroupVersion.Group),
						Kind:      gatewayv1.Kind("KongPluginInstallation"),
						Namespace: gatewayv1.Namespace(namespace.Name),
					},
				},
			},
		}
		_, err = GetClients().GatewayClient.GatewayV1beta1().ReferenceGrants(secondNamespace.Name).Create(GetCtx(), refGrant, metav1.CreateOptions{})
		require.NoError(t, err)

		t.Log("waiting for the KongPluginInstallation resource to be reconciled and report missing Secret with credentials")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionFalse, fmt.Sprintf(`cannot retrieve secret "%s/%s"`, *secretRef.Namespace, secretRef.Name),
		)

		t.Log("add missing Secret with credentials")
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      string(secretRef.Name),
				Namespace: secondNamespace.Name,
			},
			Type: corev1.SecretTypeDockerConfigJson,
			StringData: map[string]string{
				".dockerconfigjson": registryCreds,
			},
		}
		_, err = GetClients().K8sClient.CoreV1().Secrets(secondNamespace.Name).Create(GetCtx(), &secret, metav1.CreateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled successfully")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionTrue, "plugin successfully saved in cluster as ConfigMap",
		)
		var updatedCM *corev1.ConfigMap
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			updatedCM, err = GetClients().K8sClient.CoreV1().ConfigMaps(kpiNN.Namespace).Get(GetCtx(), respectiveCMName, metav1.GetOptions{})
			assert.NoError(c, err)
			assert.Equal(c, privatePluginExpectedContent(), updatedCM.Data)
		}, 15*time.Second, time.Second)
	} else {
		t.Log("skipping private image test - no credentials provided")
	}

	t.Log("delete KongPluginInstallation resource")
	err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Delete(GetCtx(), kpiNN.Name, metav1.DeleteOptions{})
	require.NoError(t, err)
	t.Log("check deletion of respective ConfigMap")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := GetClients().K8sClient.CoreV1().ConfigMaps(kpiNN.Namespace).Get(GetCtx(), respectiveCM.Name, metav1.GetOptions{})
		assert.True(c, apierrors.IsNotFound(err), "ConfigMap not deleted")
	}, 15*time.Second, time.Second)
}

func checkKongPluginInstallationConditions(
	t *testing.T,
	namespacedName k8stypes.NamespacedName,
	conditionStatus metav1.ConditionStatus,
	expectedMessage string,
) {
	t.Helper()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		kpi, err := GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(namespacedName.Namespace).Get(GetCtx(), namespacedName.Name, metav1.GetOptions{})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotEmpty(c, kpi.Status.Conditions) {
			return
		}
		status := kpi.Status.Conditions[0]
		assert.EqualValues(c, v1alpha1.KongPluginInstallationConditionStatusAccepted, status.Type)
		assert.EqualValues(c, conditionStatus, status.Status)
		if conditionStatus == metav1.ConditionTrue {
			assert.EqualValues(c, v1alpha1.KongPluginInstallationReasonReady, status.Reason)
		} else {
			assert.EqualValues(c, v1alpha1.KongPluginInstallationReasonFailed, status.Reason)
		}
		assert.Contains(c, status.Message, expectedMessage)
	}, 15*time.Second, time.Second)
}

func pluginExpectedContent() map[string]string {
	return map[string]string{
		"handler.lua": "handler-content\n",
		"schema.lua":  "schema-content\n",
	}
}

func privatePluginExpectedContent() map[string]string {
	return map[string]string{
		"handler.lua": "handler-content-private\n",
		"schema.lua":  "schema-content-private\n",
	}
}
