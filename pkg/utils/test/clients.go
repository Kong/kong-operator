package test

import (
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kubernetesclient "k8s.io/client-go/kubernetes"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	configurationclient "github.com/kong/kubernetes-configuration/v2/pkg/clientset"

	configurationv1 "github.com/kong/kong-operator/apis/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/apis/configuration/v1beta1"
	operatorv1alpha1 "github.com/kong/kong-operator/apis/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/apis/v1alpha2"
	operatorv2beta1 "github.com/kong/kong-operator/apis/v2beta1"
)

// K8sClients is a struct that contains all the Kubernetes clients needed by the tests.
type K8sClients struct {
	K8sClient           *kubernetesclient.Clientset
	OperatorClient      *configurationclient.Clientset
	GatewayClient       *gatewayclient.Clientset
	ConfigurationClient *configurationclient.Clientset
	MgrClient           ctrlruntimeclient.Client
}

// NewK8sClients returns a new K8sClients struct with all the clients needed by the tests.
func NewK8sClients(env environments.Environment) (K8sClients, error) {
	var err error
	var clients K8sClients

	clients.K8sClient = env.Cluster().Client()
	clients.OperatorClient, err = configurationclient.NewForConfig(env.Cluster().Config())
	if err != nil {
		return clients, err
	}
	clients.GatewayClient, err = gatewayclient.NewForConfig(env.Cluster().Config())
	if err != nil {
		return clients, err
	}
	clients.ConfigurationClient, err = configurationclient.NewForConfig(env.Cluster().Config())
	if err != nil {
		return clients, err
	}

	clients.MgrClient, err = ctrlruntimeclient.New(env.Cluster().Config(), ctrlruntimeclient.Options{})
	if err != nil {
		return clients, err
	}

	if err := apiextensionsv1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := gatewayv1.Install(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := gatewayv1beta1.Install(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := konnectv1alpha1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := konnectv1alpha2.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := configurationv1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := configurationv1beta1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := configurationv1alpha1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := configurationv1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}
	if err := configurationv1beta1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	// TODO: remove this when support for v1alpha2 is dropped in GW API. For now
	// we need to add it to the scheme so that we can pass conformance tests.
	if err := gatewayv1alpha2.Install(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	if err := operatorv1alpha1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	if err := operatorv1beta1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	if err := operatorv2beta1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	return clients, nil
}
