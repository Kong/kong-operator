package konnect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func getExtension(ctx context.Context, cl client.Client, objNamespace string, extRef commonv1alpha1.ExtensionRef) (*konnectv1alpha2.KonnectExtension, error) {
	if extRef.Group != konnectv1alpha1.SchemeGroupVersion.Group || extRef.Kind != konnectv1alpha2.KonnectExtensionKind {
		return nil, nil
	}

	if extRef.Namespace != nil && *extRef.Namespace != objNamespace {
		return nil, errors.Join(extensionserrors.ErrCrossNamespaceReference, fmt.Errorf("the cross-namespace reference to the extension %s/%s is not permitted", *extRef.Namespace, extRef.Name))
	}

	konnectExt := konnectv1alpha2.KonnectExtension{}
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: objNamespace,
		Name:      extRef.Name,
	}, &konnectExt); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Join(extensionserrors.ErrKonnectExtensionNotFound, fmt.Errorf("the extension %s/%s is not found", objNamespace, extRef.Name))
		}
		return nil, err
	}
	if !k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, &konnectExt) ||
		konnectExt.Status.Konnect == nil ||
		konnectExt.Status.Konnect.ClusterType == "" {
		return nil, errors.Join(extensionserrors.ErrKonnectExtensionNotReady, fmt.Errorf("the extension %s/%s is not ready", objNamespace, extRef.Name))
	}

	return &konnectExt, nil
}

// IsHybridClusterType returns true for Konnect hybrid cluster types, whose DataPlanes don't need the admin API.
func IsHybridClusterType(ct konnectv1alpha2.KonnectExtensionClusterType) bool {
	switch ct {
	case konnectv1alpha2.ClusterTypeControlPlane, konnectv1alpha2.ClusterTypeControlPlaneGroup:
		return true
	default:
		return false
	}
}

// AdminAPIDisabled returns true when the Konnect cluster type is hybrid and the user
// didn't set KONG_ADMIN_LISTEN in the DataPlane pod template.
func AdminAPIDisabled(dp *operatorv1beta1.DataPlane, ct konnectv1alpha2.KonnectExtensionClusterType) bool {
	return IsHybridClusterType(ct) && !adminListenSetByUser(dp)
}

// adminListenSetByUser returns true when the proxy container sets KONG_ADMIN_LISTEN to a literal value other than "off".
func adminListenSetByUser(dp *operatorv1beta1.DataPlane) bool {
	if dp == nil || dp.Spec.Deployment.PodTemplateSpec == nil {
		return false
	}
	c := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if c == nil {
		return false
	}
	v := strings.TrimSpace(k8sutils.EnvValueByName(c.Env, "KONG_ADMIN_LISTEN"))
	return v != "" && v != "off"
}

// DataPlaneAdminAPIDisabled returns whether the admin API is disabled for the DataPlane
// given the KonnectExtension it references (false when it references none).
func DataPlaneAdminAPIDisabled(ctx context.Context, cl client.Client, dp *operatorv1beta1.DataPlane) (bool, error) {
	for _, extensionRef := range dp.Spec.Extensions {
		ext, err := getExtension(ctx, cl, dp.Namespace, extensionRef)
		if err != nil {
			return false, err
		}
		if ext != nil {
			return AdminAPIDisabled(dp, ext.Status.Konnect.ClusterType), nil
		}
	}
	return false, nil
}
