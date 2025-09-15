package specialized

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/controller/pkg/log"
	secretref "github.com/kong/kong-operator/controller/pkg/secrets/ref"
)

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Owned Resource Create/Update
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) createOrUpdateHTTPRoute(
	ctx context.Context,
	logger logr.Logger,
	httpRoute *gatewayv1.HTTPRoute,
) (bool, error) {
	log.Trace(logger, "checking for any existing httproute for aigateway")

	// TODO - use GenerateName
	//
	// See: https://github.com/kong/kong-operator/issues/137
	found := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      httpRoute.Name,
		Namespace: httpRoute.Namespace,
	}, found)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(logger, "creating httproute for aigateway")
			return true, r.Create(ctx, httpRoute)
		}
		return false, err
	}

	// TODO - implement patching
	//
	// See: https://github.com/kong/kong-operator/issues/137

	return false, nil
}

func (r *AIGatewayReconciler) createOrUpdatePlugin(
	ctx context.Context,
	logger logr.Logger,
	kongPlugin *configurationv1.KongPlugin,
) (bool, error) {
	log.Trace(logger, "checking for any existing plugin for aigateway")

	// TODO - use GenerateName
	//
	// See: https://github.com/kong/kong-operator/issues/137
	found := &configurationv1.KongPlugin{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      kongPlugin.Name,
		Namespace: kongPlugin.Namespace,
	}, found)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(logger, "creating plugin for aigateway")
			return true, r.Create(ctx, kongPlugin)
		}
		return false, err
	}

	// TODO - implement patching
	//
	// See: https://github.com/kong/kong-operator/issues/137

	return false, nil
}

func (r *AIGatewayReconciler) createOrUpdateGateway(
	ctx context.Context,
	logger logr.Logger,
	gateway *gatewayv1.Gateway,
) (bool, error) {
	log.Trace(logger, "checking for any existing gateway for aigateway")
	found := &gatewayv1.Gateway{}

	// TODO - use GenerateName
	//
	// See: https://github.com/kong/kong-operator/issues/137
	err := r.Get(ctx, types.NamespacedName{
		Name:      gateway.Name,
		Namespace: gateway.Namespace,
	}, found)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(logger, "creating gateway for aigateway")
			return true, r.Create(ctx, gateway)
		}

		return false, err
	}

	// TODO - implement patching
	//
	// See: https://github.com/kong/kong-operator/issues/137

	return false, nil
}

func (r *AIGatewayReconciler) createOrUpdateSvc(
	ctx context.Context,
	logger logr.Logger,
	service *corev1.Service,
) (bool, error) {
	log.Trace(logger, "checking for any existing service for aigateway")

	// TODO - use GenerateName
	//
	// See: https://github.com/kong/kong-operator/issues/137
	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      service.Name,
		Namespace: service.Namespace,
	}, found)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info(logger, "creating service for aigateway")
			return true, r.Create(ctx, service)
		}

		return false, err
	}

	// TODO - implement patching
	//
	// See: https://github.com/kong/kong-operator/issues/137

	return false, nil
}

// -----------------------------------------------------------------------------
// AIGatewayReconciler - Owned Resource Management
// -----------------------------------------------------------------------------

func (r *AIGatewayReconciler) manageGateway(
	ctx context.Context,
	logger logr.Logger,
	aiGateway *operatorv1alpha1.AIGateway,
) (
	bool, // whether any changes were made
	error,
) {
	change, err := r.createOrUpdateGateway(ctx, logger, aiGatewayToGateway(aiGateway))
	if change {
		return true, err
	}
	if err != nil {
		return change, fmt.Errorf("could not reconcile Gateway, %w", err)
	}

	return change, nil
}

