package policychain

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// OwnerAnnotation records a parent-scoped claim, not a remote programming result.
const OwnerAnnotation = "gateway-operator.konghq.com/cluster-policy-chain-owner"

// ListenerOwnerAnnotation records a listener-scoped claim independently of cluster chains.
const ListenerOwnerAnnotation = "gateway-operator.konghq.com/listener-policy-chain-owner"

// ProduceOwnerAnnotation records a virtual-cluster produce claim independently of other chains.
const ProduceOwnerAnnotation = "gateway-operator.konghq.com/produce-policy-chain-owner"

// ConsumeOwnerAnnotation records a virtual-cluster consume claim independently of other chains.
const ConsumeOwnerAnnotation = "gateway-operator.konghq.com/consume-policy-chain-owner"

// Chain declares singleton configuration on a namespaced parent.
type Chain interface {
	client.Object
	GetParentRef() commonv1alpha1.ObjectRef
}

// Scope returns the parent, owner declaration type and annotation for a chain.
func Scope(chain Chain) (parent client.Object, owner Chain, annotation string, err error) {
	switch chain.(type) {
	case *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain:
		return &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}, OwnerAnnotation, nil
	case *configurationv1alpha1.EventGatewayListenerPolicyChain:
		return &configurationv1alpha1.EventGatewayListener{}, &configurationv1alpha1.EventGatewayListenerPolicyChain{}, ListenerOwnerAnnotation, nil
	case *configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain:
		return &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{}, ProduceOwnerAnnotation, nil
	case *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain:
		return &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{}, ConsumeOwnerAnnotation, nil
	default:
		return nil, nil, "", fmt.Errorf("unsupported policy chain type %T", chain)
	}
}

// kinds returns the kinds of the chain and of its parent (see Scope), which a
// KongReferenceGrant names to let a chain of another namespace reference the
// parent: those of their Go types.
func kinds(chain Chain) (chainKind, parentKind string, err error) {
	parent, _, _, err := Scope(chain)
	if err != nil {
		return "", "", err
	}
	return reflect.TypeOf(chain).Elem().Name(), reflect.TypeOf(parent).Elem().Name(), nil
}

// ParentKey returns the key of the parent the chain references, in the
// namespace of the chain unless the reference sets another one, and false
// when it references none by name.
func ParentKey(chain Chain) (client.ObjectKey, bool) {
	parentRef := chain.GetParentRef()
	ref := parentRef.NamespacedRef
	if parentRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref == nil || ref.Name == "" {
		return client.ObjectKey{}, false
	}
	key := client.ObjectKey{Namespace: chain.GetNamespace(), Name: ref.Name}
	if ref.Namespace != nil && *ref.Namespace != "" {
		key.Namespace = *ref.Namespace
	}
	return key, true
}

// ClaimParentKey returns the key of the parent the chain may claim, or a
// PolicyChainInvalidError: the chain must reference its parent by name.
// Whether a KongReferenceGrant lets a chain of another namespace claim it is
// up to the caller.
func ClaimParentKey(chain Chain) (client.ObjectKey, error) {
	key, ok := ParentKey(chain)
	if !ok {
		return client.ObjectKey{}, configurationv1alpha1.PolicyChainInvalidError{Message: "policy-chain ownership requires a parent reference"}
	}
	return key, nil
}

// ClaimValue identifies a particular incarnation of a chain, on the parent it
// references: name/uid for a chain in the namespace of the parent, as claims
// were before cross-namespace references, or namespace/name/uid for a chain
// in another one.
func ClaimValue[T Chain](chain T) string {
	if parent, ok := ParentKey(chain); ok && parent.Namespace != chain.GetNamespace() {
		return chain.GetNamespace() + "/" + chain.GetName() + "/" + string(chain.GetUID())
	}
	return chain.GetName() + "/" + string(chain.GetUID())
}

// ParseClaim returns the key and the UID of the chain a claim on a parent in
// parentNamespace names, and false when it's malformed.
func ParseClaim(claim, parentNamespace string) (client.ObjectKey, string, bool) {
	validUID := func(uid string) bool { return uid != "" && !strings.ContainsAny(uid, "/ \t\n") }
	validName := func(name string) bool { return name != "" && len(validation.IsDNS1123Subdomain(name)) == 0 }
	parts := strings.Split(claim, "/")
	switch {
	case len(parts) == 2 && validName(parts[0]) && validUID(parts[1]):
		return client.ObjectKey{Namespace: parentNamespace, Name: parts[0]}, parts[1], true
	case len(parts) == 3 && len(validation.IsDNS1123Label(parts[0])) == 0 && validName(parts[1]) && validUID(parts[2]):
		return client.ObjectKey{Namespace: parts[0], Name: parts[1]}, parts[2], true
	default:
		return client.ObjectKey{}, "", false
	}
}

// NameIn returns how a message about an object of namespace names the object
// of key: its name, or namespace/name for an object of another namespace.
func NameIn(key client.ObjectKey, namespace string) string {
	if key.Namespace == namespace {
		return key.Name
	}
	return key.String()
}

// Holder returns the chain holding a claim on the parent of parentKey, read
// into owner, a chain of the kind of the claimant, or nil when the claim is
// stale, and replaceable: malformed, or the chain it names no longer exists,
// is another incarnation, references another parent, or is of another
// namespace and no longer allowed, by a KongReferenceGrant, to reference the
// parent.
func Holder(ctx context.Context, cl client.Client, owner Chain, parentKey client.ObjectKey, claim string) (Chain, error) {
	ownerKey, uid, valid := ParseClaim(claim, parentKey.Namespace)
	if !valid {
		return nil, nil
	}
	if err := cl.Get(ctx, ownerKey, owner); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading policy-chain owner %s: %w", ownerKey, err)
	}
	if string(owner.GetUID()) != uid {
		return nil, nil
	}
	if key, ok := ParentKey(owner); !ok || key != parentKey {
		return nil, nil
	}
	if ownerKey.Namespace != parentKey.Namespace {
		chainKind, parentKind, err := kinds(owner)
		if err != nil {
			return nil, err
		}
		err = crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, ownerKey.Namespace, parentKey.Namespace, parentKey.Name,
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(chainKind)),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind(parentKind)))
		if crossnamespace.IsReferenceNotGranted(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return owner, nil
}

// Claim atomically claims the parent, or returns the name of its existing
// owner (see NameIn). A stale claim is replaceable (see Holder). Whether a
// KongReferenceGrant lets the chain, of another namespace, reference the
// parent is up to the caller.
func Claim(ctx context.Context, cl client.Client, chain Chain) (string, error) {
	parent, existing, annotation, err := Scope(chain)
	if err != nil {
		return "", err
	}
	key, err := ClaimParentKey(chain)
	if err != nil {
		return "", err
	}
	if chain.GetUID() == "" {
		return "", fmt.Errorf("policy chain %s has no Kubernetes UID", client.ObjectKeyFromObject(chain))
	}
	if err := cl.Get(ctx, key, parent); err != nil {
		return "", err
	}
	claim := ClaimValue(chain)
	owner := parent.GetAnnotations()[annotation]
	if owner != "" && owner != claim {
		holder, err := Holder(ctx, cl, existing, key, owner)
		if err != nil {
			return "", err
		}
		if holder != nil {
			return NameIn(client.ObjectKeyFromObject(holder), key.Namespace), nil
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
