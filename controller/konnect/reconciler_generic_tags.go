package konnect

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

// handleReservedTags checks whether the tags that would be attached to the Konnect entity
// (from the object's spec and its konghq.com/tags annotation) contain tags with prefixes
// reserved for the Kubernetes metadata of the source object (e.g. "k8s-uid:").
// Such tags could make the operator match a Konnect entity owned by another object,
// so the object is marked as not programmed and the reconciliation should stop.
//
// CEL validation rules reject these tags in spec, but objects created before the rules
// were introduced and tags set via annotations are not covered by them.
//
// Returns:
//   - bool: true if reconciliation should stop (reserved tags were found)
//   - ctrl.Result: non-zero if a requeue is needed
//   - error: any error encountered
func handleReservedTags[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (bool, ctrl.Result, error) {
	tags, err := userTagsForEntity(ctx, cl, ent)
	if err != nil {
		return false, ctrl.Result{}, err
	}

	reserved := metadata.ReservedTags(tags)
	if len(reserved) == 0 {
		return false, ctrl.Result{}, nil
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KonnectEntityProgrammedConditionType,
		metav1.ConditionFalse,
		konnectv1alpha1.KonnectEntityProgrammedReasonInvalidTags,
		fmt.Sprintf("tags [%s] must not start with reserved prefixes: %s",
			strings.Join(reserved, ", "),
			strings.Join(metadata.ReservedTagPrefixes, ", "),
		),
	); errStatus != nil || !res.IsZero() {
		return true, res, errStatus
	}
	return true, ctrl.Result{}, nil
}

// userTagsForEntity returns the user provided tags which are attached to the Konnect entity
// created for the given object, i.e. the tags from its spec and its konghq.com/tags annotation.
// For KongPluginBinding, the tags of the referenced KongPlugin are included as well.
// It returns nil for types which do not support tags.
func userTagsForEntity(ctx context.Context, cl client.Client, ent client.Object) ([]string, error) {
	var specTags []string
	switch e := ent.(type) {
	case *configurationv1alpha1.KongService:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongRoute:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongUpstream:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongTarget:
		specTags = e.Spec.Tags
	case *configurationv1.KongConsumer:
		specTags = e.Spec.Tags
	case *configurationv1beta1.KongConsumerGroup:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCACertificate:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCertificate:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongSNI:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongKey:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongKeySet:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongVault:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCredentialBasicAuth:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCredentialACL:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCredentialAPIKey:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCredentialJWT:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongCredentialHMAC:
		specTags = e.Spec.Tags
	case *configurationv1alpha1.KongPluginBinding:
		pluginTags, err := referencedKongPluginTags(ctx, cl, e)
		if err != nil {
			return nil, err
		}
		specTags = slices.Concat(pluginTags, e.Spec.Tags)
	default:
		return nil, nil
	}

	return slices.Concat(specTags, metadata.ExtractTags(ent)), nil
}

// referencedKongPluginTags returns the tags (from spec and the konghq.com/tags annotation)
// of the KongPlugin referenced by the given KongPluginBinding.
// If the KongPlugin does not exist, it returns no tags as the missing reference is reported by handlePluginRef.
func referencedKongPluginTags(
	ctx context.Context,
	cl client.Client,
	pluginBinding *configurationv1alpha1.KongPluginBinding,
) ([]string, error) {
	pluginRef := pluginBinding.Spec.PluginReference
	pluginNS := pluginRef.Namespace
	if pluginNS == "" {
		pluginNS = pluginBinding.GetNamespace()
	}

	var plugin configurationv1.KongPlugin
	if err := cl.Get(ctx, client.ObjectKey{Name: pluginRef.Name, Namespace: pluginNS}, &plugin); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get KongPlugin %s/%s: %w", pluginNS, pluginRef.Name, err)
	}

	return slices.Concat(plugin.Tags, metadata.ExtractTags(&plugin)), nil
}
