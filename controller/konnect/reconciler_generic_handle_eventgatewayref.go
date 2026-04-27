package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// handleEventGatewayRef handles the GatewayRef for KonnectEventDataPlaneCertificate.
func handleEventGatewayRef(
	ctx context.Context,
	cl client.Client,
	obj k8sutils.ConditionsAwareObject,
) (ctrl.Result, error) {
	cert, ok := any(obj).(*konnectv1alpha1.KonnectEventDataPlaneCertificate)
	if !ok {
		return ctrl.Result{}, &UnsupportedGeneratedReferenceTypeError{
			TypeName: fmt.Sprintf("%T", obj),
		}
	}

	if res, err := ensureKongReferenceGrantForEventGatewayRef(ctx, cl, cert); err != nil || !res.IsZero() {
		return res, err
	}

	gateway, nn, err := getEventGatewayForRef(ctx, cl, cert.Spec.GatewayRef, cert.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, cert,
			konnectv1alpha1.EventGatewayRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.EventGatewayRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	if delTimestamp := gateway.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		msg := fmt.Sprintf("Referenced KonnectEventGateway %s is being deleted", nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, cert,
			konnectv1alpha1.EventGatewayRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.EventGatewayRefReasonInvalid,
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedObjectIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, gateway)
	if !ok || cond.Status != metav1.ConditionTrue {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, cert,
			konnectv1alpha1.EventGatewayRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.EventGatewayRefReasonNotProgrammed,
			fmt.Sprintf("Referenced KonnectEventGateway %s is not programmed yet", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if gateway.GetKonnectID() == "" {
		err := ReferencedObjectIsInvalidError{
			Reference: nn.String(),
			Msg:       "Referenced KonnectEventGateway does not have a Konnect ID yet",
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, cert,
			konnectv1alpha1.EventGatewayRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.EventGatewayRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	old := cert.DeepCopy()
	if cert.Status.GatewayID == nil {
		cert.Status.GatewayID = &konnectv1alpha1.KonnectEntityRef{}
	}
	cert.Status.GatewayID.ID = gateway.GetKonnectID()
	_, err = patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), cert, old)
	if err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, cert,
		konnectv1alpha1.EventGatewayRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.EventGatewayRefReasonValid,
		fmt.Sprintf("Referenced KonnectEventGateway %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

func ensureKongReferenceGrantForEventGatewayRef(
	ctx context.Context,
	cl client.Client,
	ent *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (ctrl.Result, error) {
	ref := ent.Spec.GatewayRef
	if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		ref.NamespacedRef == nil ||
		ref.NamespacedRef.Namespace == nil ||
		*ref.NamespacedRef.Namespace == ent.GetNamespace() {
		if res, errStatus := patch.StatusWithoutCondition(
			ctx, cl, ent,
			configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	targetNamespace := *ref.NamespacedRef.Namespace
	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		ent.GetNamespace(),
		targetNamespace,
		ref.NamespacedRef.Name,
		metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
		metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind("KonnectEventGateway")),
	)
	if crossnamespace.IsReferenceNotGranted(err) {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionFalse,
			configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			fmt.Sprintf(
				"KongReferenceGrants do not allow access to KonnectEventGateway %s/%s",
				targetNamespace,
				ref.NamespacedRef.Name,
			),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
		metav1.ConditionTrue,
		configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		fmt.Sprintf(
			"KongReferenceGrants allow access to KonnectEventGateway %s/%s",
			targetNamespace,
			ref.NamespacedRef.Name,
		),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

func getEventGatewayForRef(
	ctx context.Context,
	cl client.Client,
	ref commonv1alpha1.ObjectRef,
	namespace string,
) (*konnectv1alpha1.KonnectEventGateway, types.NamespacedName, error) {
	switch ref.Type {
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		if ref.NamespacedRef == nil {
			return nil, types.NamespacedName{}, fmt.Errorf("gatewayRef.namespacedRef is required when type is namespacedRef")
		}
		nn := types.NamespacedName{
			Name:      ref.NamespacedRef.Name,
			Namespace: namespace,
		}
		if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
			nn.Namespace = *ref.NamespacedRef.Namespace
		}

		var gateway konnectv1alpha1.KonnectEventGateway
		if err := cl.Get(ctx, nn, &gateway); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nn, ReferencedObjectDoesNotExistError{
					Reference: nn,
					Err:       err,
				}
			}
			return nil, nn, fmt.Errorf("failed to get KonnectEventGateway %s: %w", nn, err)
		}
		return &gateway, nn, nil

	case commonv1alpha1.ObjectRefTypeKonnectID:
		if ref.KonnectID == nil || *ref.KonnectID == "" {
			return nil, types.NamespacedName{}, fmt.Errorf("gatewayRef.konnectID is required when type is konnectID")
		}

		var gateways konnectv1alpha1.KonnectEventGatewayList
		if err := cl.List(ctx, &gateways, client.InNamespace(namespace)); err != nil {
			return nil, types.NamespacedName{}, fmt.Errorf("failed to list KonnectEventGateways in namespace %s: %w", namespace, err)
		}

		for i := range gateways.Items {
			if gateways.Items[i].GetKonnectID() == *ref.KonnectID {
				gateway := gateways.Items[i]
				return &gateway, types.NamespacedName{
					Name:      gateway.GetName(),
					Namespace: gateway.GetNamespace(),
				}, nil
			}
		}

		return nil, types.NamespacedName{}, ReferencedObjectIsInvalidError{
			Reference: fmt.Sprintf("<konnectID:%s>", *ref.KonnectID),
			Msg: fmt.Sprintf(
				"no local KonnectEventGateway with matching Konnect ID was found in namespace %s",
				namespace,
			),
		}

	default:
		return nil, types.NamespacedName{}, fmt.Errorf("unsupported gatewayRef type %q", ref.Type)
	}
}