func (r *AIGatewayReconciler) configurePlugins(
	ctx context.Context,
	logger logr.Logger,
	aiGateway *operatorv1alpha1.AIGateway,
) (
	bool, // whether any changes were made
	error,
) {
	changes := false

	log.Trace(logger, "configuring sink service for aigateway")
	aiGatewaySinkService := aiCloudGatewayToKubeSvc(aiGateway)
	changed, err := r.createOrUpdateSvc(ctx, logger, aiGatewaySinkService)
	if changed {
		changes = true
	}
	if err != nil {
		return changes, err
	}

	log.Trace(logger, "retrieving the cloud provider credentials secret for aigateway")
	if aiGateway.Spec.CloudProviderCredentials == nil {
		return changes, fmt.Errorf("ai gateway '%s' requires secret reference for Cloud Provider API keys", aiGateway.Name)
	}
	credentialSecretName := aiGateway.Spec.CloudProviderCredentials.Name
	credentialSecretNamespace := aiGateway.Namespace
	if aiGateway.Spec.CloudProviderCredentials.Namespace != nil {
		credentialSecretNamespace = *aiGateway.Spec.CloudProviderCredentials.Namespace
	}

	// check if referencing the credential secret is allowed by referencegrants.
	msg, allowed, err := secretref.CheckReferenceGrantForSecret(ctx, r.Client, aiGateway, gatewayv1.SecretObjectReference{
		Name:      gatewayv1.ObjectName(credentialSecretName),
		Namespace: lo.ToPtr(gatewayv1.Namespace(credentialSecretNamespace)),
	})
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, fmt.Errorf("Referencing Secret %s/%s is not allowed: %s", credentialSecretNamespace, credentialSecretName, msg)
	}

	credentialSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: credentialSecretNamespace, Name: credentialSecretName}, credentialSecret); err != nil {
		if k8serrors.IsNotFound(err) {
			return changes, nil
		}
		return changes, fmt.Errorf(
			"ai gateway '%s' references secret '%s/%s' but it could not be read, %w",
			aiGateway.Name, credentialSecretNamespace, credentialSecretName, err,
		)
	}

	log.Trace(logger, "generating routes and plugins for aigateway")
	for _, v := range aiGateway.Spec.LargeLanguageModels.CloudHosted {
		cloudHostedLLM := v

		log.Trace(logger, "determining whether we have API keys configured for cloud provider")
		credentialData, ok := credentialSecret.Data[string(cloudHostedLLM.AICloudProvider.Name)]
		if !ok {
			return changes, fmt.Errorf(
				"ai gateway '%s' references provider '%s' but it has no API key stored in the credentials secret",
				aiGateway.Name, string(cloudHostedLLM.AICloudProvider.Name),
			)
		}

		log.Trace(logger, "configuring the base aiproxy plugin for aigateway")
		aiProxyPlugin, err := aiCloudGatewayToKongPlugin(&cloudHostedLLM, aiGateway, &credentialData)
		if err != nil {
			return changes, err
		}
		changed, err := r.createOrUpdatePlugin(ctx, logger, aiProxyPlugin)
		if changed {
			changes = true
		}
		if err != nil {
			return changes, err
		}

		log.Trace(logger, "configuring the ai prompt decorator plugin for aigateway")
		decoratorPlugin, err := aiCloudGatewayToKongPromptDecoratorPlugin(&cloudHostedLLM, aiGateway)
		if err != nil {
			return changes, err
		}
		if decoratorPlugin != nil {
			changed, err := r.createOrUpdatePlugin(ctx, logger, decoratorPlugin)
			if changed {
				changes = true
			}
			if err != nil {
				return changes, err
			}
		}

		log.Trace(logger, "configuring an httproute for aigateway")
		plugins := []string{aiProxyPlugin.Name}
		if decoratorPlugin != nil {
			plugins = append(plugins, decoratorPlugin.Name)
		}
		httpRoute := aiCloudGatewayToHTTPRoute(&cloudHostedLLM, aiGateway, aiGatewaySinkService, plugins)
		changed, err = r.createOrUpdateHTTPRoute(ctx, logger, httpRoute)
		if changed {
			changes = true
		}
		if err != nil {
			return changes, err
		}
	}

	return changes, nil
}
