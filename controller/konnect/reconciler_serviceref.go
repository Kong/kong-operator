package konnect

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// handleKongServiceRef handles the ServiceRef for the given entity.
// It sets the owner reference to the referenced KongService and updates the
// status of the entity based on the referenced KongService status.
func handleKongServiceRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	kongServiceRef, ok := getServiceRef(ent).Get()
	if !ok {
		if kongRoute, ok := any(ent).(*configurationv1alpha1.KongRoute); ok {
			// If the entity has a resolved reference, but the spec has changed, we need to adjust the status
			// and transfer the ownership back from the KongService to the ControlPlane.
			if kongRoute.Status.Konnect != nil && kongRoute.Status.Konnect.ServiceID != "" {
				old := kongRoute.DeepCopyObject().(TEnt)
				// Reset the KeySetID in the status and set the condition to True.
				kongRoute.Status.Konnect.ServiceID = ""
				_ = patch.SetStatusWithConditionIfDifferent(ent,
					konnectv1alpha1.KongServiceRefValidConditionType,
					metav1.ConditionTrue,
					konnectv1alpha1.KeySetRefReasonValid,
					"ServiceRef is unset",
				)

				// Patch the status
				if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to patch status: %w", err)
				}

				// Check if the entity has a ControlPlaneRef as not having it as well as not having
				// a ServiceRef is an error.
				_, hasCPRef := controlplane.GetControlPlaneRef(ent).Get()
				if !hasCPRef {
					return ctrl.Result{}, fmt.Errorf("key doesn't have neither a KongService ref not a ControlPlane ref")
				}
			}
		}
		return ctrl.Result{}, nil
	}

	if kongServiceRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
		ctrllog.FromContext(ctx).Error(fmt.Errorf("unsupported KongService ref type %q", kongServiceRef.Type), "unsupported KongService ref type", "entity", ent)
		return ctrl.Result{}, nil
	}

	kongSvc := configurationv1alpha1.KongService{}
	nn := types.NamespacedName{
		Name: kongServiceRef.NamespacedRef.Name,
		// TODO: handle cross namespace refs
		Namespace: ent.GetNamespace(),
	}
	if err := cl.Get(ctx, nn, &kongSvc); err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongServiceRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongServiceRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		// If the KongService is not found, we don't want to requeue.
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, ReferencedObjectDoesNotExist{
				Reference: nn,
				Err:       err,
			}
		}

		return ctrl.Result{}, fmt.Errorf("can't get the referenced KongService %s: %w", nn, err)
	}

	old := ent.DeepCopyObject().(TEnt)

	// If referenced KongService is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := kongSvc.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.KongServiceRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongServiceRefReasonInvalid,
			fmt.Sprintf("Referenced KongService %s is being deleted", nn),
		)
		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, ReferencedKongServiceIsBeingDeleted{
			Reference: nn,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &kongSvc)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.KongServiceRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongServiceRefReasonInvalid,
			fmt.Sprintf("Referenced KongService %s is not programmed yet", nn),
		)

		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// TODO(pmalek): make this generic.
	// Service ID is not stored in KonnectEntityStatus because not all entities
	// have a ServiceRef, hence the type constraints in the reconciler can't be used.
	if route, ok := any(ent).(*configurationv1alpha1.KongRoute); ok {
		if route.Status.Konnect == nil {
			route.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{}
		}
		route.Status.Konnect.ServiceID = kongSvc.Status.Konnect.GetKonnectID()
	}

	_ = patch.SetStatusWithConditionIfDifferent(ent,
		konnectv1alpha1.KongServiceRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongServiceRefReasonValid,
		fmt.Sprintf("Referenced KongService %s programmed", nn),
	)

	_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
	if err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	kongSvcCPRef, ok := controlplane.GetControlPlaneRef(&kongSvc).Get()
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"KongRoute references a KongService %s which does not have a ControlPlane ref",
			client.ObjectKeyFromObject(&kongSvc),
		)
	}
	cp, err := controlplane.GetCPForRef(ctx, cl, kongSvcCPRef, ent.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, controlplane.ReferencedControlPlaneDoesNotExistError{
				Reference: kongSvcCPRef,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cond, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// TODO(pmalek): make this generic.
	// CP ID is not stored in KonnectEntityStatus because not all entities
	// have a ControlPlaneRef, hence the type constraints in the reconciler can't be used.
	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		resource.SetControlPlaneID(cp.Status.ID)
	}

	return ctrl.Result{}, nil
}

func getServiceRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.ServiceRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongRoute:
		if e.Spec.ServiceRef == nil {
			return mo.None[configurationv1alpha1.ServiceRef]()
		}
		return mo.Some(*e.Spec.ServiceRef)
	default:
		return mo.None[configurationv1alpha1.ServiceRef]()
	}
}
