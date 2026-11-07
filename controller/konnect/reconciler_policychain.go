package konnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const (
	policyChainOwnerAnnotation         = "gateway-operator.konghq.com/cluster-policy-chain-owner"
	listenerPolicyChainOwnerAnnotation = "gateway-operator.konghq.com/listener-policy-chain-owner"
)

type policyChain interface {
	k8sutils.ConditionsAwareObject
	GetParentRef() commonv1alpha1.ObjectRef
	GetParentGVK() schema.GroupVersionKind
	ResolveValidatedPolicyIDs(context.Context, client.Client) ([]string, error)
}

type policyChainLiveGetClient struct {
	client.Client

	apiReader client.Reader
}

// Get observes live owners and parents so cache lag cannot turn an active claim
// into a stale claim that another declaration is allowed to replace.
func (c policyChainLiveGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.apiReader.Get(ctx, key, obj, opts...)
}

type policyChainReferenceResolver struct {
	chain     policyChain
	policyIDs []string
}

// ResolveKonnectReferences validates references and captures their ordered IDs for this reconciliation.
func (r *policyChainReferenceResolver) ResolveKonnectReferences(ctx context.Context, cl client.Client) error {
	var err error
	r.policyIDs, err = r.chain.ResolveValidatedPolicyIDs(ctx, cl)
	return err
}

func policyChainOwnerClaim(chain client.Object) string {
	return chain.GetName() + "/" + string(chain.GetUID())
}

func policyChainScope(chain policyChain) (parent client.Object, owner client.Object, annotation string, err error) {
	switch chain.(type) {
	case *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain:
		return &configurationv1alpha1.EventGatewayVirtualCluster{}, &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{}, policyChainOwnerAnnotation, nil
	case *configurationv1alpha1.EventGatewayListenerPolicyChain:
		return &configurationv1alpha1.EventGatewayListener{}, &configurationv1alpha1.EventGatewayListenerPolicyChain{}, listenerPolicyChainOwnerAnnotation, nil
	default:
		return nil, nil, "", fmt.Errorf("unsupported policy chain type %T", chain)
	}
}

func finalizePolicyChain(ctx context.Context, cl client.Client, chain policyChain) (ctrl.Result, error) {
	if chain.GetDeletionTimestamp().After(time.Now()) {
		return ctrl.Result{RequeueAfter: time.Until(chain.GetDeletionTimestamp().Time)}, nil
	}
	parent, _, annotation, err := policyChainScope(chain)
	if err != nil {
		return ctrl.Result{}, err
	}
	ref := chain.GetParentRef()
	// Invalid parent references cannot claim ownership, but can acquire the
	// generic cleanup finalizer before validation rejects them.
	if ref.Type == commonv1alpha1.ObjectRefTypeNamespacedRef && ref.NamespacedRef != nil && ref.NamespacedRef.Name != "" &&
		(ref.NamespacedRef.Namespace == nil || *ref.NamespacedRef.Namespace == "" || *ref.NamespacedRef.Namespace == chain.GetNamespace()) {
		if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.GetNamespace(), Name: ref.NamespacedRef.Name}, parent); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("getting %s to release policy chain ownership: %w", chain.GetParentGVK().Kind, err)
			}
		} else if parent.GetAnnotations()[annotation] == policyChainOwnerClaim(chain) {
			old := parent.DeepCopyObject().(client.Object)
			delete(parent.GetAnnotations(), annotation)
			if err := cl.Patch(ctx, parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, fmt.Errorf("releasing policy chain ownership: %w", err)
				}
			}
		}
	}
	old := chain.DeepCopyObject().(client.Object)
	if controllerutil.RemoveFinalizer(chain, KonnectCleanupFinalizer) {
		if err := cl.Patch(ctx, chain, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("removing retained policy chain finalizer: %w", err)
			}
		}
	}
	return ctrl.Result{}, nil
}

