package gateway

import (
	"context"
	"fmt"
	"reflect"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	hybridutils "github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

const (
	konnectAPIAuthGrantLabelKey   = consts.OperatorLabelPrefix + "konnect-api-auth-grant"
	konnectAPIAuthGrantLabelValue = "true"
	konnectAPIAuthGrantNamePrefix = "konnect-api-auth-grant-"
)

func (r *Reconciler) ensureKonnectAPIAuthReferenceGrant(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	gatewayConfig *GatewayConfiguration,
) error {
	if gatewayConfig == nil ||
		gatewayConfig.Spec.Konnect == nil ||
		gatewayConfig.Spec.Konnect.APIAuthConfigurationRef == nil {
		return r.cleanupKonnectAPIAuthReferenceGrants(ctx, gateway)
	}

	authRef := gatewayConfig.Spec.Konnect.APIAuthConfigurationRef
	if authRef.Namespace == nil || *authRef.Namespace == "" || *authRef.Namespace == gateway.Namespace {
		return r.cleanupKonnectAPIAuthReferenceGrants(ctx, gateway)
	}
	authNamespace := *authRef.Namespace

	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		r.Client,
		gatewayConfig.Namespace,
		authNamespace,
		authRef.Name,
		metav1.GroupVersionKind(operatorv2beta1.SchemeGroupVersion.WithKind("GatewayConfiguration")),
		metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind("KonnectAPIAuthConfiguration")),
	)
	if err != nil {
		if crossnamespace.IsReferenceNotGranted(err) {
			return r.cleanupKonnectAPIAuthReferenceGrants(ctx, gateway)
		}
		return err
	}

	if err := r.cleanupStaleKonnectAPIAuthReferenceGrants(ctx, gateway, authNamespace, authRef.Name); err != nil {
		return err
	}
	return r.ensureManagedKonnectAPIAuthReferenceGrant(ctx, gateway, authNamespace, authRef.Name)
}

// ensureManagedKonnectAPIAuthReferenceGrant creates or updates a managed KongReferenceGrant
// that allows the KonnectGatewayControlPlane to reference a KonnectAPIAuthConfiguration
// in another namespace.
//
// When a GatewayConfiguration references a KonnectAPIAuthConfiguration in a different namespace,
// the user must create a KongReferenceGrant to permit that cross-namespace reference. However,
// the operator also creates a KonnectGatewayControlPlane that needs to reference the same
// KonnectAPIAuthConfiguration. This function ensures a managed KongReferenceGrant exists that
// mirrors the user's grant, allowing the operator-created KonnectGatewayControlPlane to access
// the KonnectAPIAuthConfiguration.
func (r *Reconciler) ensureManagedKonnectAPIAuthReferenceGrant(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	authNamespace string,
	authName string,
) error {
	desired := newKonnectAPIAuthReferenceGrant(gateway, authNamespace, authName)
	key := client.ObjectKeyFromObject(desired)
	existing := &configurationv1alpha1.KongReferenceGrant{}
	if err := r.Get(ctx, key, existing); err != nil {
		if k8serrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}
	if !isManagedKonnectAPIAuthGrant(existing, gateway) {
		return fmt.Errorf("existing KongReferenceGrant %s is not managed by Gateway %s/%s", key, gateway.Namespace, gateway.Name)
	}

	metaUpdated, updatedMeta := k8sutils.EnsureObjectMetaIsUpdated(existing.ObjectMeta, desired.ObjectMeta)
	specUpdated := !reflect.DeepEqual(existing.Spec, desired.Spec)
	if !metaUpdated && !specUpdated {
		return nil
	}

	old := existing.DeepCopy()
	existing.ObjectMeta = updatedMeta
	existing.Spec = desired.Spec
	return r.Patch(ctx, existing, client.MergeFrom(old))
}

