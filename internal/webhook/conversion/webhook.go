package conversion

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

type WebhookToConfigure struct {
	// ForType is the type for which the webhook should be configured,
	// it must implement the Hub interface.
	ForType runtime.Object
	// GVR is the GroupVersionResource for the type, required to
	// generate proper CRD patch.
	GVR schema.GroupVersionResource
	// Adjuster optionally allows to adjust
	// the webhook builder with additional options.
	Adjuster func(*builder.WebhookBuilder)
}

// WebhooksToSetup is a list of webhooks that should be registered, for
// each type is expected to implement the Hub interface (be a storage) and
// have corresponding types that implement the conversion logic:
// ConvertTo(...) and ConvertFrom(...) methods.
var WebhooksToSetup = []WebhookToConfigure{
	{
		ForType: &gwtypes.ControlPlane{},
		GVR:     gwtypes.ControlPlaneGVR(),
	},
	{
		ForType: &gwtypes.GatewayConfiguration{},
		GVR:     gwtypes.GatewayConfigurationGVR(),
	},
	{
		ForType: &konnectv1alpha2.KonnectGatewayControlPlane{},
		GVR:     konnectv1alpha2.KonnectGatewayControlPlaneGVR(),
	},
}

// SetupWebhooksWithManager registers the webhook for ControlPlane in the manager.
func SetupWebhooksWithManager(mgr ctrl.Manager) error {
	for _, whCfg := range WebhooksToSetup {
		if _, ok := whCfg.ForType.(interface{ Hub() }); !ok {
			return fmt.Errorf("type %T does not implement Hub interface", whCfg.ForType)
		}

		wh := ctrl.NewWebhookManagedBy(mgr).For(whCfg.ForType)
		if whCfg.Adjuster != nil {
			whCfg.Adjuster(wh)
		}
		if err := wh.Complete(); err != nil {
			return fmt.Errorf("failed to complete webhook for %T: %w", whCfg.ForType, err)
		}
	}
	return nil
}
