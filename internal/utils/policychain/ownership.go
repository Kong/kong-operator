package policychain

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// OwnerAnnotation records a parent-scoped claim, not a remote programming result.
const OwnerAnnotation = "gateway-operator.konghq.com/cluster-policy-chain-owner"

// ListenerOwnerAnnotation records a listener-scoped claim independently of cluster chains.
const ListenerOwnerAnnotation = "gateway-operator.konghq.com/listener-policy-chain-owner"

// Chain declares singleton configuration on a namespaced parent.
type Chain interface {
	client.Object
	GetParentRef() commonv1alpha1.ObjectRef
}

// ClaimValue identifies a particular incarnation of a chain.
func ClaimValue(chain client.Object) string {
	return chain.GetName() + "/" + string(chain.GetUID())
}

// Scope returns the parent, owner declaration type and annotation for a chain.
func Scope(chain Chain) (parent client.Object, owner Chain, annotation string, err error) {
	switch chain.(type) {
	case *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain:
		return &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}, OwnerAnnotation, nil
	case *configurationv1alpha1.EventGatewayListenerPolicyChain:
		return &configurationv1alpha1.EventGatewayListener{}, &configurationv1alpha1.EventGatewayListenerPolicyChain{}, ListenerOwnerAnnotation, nil
	default:
		return nil, nil, "", fmt.Errorf("unsupported policy chain type %T", chain)
	}
}

// Claim atomically claims the parent, or returns the name of its existing owner.
// Malformed claims and claims whose object no longer exists are replaceable.
func Claim(ctx context.Context, cl client.Client, chain Chain) (string, error) {
	parent, existing, annotation, err := Scope(chain)
	if err != nil {
		return "", err
	}
	parentRef := chain.GetParentRef()
	ref := parentRef.NamespacedRef
	if parentRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref == nil || ref.Name == "" ||
		(ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.GetNamespace()) {
		return "", configurationv1alpha1.PolicyChainInvalidError{Message: "policy-chain ownership requires a same-namespace parent reference"}
	}
	if chain.GetUID() == "" {
		return "", fmt.Errorf("policy chain %s has no Kubernetes UID", client.ObjectKeyFromObject(chain))
	}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.GetNamespace(), Name: ref.Name}, parent); err != nil {
		return "", err
	}
	claim := ClaimValue(chain)
	owner := parent.GetAnnotations()[annotation]
	if owner != "" && owner != claim {
		name, uid, valid := strings.Cut(owner, "/")
		if valid && name != "" && len(validation.IsDNS1123Subdomain(name)) == 0 && uid != "" && !strings.ContainsAny(uid, "/ \t\n") {
			err := cl.Get(ctx, client.ObjectKey{Namespace: chain.GetNamespace(), Name: name}, existing)
			if err != nil && !apierrors.IsNotFound(err) {
				return "", err
			}
			if err == nil && string(existing.GetUID()) == uid &&
				!commonv1alpha1.ObjectRefsDiffer(chain.GetParentRef(), existing.GetParentRef(), chain.GetNamespace(), existing.GetNamespace()) {
				return name, nil
			}
		}
	}
	if owner != claim {
		old := parent.DeepCopyObject().(client.Object)
		if parent.GetAnnotations() == nil {
			parent.SetAnnotations(make(map[string]string))
		}
		parent.GetAnnotations()[annotation] = claim
		if err := cl.Patch(ctx, parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
			return "", fmt.Errorf("claiming policy chain on parent: %w", err)
		}
	}
	return "", nil
}
