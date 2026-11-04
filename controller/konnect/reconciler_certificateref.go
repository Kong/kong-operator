package konnect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	hybridgatewaymetadata "github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	hybridgatewayrefs "github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func gatewayOwnerForSNI(sni *configurationv1alpha1.KongSNI) *metav1.OwnerReference {
	owner := metav1.GetControllerOf(sni)
	if owner == nil || owner.Kind != "Gateway" || !strings.HasPrefix(owner.APIVersion, gwtypes.GroupName+"/") {
		return nil
	}
	return owner
}

func getGatewayForSNI(ctx context.Context, cl client.Client, sni *configurationv1alpha1.KongSNI) (*gwtypes.Gateway, error) {
	owner := gatewayOwnerForSNI(sni)
	if owner == nil {
		return nil, nil
	}
	nn := client.ObjectKey{Namespace: sni.Namespace, Name: owner.Name}
	var gateway gwtypes.Gateway
	if err := cl.Get(ctx, nn, &gateway); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ReferencedObjectDoesNotExistError{Reference: nn, Err: err}
		}
		return nil, fmt.Errorf("failed to get KongSNI owner Gateway: %w", err)
	}
	if gateway.UID != owner.UID {
		return nil, ReferencedObjectDoesNotExistError{
			Reference: nn,
			Err:       fmt.Errorf("KongSNI owner Gateway UID %q does not match current Gateway UID %q", owner.UID, gateway.UID),
		}
	}
	return &gateway, nil
}

// Recheck Gateway-owned bindings before programming: the certificate's ControlPlane
// or grants may have changed before the Gateway converter can remove its old SNI.
func validateGatewaySNICertificate(ctx context.Context, cl client.Client, sni *configurationv1alpha1.KongSNI, cert *configurationv1alpha1.KongCertificate) error {
	gateway, err := getGatewayForSNI(ctx, cl, sni)
	if _, missing := errors.AsType[ReferencedObjectDoesNotExistError](err); missing {
		return &hybridgatewayrefs.ListenerCertificateRefError{
			Reason: gatewayv1.ListenerReasonInvalidCertificateRef, Message: err.Error(),
		}
	}
	if err != nil || gateway == nil {
		return err
	}
	ref := gatewayv1.SecretObjectReference{
		Group: new(gatewayv1.Group(configurationv1alpha1.GroupVersion.Group)),
		Kind:  new(gatewayv1.Kind("KongCertificate")), Name: gatewayv1.ObjectName(cert.Name),
	}
	var referenceGrantVersion schema.GroupVersion
	if cert.Namespace != gateway.Namespace {
		ref.Namespace = new(gatewayv1.Namespace(cert.Namespace))
		mapping, err := cl.RESTMapper().RESTMapping(schema.GroupKind{Group: gatewayv1.GroupName, Kind: "ReferenceGrant"})
		if err != nil {
			return fmt.Errorf("failed to resolve ReferenceGrant version for Gateway-owned KongSNI: %w", err)
		}
		referenceGrantVersion = mapping.GroupVersionKind.GroupVersion()
	}
	_, err = hybridgatewayrefs.ResolveListenerKongCertificate(ctx, cl, referenceGrantVersion, gateway, ref)
	return err
}

