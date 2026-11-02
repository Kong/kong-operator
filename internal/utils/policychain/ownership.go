package policychain

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// OwnerAnnotation records a parent-scoped claim, not a remote programming result.
const OwnerAnnotation = "gateway-operator.konghq.com/cluster-policy-chain-owner"

// ClaimValue identifies a particular incarnation of a chain.
func ClaimValue(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
	return chain.Name + "/" + string(chain.UID)
}

// Claim atomically claims the parent, or returns the name of its existing owner.
// Malformed claims and claims whose object no longer exists are replaceable.
func Claim(ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) (string, error) {
	parentRef := chain.Spec.EventGatewayVirtualClusterRef
	ref := parentRef.NamespacedRef
	if parentRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref == nil || ref.Name == "" ||
		(ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.Namespace) {
		return "", configurationv1alpha1.PolicyChainInvalidError{Message: "policy-chain ownership requires a same-namespace EventGatewayVirtualCluster reference"}
	}
	if chain.UID == "" {
		return "", fmt.Errorf("policy chain %s has no Kubernetes UID", client.ObjectKeyFromObject(chain))
	}
	var parent configurationv1alpha1.EventGatewayVirtualCluster
	if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.Namespace, Name: ref.Name}, &parent); err != nil {
		return "", err
	}
	claim := ClaimValue(chain)
	owner := parent.Annotations[OwnerAnnotation]
	if owner != "" && owner != claim {
		name, uid, valid := strings.Cut(owner, "/")
		if valid && name != "" && uid != "" {
			var existing configurationv1alpha1.EventGatewayVirtualClusterPolicyChain
			err := cl.Get(ctx, client.ObjectKey{Namespace: chain.Namespace, Name: name}, &existing)
			if err != nil && !apierrors.IsNotFound(err) {
				return "", err
			}
			if err == nil && string(existing.UID) == uid {
				return name, nil
			}
		}
	}
	if owner != claim {
		old := parent.DeepCopy()
		if parent.Annotations == nil {
			parent.Annotations = make(map[string]string)
		}
		parent.Annotations[OwnerAnnotation] = claim
		if err := cl.Patch(ctx, &parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
			return "", fmt.Errorf("claiming policy chain on virtual cluster: %w", err)
		}
	}
	return "", nil
}
