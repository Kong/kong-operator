package extensions

import (
	"context"
	"errors"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	kcfgkonnect "github.com/kong/kubernetes-configuration/v2/api/konnect"

	extensionserrors "github.com/kong/kong-operator/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/controller/pkg/extensions/konnect"
	"github.com/kong/kong-operator/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/internal/types"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// ExtendableT is the interface implemented by the objects which implementation
// can be extended through extensions.
type ExtendableT interface {
	client.Object
	withExtensions
	k8sutils.ConditionsAware

	*operatorv1beta1.DataPlane |
		*gwtypes.ControlPlane |
		*operatorv1beta1.GatewayConfiguration
}

type withExtensions interface {
	GetExtensions() []commonv1alpha1.ExtensionRef
}

// ApplyExtensions patches the dataplane or controlplane spec by taking into account customizations from the referenced extensions.
// In case any extension is referenced, it adds a resolvedRefs condition to the dataplane, indicating the status of the
// extension reference. it returns 3 values:
//   - stop: a boolean indicating if the caller must return. It's true when the dataplane status has been patched.
//   - res: a ctrl.Result indicating if the dataplane should be requeued. If the error was unexpected (e.g., because of API server error), the dataplane should be requeued.
//     In case the error is related to a misconfiguration, the dataplane does not need to be requeued, and feedback is provided into the dataplane status.
//   - err: an error in case of failure.
func ApplyExtensions[t ExtendableT](ctx context.Context, cl client.Client, o t, konnectEnabled bool) (stop bool, res ctrl.Result, err error) {
	// extensionsCondition can be nil. In that case, no extensions are referenced by the object.
	extensionsCondition := validateExtensions(o)
	if extensionsCondition == nil {
		return false, ctrl.Result{}, nil
	}

	if res, err := patch.StatusWithCondition(
		ctx,
		cl,
		o,
		kcfgconsts.ConditionType(extensionsCondition.Type),
		extensionsCondition.Status,
		kcfgconsts.ConditionReason(extensionsCondition.Reason),
		extensionsCondition.Message,
	); err != nil || !res.IsZero() {
		return true, res, err
	}
	if extensionsCondition.Status == metav1.ConditionFalse {
		return false, ctrl.Result{}, extensionserrors.ErrInvalidExtensions
	}

	// the konnect extension is the only one implemented at the moment. In case konnect is not enabled, we return early.
	if !konnectEnabled {
		return false, ctrl.Result{}, nil
	}

	// in case the extensionsCondition is true, let's apply the extensions.
	konnectExtensionApplied := k8sutils.NewConditionWithGeneration(kcfgkonnect.KonnectExtensionAppliedType, metav1.ConditionTrue, kcfgkonnect.KonnectExtensionAppliedReason, "The Konnect extension has been successsfully applied", o.GetGeneration())
	if extensionsCondition.Status == metav1.ConditionTrue {
		var (
			extensionRefFound bool
			err               error
		)

		switch obj := any(o).(type) {
		case *operatorv1beta1.DataPlane:
			extensionRefFound, err = konnect.ApplyDataPlaneKonnectExtension(ctx, cl, obj)
		case *gwtypes.ControlPlane:
			extensionRefFound, err = konnect.ApplyControlPlaneKonnectExtension(ctx, cl, obj)
		default:
			return false, ctrl.Result{}, errors.New("unsupported object type")
		}
		if err != nil {
			switch {
			case errors.Is(err, extensionserrors.ErrCrossNamespaceReference):
				konnectExtensionApplied.Status = metav1.ConditionFalse
				konnectExtensionApplied.Reason = string(kcfgkonnect.RefNotPermittedReason)
				konnectExtensionApplied.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrKonnectExtensionNotFound):
				konnectExtensionApplied.Status = metav1.ConditionFalse
				konnectExtensionApplied.Reason = string(kcfgkonnect.InvalidExtensionRefReason)
				konnectExtensionApplied.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrClusterCertificateNotFound):
				konnectExtensionApplied.Status = metav1.ConditionFalse
				konnectExtensionApplied.Reason = string(kcfgkonnect.InvalidSecretRefReason)
				konnectExtensionApplied.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			case errors.Is(err, extensionserrors.ErrKonnectExtensionNotReady):
				konnectExtensionApplied.Status = metav1.ConditionFalse
				konnectExtensionApplied.Reason = string(kcfgkonnect.KonnectExtensionNotReadyReason)
				konnectExtensionApplied.Message = strings.ReplaceAll(err.Error(), "\n", " - ")
			default:
				return true, ctrl.Result{}, err
			}
		}
		if !extensionRefFound {
			return false, ctrl.Result{}, nil
		}
	}

	if res, err := patch.StatusWithCondition(
		ctx,
		cl,
		o,
		kcfgconsts.ConditionType(konnectExtensionApplied.Type),
		konnectExtensionApplied.Status,
		kcfgconsts.ConditionReason(konnectExtensionApplied.Reason),
		konnectExtensionApplied.Message,
	); err != nil || !res.IsZero() {
		return true, res, err
	}

	return false, ctrl.Result{}, err
}
