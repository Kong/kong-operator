package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/kong/gateway-operator/api/v1alpha1"
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
	kpi.Spec.Image = registryUrl + "plugin-example/valid"
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
			return strings.HasPrefix(cm.Name, kpiNN.Name)
		})
		if !assert.True(c, found) {
			return
		}
	}, 15*time.Second, time.Second)
	checkContentOfRespectiveCM(t, respectiveCM, kpiNN.Name, "plugin-content\n")

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
	checkContentOfRespectiveCM(t, *recreatedCM, kpiNN.Name, "plugin-content\n")

	if registryCreds := GetKongPluginImageRegistryCredentialsForTests(); registryCreds != "" {
		t.Log("update KongPluginInstallation resource to a private image")
		kpi, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Get(GetCtx(), kpiNN.Name, metav1.GetOptions{})
		kpi.Spec.Image = registryUrl + "plugin-example-private/valid:v1.0"
		require.NoError(t, err)
		_, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Update(GetCtx(), kpi, metav1.UpdateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled and report unauthenticated request")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionFalse, "response status code 403: denied: Unauthenticated request. Unauthenticated requests do not have permission",
		)

		t.Log("update KongPluginInstallation resource with credentials reference")
		kpi, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Get(GetCtx(), kpiNN.Name, metav1.GetOptions{})
		secretRef := corev1.SecretReference{Name: "kong-plugin-image-registry-credentials"} // Namespace is not specified, it will be inferred.
		kpi.Spec.ImagePullSecretRef = &secretRef
		_, err = GetClients().OperatorClient.ApisV1alpha1().KongPluginInstallations(kpiNN.Namespace).Update(GetCtx(), kpi, metav1.UpdateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled and report missing Secret with credentials")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionFalse, fmt.Sprintf(`cannot retrieve secret "%s/%s"`, kpiNN.Namespace, secretRef.Name),
		)

		t.Log("add missing Secret with credentials")
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretRef.Name,
			},
			Type: corev1.SecretTypeDockerConfigJson,
			StringData: map[string]string{
				".dockerconfigjson": registryCreds,
			},
		}
		_, err = GetClients().K8sClient.CoreV1().Secrets(kpiNN.Namespace).Create(GetCtx(), &secret, metav1.CreateOptions{})
		require.NoError(t, err)
		t.Log("waiting for the KongPluginInstallation resource to be reconciled successfully")
		checkKongPluginInstallationConditions(
			t, kpiNN, metav1.ConditionTrue, "plugin successfully saved in cluster as ConfigMap",
		)
		var updatedCM *corev1.ConfigMap
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			updatedCM, err = GetClients().K8sClient.CoreV1().ConfigMaps(kpiNN.Namespace).Get(GetCtx(), respectiveCMName, metav1.GetOptions{})
			assert.NoError(c, err)
			checkContentOfRespectiveCM(t, *updatedCM, kpiNN.Name, "plugin-content-private\n")
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

func checkContentOfRespectiveCM(t *testing.T, respectiveCM corev1.ConfigMap, kpiName, expectedPluginContent string) {
	pluginContent, ok := respectiveCM.Data[kpiName+".lua"]
	require.True(t, ok, "plugin.lua not found in ConfigMap")
	require.Equal(t, expectedPluginContent, pluginContent)
}