// claimPolicyChain serializes ownership on the parent with an optimistic-lock
// patch. A cached list of chains alone cannot prevent simultaneous claimants.
func claimPolicyChain(ctx context.Context, cl client.Client, chain policyChain, syncPeriod time.Duration) (bool, ctrl.Result, error) {
	parent, existing, annotation, err := policyChainScope(chain)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	parentKind := chain.GetParentGVK().Kind
	parentRef := chain.GetParentRef()
	ref := parentRef.NamespacedRef
	if parentRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref == nil || ref.Name == "" ||
		(ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.GetNamespace()) {
		return false, ctrl.Result{}, configurationv1alpha1.PolicyChainInvalidError{Message: "policy-chain ownership requires a same-namespace " + parentKind + " reference"}
	}
	if chain.GetUID() == "" {
		return false, ctrl.Result{}, fmt.Errorf("policy chain %s has no Kubernetes UID", client.ObjectKeyFromObject(chain))
	}
	current := existing.DeepCopyObject().(client.Object)
	if err := cl.Get(ctx, client.ObjectKeyFromObject(chain), current); err != nil {
		if apierrors.IsNotFound(err) {
			return false, ctrl.Result{}, nil
		}
		return false, ctrl.Result{}, fmt.Errorf("getting current policy chain before claiming ownership: %w", err)
	}
	if current.GetUID() != chain.GetUID() || current.GetGeneration() != chain.GetGeneration() || !current.GetDeletionTimestamp().IsZero() {
		return false, ctrl.Result{Requeue: true}, nil
	}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.GetNamespace(), Name: ref.Name}, parent); err != nil {
		return false, ctrl.Result{}, err
	}
	claim := policyChainOwnerClaim(chain)
	owner := parent.GetAnnotations()[annotation]
	if owner != "" && owner != claim {
		name, uid, valid := strings.Cut(owner, "/")
		claimed := false
		if valid && name != "" && len(validation.IsDNS1123Subdomain(name)) == 0 && uid != "" && !strings.ContainsAny(uid, "/ \t\n") {
			err := cl.Get(ctx, client.ObjectKey{Namespace: chain.GetNamespace(), Name: name}, existing)
			if err != nil && !apierrors.IsNotFound(err) {
				return false, ctrl.Result{}, err
			}
			claimed = err == nil && string(existing.GetUID()) == uid
			if existingChain, ok := existing.(policyChain); claimed && ok {
				claimed = !commonv1alpha1.ObjectRefsDiffer(chain.GetParentRef(), existingChain.GetParentRef(), chain.GetNamespace(), existingChain.GetNamespace())
			}
		}
		if claimed {
			res, err := patch.StatusWithCondition(ctx, cl, chain,
				apiconsts.ConditionType("PolicyChainOwnerValid"), metav1.ConditionFalse,
				apiconsts.ConditionReason("ConflictingChain"),
				fmt.Sprintf("%s %s is owned by policy chain %s; delete that chain before claiming it", parentKind, ref.Name, name))
			if err != nil || !res.IsZero() {
				return false, res, err
			}
			res, err = patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, cl, chain)
			if err != nil || !res.IsZero() {
				return false, res, err
			}
			return false, ctrl.Result{RequeueAfter: syncPeriod}, nil
		}
	}
	if owner != claim {
		old := parent.DeepCopyObject().(client.Object)
		if parent.GetAnnotations() == nil {
			parent.SetAnnotations(make(map[string]string))
		}
		parent.GetAnnotations()[annotation] = claim
		if err := cl.Patch(ctx, parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) {
				return false, ctrl.Result{Requeue: true}, nil
			}
			return false, ctrl.Result{}, fmt.Errorf("claiming policy chain on %s: %w", parentKind, err)
		}
	}
	res, err := patch.StatusWithCondition(ctx, cl, chain,
		apiconsts.ConditionType("PolicyChainOwnerValid"), metav1.ConditionTrue,
		apiconsts.ConditionReason("Owner"), "This resource owns the "+parentKind+" policy chain")
	return err == nil && res.IsZero(), res, err
}
