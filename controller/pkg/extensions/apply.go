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
	operatorv2beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2beta1"
	kcfgkonnect "github.com/kong/kubernetes-configuration/v2/api/konnect"

	extensionserrors "github.com/kong/kong-operator/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/internal/types"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// ExtendableT is the interface implemented by the objects which implementation
// can be extended through extensions.
type ExtendableT interface {
	client.Object
	Extendable
	k8sutils.ConditionsAware

	*operatorv1beta1.DataPlane |
		*gwtypes.ControlPlane |
		*operatorv2beta1.GatewayConfiguration
}

// Extendable is an interface that provides access to the extensions of an object.
type Extendable interface {
	GetExtensions() []commonv1alpha1.ExtensionRef
}

// ApplyExtensions patches the spec of extensible resources (DataPlane, ControlPlane, GatewayConfiguration) by
// applying customizations from their referenced extensions. The actual extension logic is implemented by the
// provided ExtensionProcessor, which handles the resource-specific extension application.
// If extensions are referenced, it adds appropriate conditions to the resource status to indicate the status
// of extension processing. It returns 3 values:
//   - stop: a boolean indicating if the caller must return. It's true when the resource status has been patched.
//   - res: a ctrl.Result indicating if the resource should be requeued. If the error was unexpected (e.g., because
//     of an API server error), the resource should be requeued. For misconfiguration errors, the resource does not
//     need to be requeued, and feedback is provided via resource status conditions.
//   - err: an error in case of failure.
func ApplyExtensions[t ExtendableT](ctx context.Context, cl client.Client, o t, konnectEnabled bool, processor Processor) (stop bool, res ctrl.Result, err error) {
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

		// Process the extensions using the provided processor.
		extensionRefFound, err = processor.Process(ctx, cl, o)
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
