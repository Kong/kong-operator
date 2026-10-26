package konnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	apiconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
)

const policyChainOwnerAnnotation = "gateway-operator.konghq.com/cluster-policy-chain-owner"

type policyChainReferenceResolver struct {
	*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain

	policyIDs []string
}

// ResolveKonnectReferences validates references and captures their ordered IDs for this reconciliation.
func (r *policyChainReferenceResolver) ResolveKonnectReferences(ctx context.Context, cl client.Client) error {
	var err error
	r.policyIDs, err = r.ResolveValidatedPolicyIDs(ctx, cl)
	return err
}

func policyChainOwnerClaim(chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) string {
	return chain.Name + "/" + string(chain.UID)
}

func finalizePolicyChain(ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) (ctrl.Result, error) {
	if chain.DeletionTimestamp.After(time.Now()) {
		return ctrl.Result{RequeueAfter: time.Until(chain.DeletionTimestamp.Time)}, nil
	}
	ref := chain.Spec.EventGatewayVirtualClusterRef
	// Invalid parent references cannot claim ownership, but can acquire the
	// generic cleanup finalizer before validation rejects them.
	if ref.Type == commonv1alpha1.ObjectRefTypeNamespacedRef && ref.NamespacedRef != nil && ref.NamespacedRef.Name != "" &&
		(ref.NamespacedRef.Namespace == nil || *ref.NamespacedRef.Namespace == "" || *ref.NamespacedRef.Namespace == chain.Namespace) {
		var parent configurationv1alpha1.EventGatewayVirtualCluster
		if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.Namespace, Name: ref.NamespacedRef.Name}, &parent); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("getting virtual cluster to release policy chain ownership: %w", err)
			}
		} else if parent.Annotations[policyChainOwnerAnnotation] == policyChainOwnerClaim(chain) {
			old := parent.DeepCopy()
			delete(parent.Annotations, policyChainOwnerAnnotation)
			if err := cl.Patch(ctx, &parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, fmt.Errorf("releasing policy chain ownership: %w", err)
				}
			}
		}
	}
	old := chain.DeepCopy()
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
func claimPolicyChain(ctx context.Context, cl client.Client, chain *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain, syncPeriod time.Duration) (bool, ctrl.Result, error) {
	ref := chain.Spec.EventGatewayVirtualClusterRef.NamespacedRef
	if ref == nil || ref.Name == "" || (ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != chain.Namespace) {
		return false, ctrl.Result{}, configurationv1alpha1.PolicyChainInvalidError{Message: "policy-chain ownership requires a same-namespace EventGatewayVirtualCluster reference"}
	}
	if chain.UID == "" {
		return false, ctrl.Result{}, fmt.Errorf("policy chain %s has no Kubernetes UID", client.ObjectKeyFromObject(chain))
	}
	var parent configurationv1alpha1.EventGatewayVirtualCluster
	if err := cl.Get(ctx, client.ObjectKey{Namespace: chain.Namespace, Name: ref.Name}, &parent); err != nil {
		return false, ctrl.Result{}, err
	}
	claim := policyChainOwnerClaim(chain)
	owner := parent.Annotations[policyChainOwnerAnnotation]
	if owner != "" && owner != claim {
		name, uid, valid := strings.Cut(owner, "/")
		var existing configurationv1alpha1.EventGatewayVirtualClusterPolicyChain
		claimed := false
		if valid && name != "" && uid != "" {
			err := cl.Get(ctx, client.ObjectKey{Namespace: chain.Namespace, Name: name}, &existing)
			if err != nil && !apierrors.IsNotFound(err) {
				return false, ctrl.Result{}, err
			}
			claimed = err == nil && string(existing.UID) == uid
		}
		if claimed {
			res, err := patch.StatusWithCondition(ctx, cl, chain,
				apiconsts.ConditionType("PolicyChainOwnerValid"), metav1.ConditionFalse,
				apiconsts.ConditionReason("ConflictingChain"),
				fmt.Sprintf("EventGatewayVirtualCluster %s is owned by policy chain %s; delete that chain before claiming it", ref.Name, name))
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
		old := parent.DeepCopy()
		if parent.Annotations == nil {
			parent.Annotations = make(map[string]string)
		}
		parent.Annotations[policyChainOwnerAnnotation] = claim
		if err := cl.Patch(ctx, &parent, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) {
				return false, ctrl.Result{Requeue: true}, nil
			}
			return false, ctrl.Result{}, fmt.Errorf("claiming policy chain on virtual cluster: %w", err)
		}
	}
	res, err := patch.StatusWithCondition(ctx, cl, chain,
		apiconsts.ConditionType("PolicyChainOwnerValid"), metav1.ConditionTrue,
		apiconsts.ConditionReason("Owner"), "This resource owns the parent cluster policy chain")
	return err == nil && res.IsZero(), res, err
}
