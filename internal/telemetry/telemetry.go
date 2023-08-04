package telemetry

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	splunkEndpoint  = "kong-hf.konghq.com:61833"
	telemetryPeriod = time.Hour

	SignalStart = "gateway-operator-start"
	SignalPing  = "gateway-operator-ping"
)

type Payload = types.ProviderReport

// CreateManager creates telemetry manager using the provided rest.Config.
func CreateManager(signal string, restConfig *rest.Config, log logr.Logger, payload Payload) (telemetry.Manager, error) {

	k, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client-go kubernetes client: %w", err)
	}

	cl, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller-runtime's client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic kubernetes client: %w", err)
	}

	m, err := createManager(
		types.Signal(SignalPing),
		k,
		cl,
		dyn,
		payload,
		telemetry.OptManagerPeriod(telemetryPeriod),
		telemetry.OptManagerLogger(log),
	)
	if err != nil {
		return nil, err
	}

	tf, err := forwarders.NewTLSForwarder(splunkEndpoint, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry TLSForwarder: %w", err)
	}
	serializer := serializers.NewSemicolonDelimited()
	consumer := telemetry.NewConsumer(serializer, tf)
	err = m.AddConsumer(consumer)
	if err != nil {
		return nil, fmt.Errorf("failed to add telemetry consumer to manager: %w", err)
	}

	return m, nil
}

// createManager creates a telemetry manager with given kubernetes clientset, dynamic client and consumer.
//
//	It was separated to allow testing with mocked dependencies.
func createManager(
	signal types.Signal,
	k kubernetes.Interface,
	cl client.Client,
	dyn dynamic.Interface,
	fixedPayload Payload,
	opts ...telemetry.OptManager,
) (telemetry.Manager, error) {
	m, err := telemetry.NewManager(signal, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry manager: %w", err)
	}

	// Add identify cluster workflow
	{
		w, err := telemetry.NewIdentifyPlatformWorkflow(k)
		if err != nil {
			return nil, fmt.Errorf("failed to create identify platform workflow: %w", err)
		}
		m.AddWorkflow(w)
	}
	// Add cluster state workflow
	{
		w, err := telemetry.NewClusterStateWorkflow(dyn, cl.RESTMapper())
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster state workflow: %w", err)
		}

		m.AddWorkflow(w)
	}
	// Add state workflow
	{
		w, err := telemetry.NewStateWorkflow()
		if err != nil {
			return nil, fmt.Errorf("failed to create state workflow: %w", err)
		}

		p, err := provider.NewFixedValueProvider("payload", fixedPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to create fixed value provider: %w", err)
		}
		w.AddProvider(p)

		m.AddWorkflow(w)
	}

	return m, nil
}
