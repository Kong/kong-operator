package test

import (
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	kubernetesclient "k8s.io/client-go/kubernetes"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorclient "github.com/kong/gateway-operator/pkg/clientset"
)

// K8sClients is a struct that contains all the Kubernetes clients needed by the tests.
type K8sClients struct {
	K8sClient      *kubernetesclient.Clientset
	OperatorClient *operatorclient.Clientset
	GatewayClient  *gatewayclient.Clientset
	MgrClient      ctrlruntimeclient.Client
}

// NewK8sClients returns a new K8sClients struct with all the clients needed by the tests.
func NewK8sClients(env environments.Environment) (K8sClients, error) {
	var err error
	var clients K8sClients

	clients.K8sClient = env.Cluster().Client()
	clients.OperatorClient, err = operatorclient.NewForConfig(env.Cluster().Config())
	if err != nil {
		return clients, err
	}

	clients.GatewayClient, err = gatewayclient.NewForConfig(env.Cluster().Config())
	if err != nil {
		return clients, err
	}

	clients.MgrClient, err = ctrlruntimeclient.New(env.Cluster().Config(), ctrlruntimeclient.Options{})
	if err != nil {
		return clients, err
	}

	if err := gatewayv1beta1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	// TODO: remove this when support for v1alpha2 is dropped in GW API. For now
	// we need to add it to the scheme so that we can pass conformance tests.
	if err := gatewayv1alpha2.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	if err := operatorv1alpha1.AddToScheme(clients.MgrClient.Scheme()); err != nil {
		return clients, err
	}

	return clients, nil
}