func (r *Reconciler) cleanupKonnectAPIAuthReferenceGrants(ctx context.Context, gateway *gwtypes.Gateway) error {
	var grants configurationv1alpha1.KongReferenceGrantList
	if err := r.List(ctx, &grants, client.MatchingLabels(konnectAPIAuthGrantLabels(gateway))); err != nil {
		return err
	}
	for i := range grants.Items {
		grant := &grants.Items[i]
		if !isManagedKonnectAPIAuthGrant(grant, gateway) {
			continue
		}
		if err := r.Delete(ctx, grant); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// cleanupStaleKonnectAPIAuthReferenceGrants deletes any KonnectAPIAuthConfiguration reference grants
// owned by this gateway that don't match the current desired auth target specified by authNamespace
// and authName. This ensures only the current desired managed grant remains.
func (r *Reconciler) cleanupStaleKonnectAPIAuthReferenceGrants(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	authNamespace string,
	authName string,
) error {
	desiredName := konnectAPIAuthReferenceGrantName(gateway, authNamespace, authName)
	var grants configurationv1alpha1.KongReferenceGrantList
	if err := r.List(ctx, &grants, client.MatchingLabels(konnectAPIAuthGrantLabels(gateway))); err != nil {
		return err
	}
	for i := range grants.Items {
		grant := &grants.Items[i]
		if !isManagedKonnectAPIAuthGrant(grant, gateway) {
			continue
		}
		if grant.Namespace == authNamespace && grant.Name == desiredName {
			continue
		}
		if err := r.Delete(ctx, grant); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func konnectAPIAuthReferenceGrantName(gateway *gwtypes.Gateway, authNamespace string, authName string) string {
	hashInput := struct {
		GatewayNamespace string
		GatewayName      string
		AuthNamespace    string
		AuthName         string
	}{
		GatewayNamespace: gateway.Namespace,
		GatewayName:      gateway.Name,
		AuthNamespace:    authNamespace,
		AuthName:         authName,
	}
	return konnectAPIAuthGrantNamePrefix + hybridutils.Hash64(hashInput)
}

func newKonnectAPIAuthReferenceGrant(
	gateway *gwtypes.Gateway,
	authNamespace string,
	authName string,
) *configurationv1alpha1.KongReferenceGrant {
	return &configurationv1alpha1.KongReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      konnectAPIAuthReferenceGrantName(gateway, authNamespace, authName),
			Namespace: authNamespace,
			Labels:    konnectAPIAuthGrantLabels(gateway),
		},
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
					Kind:      configurationv1alpha1.Kind("KonnectGatewayControlPlane"),
					Namespace: configurationv1alpha1.Namespace(gateway.Namespace),
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
					Kind:  configurationv1alpha1.Kind("KonnectAPIAuthConfiguration"),
					Name:  toObjectNamePtr(authName),
				},
			},
		},
	}
}

func konnectAPIAuthGrantLabels(gateway *gwtypes.Gateway) map[string]string {
	labels := k8sutils.GetManagedByLabelSet(gateway)
	labels[konnectAPIAuthGrantLabelKey] = konnectAPIAuthGrantLabelValue
	return labels
}

func isManagedKonnectAPIAuthGrant(grant *configurationv1alpha1.KongReferenceGrant, gateway *gwtypes.Gateway) bool {
	labels := grant.GetLabels()
	if labels == nil {
		return false
	}
	return labels[konnectAPIAuthGrantLabelKey] == konnectAPIAuthGrantLabelValue &&
		labels[consts.GatewayOperatorManagedByLabel] == consts.GatewayManagedLabelValue &&
		labels[consts.GatewayOperatorManagedByNameLabel] == gateway.Name &&
		labels[consts.GatewayOperatorManagedByNamespaceLabel] == gateway.Namespace
}

func toObjectNamePtr(name string) *configurationv1alpha1.ObjectName {
	objName := configurationv1alpha1.ObjectName(name)
	return &objName
}
