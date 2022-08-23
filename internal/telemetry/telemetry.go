package telemetry

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
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

type Payload = provider.Report

// CreateManager creates telemetry manager using the provider rest.Config.
func CreateManager(signal string, restConfig *rest.Config, log logr.Logger, payload Payload) (telemetry.Manager, error) {
	m, err := telemetry.NewManager(
		SignalPing,
		telemetry.OptManagerPeriod(telemetryPeriod),
		telemetry.OptManagerLogger(log),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry manager: %w", err)
	}

	// Add identify cluster workflow
	{
		cl, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create client-go kubernetes client: %w", err)
		}

		w, err := telemetry.NewIdentifyPlatformWorkflow(cl)
		if err != nil {
			return nil, fmt.Errorf("failed to create identify platform workflow: %w", err)
		}
		m.AddWorkflow(w)
	}
	// Add cluster state workflow
	{
		dyn, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create dynamic kubernetes client: %w", err)
		}

		cl, err := client.New(restConfig, client.Options{})
		if err != nil {
			return nil, fmt.Errorf("failed to create controller-runtime's client: %w", err)
		}

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

		p, err := provider.NewFixedValueProvider("payload", payload)
		if err != nil {
			return nil, fmt.Errorf("failed to create fixed value provider: %w", err)
		}
		w.AddProvider(p)

		m.AddWorkflow(w)
	}

	tf, err := forwarders.NewTLSForwarder(splunkEndpoint, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry TLSForwarder: %w", err)
	}

	serializer := serializers.NewSemicolonDelimited()
	consumer := telemetry.NewConsumer(serializer, tf)
	if err := m.AddConsumer(consumer); err != nil {
		return nil, fmt.Errorf("failed to add TLSforwarder: %w", err)
	}

	return m, nil
}
