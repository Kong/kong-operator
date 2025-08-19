package validation

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kong/kong-operator/ingress-controller/internal/admission"
	ctrlref "github.com/kong/kong-operator/ingress-controller/internal/controllers/reference"
	"github.com/kong/kong-operator/ingress-controller/internal/dataplane/translator"
	"github.com/kong/kong-operator/ingress-controller/internal/store"
)

// SetupAdmissionServer sets up the admission webhook server.
func SetupAdmissionServer(
	ctx context.Context,
	m ctrl.Manager,
	// clientsManager admission.GatewayClientsProvider,
	// referenceIndexers ctrlref.CacheIndexers,
	// translatorFeatures translator.FeatureFlags,
	// storer store.Storer,
) error {
	admissionLogger := ctrl.LoggerFrom(ctx).WithName("admission-server")

	var clientsManager admission.GatewayClientsProvider
	adminAPIServicesProvider := admission.NewDefaultAdminAPIServicesProvider(clientsManager)

	const ingressClassName = "kong"

	// On the level of particular KIC instance for now enable all.
	allEnabled := translator.FeatureFlags{
		RewriteURIs:                             true,
		FillIDs:                                 true,
		ReportConfiguredKubernetesObjects:       true,
		ExpressionRoutes:                        true,
		KongServiceFacade:                       true,
		EnterpriseEdition:                       true,
		KongCustomEntity:                        true,
		CombinedServicesFromDifferentHTTPRoutes: true,
		SupportRedirectPlugin:                   true,
	}

	var storer store.Storer
	srv, err := admission.MakeTLSServer(5443, &admission.RequestHandler{
		Validator: admission.NewKongHTTPValidator(
			admissionLogger,
			m.GetClient(),
			ingressClassName,
			adminAPIServicesProvider,
			allEnabled,
			storer,
		),
		ReferenceIndexers: ctrlref.CacheIndexers{},
		Logger:            admissionLogger,
	})
	if err != nil {
		return err
	}

	go func() {
		admissionLogger.Info(">>> Starting admission server")
		if err := srv.Start(ctx); err != nil {
			admissionLogger.Error(err, "Admission server exited")
		}
	}()

	return nil
}
