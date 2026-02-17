package konnect

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
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
