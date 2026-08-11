package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

// generateHashedName builds a Kubernetes-safe NamespacedName from a namespace,
// a name prefix, and a hash key. The key is SHA-256 hashed and truncated to
// 8 hex characters. The total name is capped at 63 characters (DNS label limit),
// with the 8-char hash suffix always preserved; the prefix is truncated if needed.
func generateHashedName(namespace, prefix, hashKey string) types.NamespacedName {
	const maxLen = 63

	hash := sha256.Sum256([]byte(hashKey))
	shortHash := hex.EncodeToString(hash[:])[:8]
	suffix := "-" + shortHash // 9 chars

	maxPrefix := maxLen - len(suffix)
	if len(prefix) > maxPrefix {
		prefix = prefix[:maxPrefix]
	}
	prefix = strings.TrimRight(prefix, "-")

	return types.NamespacedName{
		Namespace: namespace,
		Name:      prefix + suffix,
	}
}

// ownerControlPlaneName returns the name of the KonnectGatewayControlPlane that
// owns the given MCPServer, or an empty string if no such owner is found.
func ownerControlPlaneName(mcpServer *konnectv1alpha1.MCPServer) string {
	for _, ref := range mcpServer.OwnerReferences {
		if ref.APIVersion == konnectv1alpha2.GroupVersion.String() && ref.Kind == "KonnectGatewayControlPlane" {
			return ref.Name
		}
	}
	return ""
}
