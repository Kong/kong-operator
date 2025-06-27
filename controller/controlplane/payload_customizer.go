package controlplane

import (
	"fmt"
	"os"

	"github.com/kong/kubernetes-ingress-controller/v3/pkg/telemetry/types"
	"github.com/kong/kubernetes-telemetry/pkg/provider"
)

// defaultPayloadCustomizer creates a PayloadCustomizer that injects the hostname into the payload
// and removes unnecessary fields.
// TODO: Consider setting the hostname via an environment variable using the node name and the Kubernetes Downward API for improved configurability.
func defaultPayloadCustomizer(hostnameRetriever func() (string, error)) (types.PayloadCustomizer, error) {
	var hostname string
	var err error
	if hostnameRetriever != nil {
		if hostname, err = hostnameRetriever(); err != nil {
			return nil, fmt.Errorf("failed to get defaultPayloadCustomizer: %w", err)
		}
	} else {
		// Get the hostname where KO is running.
		if hostname, err = os.Hostname(); err != nil {
			return nil, fmt.Errorf("failed to get defaultPayloadCustomizer: %w", err)
		}
	}

	payloadCustomizer := func(payload types.Payload) types.Payload {
		// Create a copy of the payload
		newPayload := make(types.Payload, len(payload))
		for k, v := range payload {
			newPayload[k] = v
		}

		// Remove the unnecessary version field from the payload.
		// The KIC version is not required.
		delete(newPayload, "v")

		// Insert the hostname field. This acts as a foreign key to link KIC reports with KO reports.
		newPayload[provider.HostnameKey] = hostname

		return newPayload
	}

	return payloadCustomizer, nil
}
