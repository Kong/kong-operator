package konnect

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

func getConsumerRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[corev1.LocalObjectReference] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongCredentialBasicAuth:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialAPIKey:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialACL:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialJWT:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialHMAC:
		return mo.Some(e.Spec.ConsumerRef)
	default:
		return mo.None[corev1.LocalObjectReference]()
	}
}

// handleKongConsumerRef handles the ConsumerRef for the given entity.
// It sets the owner reference to the referenced KongConsumer and updates the
// status of the entity based on the referenced KongConsumer status.
func handleKongConsumerRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	kongConsumerRef, ok := getConsumerRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}
	consumer := configurationv1.KongConsumer{}
	nn := types.NamespacedName{
		Name:      kongConsumerRef.Name,
		Namespace: ent.GetNamespace(),
	}

	if err := cl.Get(ctx, nn, &consumer); err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongConsumerRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongConsumerRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongConsumerDoesNotExist{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongConsumer is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := consumer.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongConsumerIsBeingDeleted{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &consumer)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongConsumerRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongConsumerRefReasonInvalid,
			fmt.Sprintf("Referenced KongConsumer %s is not programmed yet", nn),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	type EntityWithConsumerRef interface {
		SetKonnectConsumerIDInStatus(string)
	}
	if cred, ok := any(ent).(EntityWithConsumerRef); ok {
		cred.SetKonnectConsumerIDInStatus(consumer.Status.Konnect.GetKonnectID())
	} else {
		return ctrl.Result{}, fmt.Errorf(
			"cannot set referenced Consumer %s KonnectID in %s %sstatus",
			client.ObjectKeyFromObject(&consumer), constraints.EntityTypeName[T](), client.ObjectKeyFromObject(ent),
		)
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KongConsumerRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongConsumerRefReasonValid,
		fmt.Sprintf("Referenced KongConsumer %s programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	cpRef, ok := controlplane.GetControlPlaneRef(&consumer).Get()
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"KongRoute references a KongConsumer %s which does not have a ControlPlane ref",
			client.ObjectKeyFromObject(&consumer),
		)
	}
	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, ent.GetNamespace())
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
				Reference: cpRef,
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

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		old := ent.DeepCopyObject().(TEnt)
		resource.SetControlPlaneID(cp.Status.ID)
		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ControlPlaneRefReasonValid,
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
