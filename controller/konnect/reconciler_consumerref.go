package konnect

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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

	if ok, err := controllerutil.HasOwnerReference(consumer.OwnerReferences, ent, cl.Scheme()); err != nil {
		ctrllog.FromContext(ctx).Info("failed to check if KongConsumer has owner reference", "error", err)
	} else if ok {
		old := ent.DeepCopyObject().(TEnt)
		if err := controllerutil.RemoveOwnerReference(&consumer, ent, cl.Scheme()); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete owner reference: %w", err)
		}
		if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}
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

	cpRef, ok := getControlPlaneRef(&consumer).Get()
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"KongRoute references a KongConsumer %s which does not have a ControlPlane ref",
			client.ObjectKeyFromObject(&consumer),
		)
	}
	cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
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
			return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
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

func objectHasDeletedKongConsumerOwner(
	obj client.Object,
	scheme *runtime.Scheme,
	err error,
) (bool, error) {
	var (
		nn                types.NamespacedName
		errDoesNotExist   ReferencedKongConsumerDoesNotExist
		errIsBeingDeleted ReferencedKongConsumerIsBeingDeleted
	)

	switch {
	case errors.As(err, &errDoesNotExist):
		nn = errDoesNotExist.Reference
	case errors.As(err, &errIsBeingDeleted):
		nn = errIsBeingDeleted.Reference
	default:
		return false, nil
	}

	c := configurationv1.KongConsumer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongConsumer",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
	}
	ok, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), &c, scheme)
	if err != nil {
		return false, err
	}
	return ok, nil
}
