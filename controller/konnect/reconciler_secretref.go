package konnect

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/internal/utils/crossnamespace"
)

// GetSecretRefs extracts all NamespacedRef secret references from the given entity.
func getSecretRefs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) []commonv1alpha1.NamespacedRef {
	secretRefs := []commonv1alpha1.NamespacedRef{}
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongCertificate:
		if e.Spec.SecretRef != nil {
			secretRefs = append(secretRefs, *e.Spec.SecretRef)
		}
		if e.Spec.SecretRefAlt != nil {
			secretRefs = append(secretRefs, *e.Spec.SecretRefAlt)
		}
	case *configurationv1alpha1.KongCACertificate:
		if e.Spec.SecretRef != nil {
			secretRefs = append(secretRefs, *e.Spec.SecretRef)
		}
	}
	return secretRefs
}

// handleSecretRef handles the SecretRef for the given entity.
// It checks if the referenced Secrets exist and verifies cross-namespace
// access via KongReferenceGrants if applicable.
func handleSecretRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, bool, error) {
	var entityHasCrossNamespaceRefs bool
	deleting := !ent.GetDeletionTimestamp().IsZero()

	secretRefs := getSecretRefs(ent)
	for _, secretRef := range secretRefs {
		ns := ent.GetNamespace()
		var crossNamespaceRef bool
		if secretRef.Namespace != nil && *secretRef.Namespace != "" && *secretRef.Namespace != ns {
			ns = *secretRef.Namespace
			crossNamespaceRef = true
			entityHasCrossNamespaceRefs = true
		}
		nn := client.ObjectKey{
			Name:      secretRef.Name,
			Namespace: ns,
		}
		secret := corev1.Secret{}
		if err := cl.Get(ctx, nn, &secret); err != nil {
			if deleting {
				continue
			}
			if res, errStatus := patch.StatusWithCondition(
				ctx, cl, ent,
				konnectv1alpha1.SecretRefValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.SecretRefReasonInvalid,
				err.Error(),
			); errStatus != nil || !res.IsZero() {
				return res, true, errStatus
			}
			return ctrl.Result{}, true, &ReferencedSecretDoesNotExist{
				Reference: nn,
				Err:       err,
			}
		}

		if crossNamespaceRef {
			err := crossnamespace.CheckKongReferenceGrantForResource(
				ctx,
				cl,
				ent.GetNamespace(),
				ns,
				secretRef.Name,
				metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
				metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret")),
			)
			if err != nil {
				if deleting {
					continue
				}
				if crossnamespace.IsReferenceNotGranted(err) {
					if res, errStatus := patch.StatusWithCondition(
						ctx, cl, ent,
						consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
						metav1.ConditionFalse,
						configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
						fmt.Sprintf("KongReferenceGrants do not allow access to Secret %s/%s", ns, secretRef.Name),
					); errStatus != nil || !res.IsZero() {
						return res, true, errStatus
					}
					return ctrl.Result{}, true, nil
				}
				return ctrl.Result{}, true, err
			}

		}
	}

	if entityHasCrossNamespaceRefs && !deleting {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionTrue,
			configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			"KongReferenceGrants allow access to Secrets",
		); errStatus != nil || !res.IsZero() {
			return res, true, errStatus
		}
	}
	return ctrl.Result{}, false, nil
}
