package ops

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// Type definitions shared across adoption implementations.

type normalizedAutoscale struct {
	Type      string               `json:"type"`
	Static    *normalizedStatic    `json:"static,omitempty"`
	Autopilot *normalizedAutopilot `json:"autopilot,omitempty"`
}

type normalizedStatic struct {
	InstanceType       string `json:"instanceType"`
	RequestedInstances int64  `json:"requestedInstances"`
}

type normalizedAutopilot struct {
	BaseRps int64  `json:"baseRps"`
	MaxRps  *int64 `json:"maxRps,omitempty"`
}

type normalizedEnvironment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type normalizedDPGroup struct {
	Provider    string                  `json:"provider"`
	Region      string                  `json:"region"`
	NetworkID   string                  `json:"networkID"`
	Autoscale   normalizedAutoscale     `json:"autoscale"`
	Environment []normalizedEnvironment `json:"environment,omitempty"`
}

type normalizedDNSConfig struct {
	RemoteDNSServerIPAddresses []string `json:"remote_dns_server_ip_addresses,omitempty"`
	DomainProxyList            []string `json:"domain_proxy_list,omitempty"`
}

// resolveNetworkRef resolves a network reference to a Konnect Network ID.
// This is used by DataPlaneGroupConfiguration to resolve network references.
func resolveNetworkRef(
	ctx context.Context,
	cl client.Client,
	namespace string,
	ref commonv1alpha1.ObjectRef,
) (string, error) {
	switch ref.Type {
	case commonv1alpha1.ObjectRefTypeKonnectID:
		return *ref.KonnectID, nil
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		if ref.NamespacedRef == nil {
			return "", fmt.Errorf("namespacedRef must be provided for networkRef")
		}
		var network konnectv1alpha1.KonnectCloudGatewayNetwork
		key := types.NamespacedName{Namespace: namespace, Name: ref.NamespacedRef.Name}
		if err := cl.Get(ctx, key, &network); err != nil {
			return "", fmt.Errorf("failed to get KonnectCloudGatewayNetwork %s: %w", key, err)
		}
		if network.GetKonnectID() == "" {
			return "", fmt.Errorf("KonnectCloudGatewayNetwork %s does not have a Konnect ID", key)
		}
		return network.GetKonnectID(), nil
	default:
		return "", fmt.Errorf("unsupported network ref type %q", ref.Type)
	}
}
