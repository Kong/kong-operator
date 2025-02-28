package extensions

import (
	"context"
	"errors"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	extensionserrors "github.com/kong/gateway-operator/controller/pkg/extensions/errors"
	"github.com/kong/gateway-operator/controller/pkg/extensions/konnect"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// ExtendableT is the interface implemented by the objects which implementation
// can be extended through extensions.
type ExtendableT interface {
	client.Object
	withExtensions
	k8sutils.ConditionsAware

	*operatorv1beta1.DataPlane |
		*operatorv1beta1.ControlPlane
}

type withExtensions interface {
	GetExtensions() []commonv1alpha1.ExtensionRef
}

// applyExtensions patches the dataplane or controlplane spec by taking into account customizations from the referenced extensions.
// In case any extension is referenced, it adds a resolvedRefs condition to the dataplane, indicating the status of the
// extension reference. it returns 3 values:
//   - stop: a boolean indicating if the caller must return. It's true when the dataplane status has been patched.
//   - requeue: a boolean indicating if the dataplane should be requeued. If the error was unexpected (e.g., because of API server error), the dataplane should be requeued.
//     In case the error is related to a misconfiguration, the dataplane does not need to be requeued, and feedback is provided into the dataplane status.
//   - err: an error in case of failure.
func ApplyExtensions[t ExtendableT](ctx context.Context, cl client.Client, logger logr.Logger, o t, konnectEnabled bool) (stop bool, requeue bool, err error) {
	// extensionsCondition can be nil. In that case, no extensions are referenced by the object.
	extensionsCondition := validateExtensions(o)
	if extensionsCondition == nil {
		return false, false, nil
	}

	if res, err := patch.StatusWithCondition(
		ctx,
		cl,
		o,
		consts.ConditionType(extensionsCondition.Type),
		extensionsCondition.Status,
		consts.ConditionReason(extensionsCondition.Reason),
		extensionsCondition.Message,
	); err != nil || !res.IsZero() {
		return true, true, err
	}
	if extensionsCondition.Status == metav1.ConditionFalse {
		return false, false, errors.New(extensionsCondition.Message)
	}

	// the konnect extension is the only one implemented at the moment. In case konnect is not enabled, we return early.
	if !konnectEnabled {
		return false, false, nil
	}

	// in case the extensionsCondition is true, let's apply the extensions.
	konnectExtensionCondition := k8sutils.NewConditionWithGeneration(consts.KonnectExtensionAppliedType, metav1.ConditionTrue, consts.KonnectExtensionAppliedReason, "The Konnect extension has been successsfully applied", o.GetGeneration())
	if extensionsCondition.Status == metav1.ConditionTrue {
		var (
			found bool
			err   error
		)
		switch obj := any(o).(type) {
		case *operatorv1beta1.DataPlane:
			found, err = konnect.ApplyDataPlaneKonnectExtension(ctx, cl, obj)
		case *operatorv1beta1.ControlPlane:
			found, err = konnect.ApplyControlPlaneKonnectExtension(ctx, cl, obj)
		}
		if !found {
			return false, false, nil
		}
		if err != nil {
			switch {
			case errors.Is(err, extensionserrors.ErrCrossNamespaceReference):
				konnectExtensionCondition.Status = metav1.ConditionFalse
				konnectExtensionCondition.Reason = string(consts.RefNotPermittedReason)
				konnectExtensionCondition.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrKonnectExtensionNotFound):
				konnectExtensionCondition.Status = metav1.ConditionFalse
				konnectExtensionCondition.Reason = string(consts.InvalidExtensionRefReason)
				konnectExtensionCondition.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrClusterCertificateNotFound):
				konnectExtensionCondition.Status = metav1.ConditionFalse
				konnectExtensionCondition.Reason = string(consts.InvalidSecretRefReason)
				konnectExtensionCondition.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrKonnectExtensionNotReady):
				konnectExtensionCondition.Status = metav1.ConditionFalse
				konnectExtensionCondition.Reason = string(consts.KonnectExtensionNotReadyReason)
				konnectExtensionCondition.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			default:
				return true, false, err
			}
		}
	}

	if res, err := patch.StatusWithCondition(
		ctx,
		cl,
		o,
		consts.ConditionType(extensionsCondition.Type),
		extensionsCondition.Status,
		consts.ConditionReason(extensionsCondition.Reason),
		extensionsCondition.Message,
	); err != nil || !res.IsZero() {
		return true, true, err
	}

	return false, false, err
}
