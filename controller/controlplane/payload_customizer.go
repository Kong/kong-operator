package controlplane

import (
	"fmt"
	"os"

	"github.com/kong/kubernetes-ingress-controller/v3/pkg/telemetry/types"
	"github.com/kong/kubernetes-telemetry/pkg/provider"
)

// hostnameRetriever is a function type that retrieves the hostname as a string.
// It returns the hostname and an error if retrieval fails.
type hostnameRetriever func() (string, error)

// payloadCustomizerConfig configuration for dafaultPayloadCustomizer.
type payloadCustomizerConfig struct {
	hostnameRetriever hostnameRetriever
}

// payloadCustomizerOption defines a function type that modifies the payloadCustomizerConfig.
// It is used to provide optional configuration to the defaultPayloadCustomizer.
type payloadCustomizerOption func(*payloadCustomizerConfig)

// withHostnameRetriever returns a payloadCustomizerOption that sets the hostnameRetriever
// function in the payloadCustomizerConfig. This allows customization of how the hostname
// is retrieved, which can be useful for testing or alternative hostname sources.
func withHostnameRetriever(fn hostnameRetriever) payloadCustomizerOption {
	return func(cfg *payloadCustomizerConfig) {
		cfg.hostnameRetriever = fn
	}
}

// defaultPayloadCustomizer creates a PayloadCustomizer that injects the hostname into the payload
// and removes unnecessary fields.
// TODO: Consider setting the hostname via an environment variable using the node name and the Kubernetes Downward API for improved configurability.
// https://github.com/Kong/kong-operator/issues/1783
func defaultPayloadCustomizer(opts ...payloadCustomizerOption) (types.PayloadCustomizer, error) {
	var (
		hostname string
		err      error
		cfg      = &payloadCustomizerConfig{
			hostnameRetriever: os.Hostname,
		}
	)

	// Overwrite options if any.
	for _, opt := range opts {
		opt(cfg)
	}

	if hostname, err = cfg.hostnameRetriever(); err != nil {
		return nil, fmt.Errorf("failed to get defaultPayloadCustomizer: %w", err)
	}

	payloadCustomizer := func(payload types.Payload) types.Payload {
		// Remove the unnecessary version field from the payload.
		// The KIC version is not required.
		delete(payload, "v")
		// Insert the hostname field. This acts as a foreign key to link KIC reports with KO reports.
		payload[provider.HostnameKey] = hostname

		return payload
	}

	return payloadCustomizer, nil
}