func handleHybridGatewayServiceCertificateRef(
	ctx context.Context,
	cl client.Client,
	service *configurationv1alpha1.KongService,
) (ctrl.Result, error) {
	// References may have been revoked or moved since translation. Deletion uses
	// the persisted IDs, never the current certificate's ControlPlane or status.
	if !service.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	ref := service.Spec.ClientCertificateRef
	if ref == nil || ref.Namespace == nil || *ref.Namespace == "" || *ref.Namespace == service.Namespace {
		if condition, ok := k8sutils.GetCondition(consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs), service); ok &&
			strings.Contains(condition.Message, "KongCertificate") {
			if res, err := patch.StatusWithoutCondition(ctx, cl, service, string(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)); err != nil || !res.IsZero() {
				return res, err
			}
		}
	}
	if service.Spec.ClientCertificateRef == nil {
		if res, err := patch.StatusWithoutCondition(ctx, cl, service, string(konnectv1alpha1.KongCertificateRefValidConditionType)); err != nil || !res.IsZero() {
			return res, err
		}
		if service.Status.Konnect != nil {
			service.Status.Konnect.CertificateID = ""
		}
		return ctrl.Result{}, nil
	}
	cert, err := hybridgatewayrefs.ResolveServiceKongCertificate(ctx, cl, service)
	if invalid, ok := errors.AsType[*hybridgatewayerrors.ClientCertificateRefError](err); ok {
		if invalid.Reason == string(gatewayv1.RouteReasonRefNotPermitted) {
			if res, err := patch.StatusWithCondition(ctx, cl, service,
				consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
				metav1.ConditionFalse, configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted, invalid.Message,
			); err != nil || !res.IsZero() {
				return res, err
			}
		}
		if res, err := patch.StatusWithCondition(ctx, cl, service,
			konnectv1alpha1.KongCertificateRefValidConditionType, metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid, invalid.Message,
		); err != nil || !res.IsZero() {
			return res, err
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if ref := service.Spec.ClientCertificateRef; ref.Namespace != nil && *ref.Namespace != "" && *ref.Namespace != service.Namespace {
		if res, err := patch.StatusWithCondition(ctx, cl, service,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionTrue, configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			"KongReferenceGrants allow access to KongCertificate",
		); err != nil || !res.IsZero() {
			return res, err
		}
	}
	if res, err := patch.StatusWithCondition(ctx, cl, service,
		konnectv1alpha1.KongCertificateRefValidConditionType, metav1.ConditionTrue,
		konnectv1alpha1.KongCertificateRefReasonValid, "Referenced KongCertificate is programmed in the KongService ControlPlane",
	); err != nil || !res.IsZero() {
		return res, err
	}
	if service.Status.Konnect == nil {
		service.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{}
	}
	// Do not derive or overwrite the KongService ControlPlane from a certificate.
	service.Status.Konnect.CertificateID = cert.GetKonnectID()
	return ctrl.Result{}, nil
}

// getKongCertificateRef gets the reference of KongCertificate.
func getKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.NamespacedRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongSNI:
		// Return the full CertificateRef including the optional Namespace for cross-namespace refs.
		return mo.Some(e.Spec.CertificateRef)
	case *configurationv1alpha1.KongService:
		if e.Spec.ClientCertificateRef != nil {
			return mo.Some(*e.Spec.ClientCertificateRef)
		}
		return mo.None[commonv1alpha1.NamespacedRef]()
	case *configurationv1alpha1.KongUpstream:
		if e.Spec.ClientCertificateRef != nil {
			return mo.Some(*e.Spec.ClientCertificateRef)
		}
		return mo.None[commonv1alpha1.NamespacedRef]()
	default:
		return mo.None[commonv1alpha1.NamespacedRef]()
	}
}

func handleKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	if service, ok := any(ent).(*configurationv1alpha1.KongService); ok && hybridgatewaymetadata.IsHybridGatewayService(service) {
		return handleHybridGatewayServiceCertificateRef(ctx, cl, service)
	}
	// Revoking a reference must not prevent deleting the existing remote SNI.
	// Keep its persisted ControlPlane/Certificate IDs for the delete operation.
	if sni, ok := any(ent).(*configurationv1alpha1.KongSNI); ok &&
		!sni.DeletionTimestamp.IsZero() && gatewayOwnerForSNI(sni) != nil {
		return ctrl.Result{}, nil
	}

	certRef, ok := getKongCertificateRef(ent).Get()
	if !ok {
		if svc, ok := any(ent).(*configurationv1alpha1.KongService); ok && svc.Status.Konnect != nil {
			svc.Status.Konnect.CertificateID = ""
		}
		if upstream, ok := any(ent).(*configurationv1alpha1.KongUpstream); ok && upstream.Status.Konnect != nil {
			upstream.Status.Konnect.CertificateID = ""
		}
		return ctrl.Result{}, nil
	}

	certNamespace := ent.GetNamespace()
	var crossNamespaceRef bool
	if certRef.Namespace != nil && *certRef.Namespace != "" && *certRef.Namespace != certNamespace {
		certNamespace = *certRef.Namespace
		crossNamespaceRef = true
	}

	if crossNamespaceRef {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx,
			cl,
			ent.GetNamespace(),
			certNamespace,
			certRef.Name,
			metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongCertificate")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				if res, errStatus := patch.StatusWithCondition(
					ctx, cl, ent,
					consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
					metav1.ConditionFalse,
					configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
					fmt.Sprintf("KongReferenceGrants do not allow access to KongCertificate %s/%s", certNamespace, certRef.Name),
				); errStatus != nil || !res.IsZero() {
					return res, errStatus
				}
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}

		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionTrue,
			configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			"KongReferenceGrants allow access to KongCertificate",
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
	}

	cert := &configurationv1alpha1.KongCertificate{}
	nn := types.NamespacedName{
		Name:      certRef.Name,
		Namespace: certNamespace,
	}
	err := cl.Get(ctx, nn, cert)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongCertificateDoesNotExistError{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongCertificate is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := cert.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid,
			fmt.Sprintf("Referenced KongCertificate %s is being deleted", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedKongCertificateIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	if sni, ok := any(ent).(*configurationv1alpha1.KongSNI); ok {
		if err := validateGatewaySNICertificate(ctx, cl, sni, cert); err != nil {
			if invalid, ok := errors.AsType[*hybridgatewayrefs.ListenerCertificateRefError](err); ok {
				if res, errStatus := patch.StatusWithCondition(ctx, cl, ent,
					konnectv1alpha1.KongCertificateRefValidConditionType, metav1.ConditionFalse,
					konnectv1alpha1.KongCertificateRefReasonInvalid, invalid.Message,
				); errStatus != nil || !res.IsZero() {
					return res, errStatus
				}
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}
	}

	// requeue it if referenced KongCertificate is not programmed yet so we cannot do the following work.
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cert)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid,
			fmt.Sprintf("Referenced KongCertificate %s is not programmed yet", nn),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}
		// Don't requeue. The referenced entity's changes will trigger the reconciliation.
		return ctrl.Result{}, nil
	}

	// Save the certificate Konnect ID now (cert is confirmed programmed).
	// We intentionally write it to ent.Status AFTER all patch.StatusWithCondition calls
	// below to avoid clobbering: each merge-patch only carries condition diffs (CertificateID
	// is identical in old and ent at snapshot time), so the server keeps its stored ""
	// and overwrites ent.Status in the response, losing the in-memory value.
	certKonnectID := cert.GetKonnectID()

	// TODO: make this more generic.
	// Initialise ent.Status.Konnect if nil so subsequent patches do not panic.
	switch ent := any(ent).(type) {
	case *configurationv1alpha1.KongSNI:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
	case *configurationv1alpha1.KongService:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{}
		}
	case *configurationv1alpha1.KongUpstream:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KongCertificateRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongCertificateRefReasonValid,
		fmt.Sprintf("Referenced KongCertificate %s programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	cpRef, ok := controlplane.GetControlPlaneRef(cert).Get()
	// TODO: ignore the entity if referenced KongCertificate does not have a Konnect control plane reference
	// because this situation is likely to mean that they are not controlled by us:
	// https://github.com/kong/kong-operator/issues/629
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"%T references a KongCertificate %s which does not have a ControlPlane ref",
			ent, client.ObjectKeyFromObject(cert),
		)
	}
	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, cert.GetNamespace())
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
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, controlplane.ReferencedControlPlaneDoesNotExistError{
				Reference: cpRef,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cpnn := types.NamespacedName{
		Name:      cp.Name,
		Namespace: cp.Namespace,
	}

	cond, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", cpnn),
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
			if apierrors.IsConflict(err) {
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
		fmt.Sprintf("Referenced ControlPlane %s is programmed", cpnn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	// Set CertificateID after all patches so the server-response from each merge patch
	// cannot clobber it (see comment above near certKonnectID declaration).
	// Status.Konnect is already initialised above so nil-guards are not needed here.
	old := ent.DeepCopyObject().(TEnt)
	switch ent := any(ent).(type) {
	case *configurationv1alpha1.KongSNI:
		ent.Status.Konnect.CertificateID = certKonnectID
	case *configurationv1alpha1.KongService:
		ent.Status.Konnect.CertificateID = certKonnectID
	case *configurationv1alpha1.KongUpstream:
		ent.Status.Konnect.CertificateID = certKonnectID
	}

	// Persist it now, before the entity is created in Konnect. KongSNI deletion
	// and the create/update payloads read the ID from the status, so a delete
	// racing the create would otherwise leave the entity undeletable in Konnect.
	if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
