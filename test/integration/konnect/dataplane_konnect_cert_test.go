package konnect

import (
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerv1client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/dataplane/certificates"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/envs"
	"github.com/kong/kong-operator/v2/test/helpers/volumes"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestDataPlaneKonnectCert(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	clients := integration.GetClients()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, integration.GetEnv())

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	issuer := &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-cluster-issuer",
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
	certClient, err := certmanagerv1client.NewForConfig(integration.GetEnv().Cluster().Config())
	require.NoError(t, err)
	_, err = certClient.ClusterIssuers().Create(ctx, issuer, metav1.CreateOptions{})
	require.NoError(t, err)
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
									},
								},
							},
						},
					},
				},
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
						Issuer: operatorv1beta1.NamespacedName{
							Name: "fake-cluster-issuer",
						},
					},
				},
			},
		},
	}

	dataplaneClient := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplane, err = dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, ctx, dataplaneName, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), time.Minute*2, time.Second)

	t.Log("verifying dataplane Deployment.Pods.Env vars")
	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	certEnv := envs.GetValueByName(proxyContainer.Env, consts.ClusterCertEnvKey)
	keyEnv := envs.GetValueByName(proxyContainer.Env, consts.ClusterCertKeyEnvKey)
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath+"tls.crt", certEnv)
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath+"tls.key", keyEnv)

	require.NotEmpty(t, volumes.GetByName(deployment.Spec.Template.Spec.Volumes, certificates.DataPlaneKonnectClientCertificateName))
	mount := volumes.GetMountsByVolumeName(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, certificates.DataPlaneKonnectClientCertificateName)[0]
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath, mount.MountPath)
}
