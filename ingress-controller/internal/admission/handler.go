package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	ctrlref "github.com/kong/kong-operator/ingress-controller/internal/controllers/reference"
	"github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/ingress-controller/internal/labels"
	"github.com/kong/kong-operator/ingress-controller/internal/util"
)

const (
	KindKongPlugin        = "KongPlugin"
	KindKongClusterPlugin = "KongClusterPlugin"
)

// RequestHandler is an HTTP server that can validate Kong Ingress Controllers'
// Custom Resources using Kubernetes Admission Webhooks.
type RequestHandler struct {
	// validators validate the entities that the k8s API-server asks
	// it the server to validate. Each instance is created per KIC
	// instance run in KO. Dispatch methods are responsible for
	// routing incoming requests to the appropriate validator.
	validatorsMut sync.RWMutex
	validators    map[string]KongValidator

	Logger logr.Logger
}

// pickReferenceIndexers returns the first validator's reference indexers if exists.
// Existing of a reference indexers is not guaranteed, so the second return boolean
// value indicates it.
func (h *RequestHandler) pickReferenceIndexers() (ctrlref.CacheIndexers, bool) {
	h.validatorsMut.RLock()
	defer h.validatorsMut.RUnlock()
	if len(h.validators) > 0 {
		return lo.Values(h.validators)[0].GetReferenceIndexers(), true
	}
	return ctrlref.CacheIndexers{}, false
}

// dispatchValidationNoMatcher returns the first validator if exists, without any matching.
// Existing of a reference indexers is not guaranteed, so the second return boolean value
// indicates it.
// There is no specific matching further so it's safe to do it this way, see related entities:
// - h.handleKongPlugin -> ValidatePlugin
// - h.handleKongClusterPlugin -> ValidateClusterPlugin
// - h.handleSecret -> ValidateCredential
// - h.handleGateway -> ValidateGateway
// - h.handleHTTPRoute -> ValidateHTTPRoute
func (h *RequestHandler) dispatchValidationNoMatcher() (KongValidator, bool) {
	h.validatorsMut.RLock()
	defer h.validatorsMut.RUnlock()
	if len(h.validators) > 0 {
		return lo.Values(h.validators)[0], true
	}
	return nil, false
}

// dispatchValidationIngressClassMatcher based on IngressClass returns the first validator if exists.
// Existing of a reference indexers is not guaranteed, so the second return boolean value indicates it.
// On that matching below entities rely:
// - h.handleKongConsumer -> ValidateConsumer
// - h.handleKongConsumerGroup -> ValidateConsumerGroup
// - h.handleKongVault -> ValidateVault
func (h *RequestHandler) dispatchValidationIngressClassMatcher(obj metav1.ObjectMeta) (KongValidator, bool) {
	h.validatorsMut.RLock()
	defer h.validatorsMut.RUnlock()
	for _, v := range h.validators {
		if v.IngressClassMatcher(&obj) {
			return v, true
		}
	}
	// Ignore object that are being not managed by any existing and running controller.
	return nil, false
}

// dispatchValidationForIngress has a specific matching function for Ingress resources. It returns the
// first validator if exists. Existing of a reference indexers is not guaranteed, so the second return
// boolean value indicates it.
// On that matching below entities rely:
// - h.handleIngress-> ValidateIngress
func (h *RequestHandler) dispatchValidationForIngress(ing netv1.Ingress) (KongValidator, bool) {
	h.validatorsMut.RLock()
	defer h.validatorsMut.RUnlock()
	for _, v := range h.validators {
		if v.IngressClassMatcher(&ing.ObjectMeta) ||
			v.IngressV1ClassMatcher(&ing) {
			return v, true
		}
	}
	// Ignore Ingresses that are being managed by any existing and running controller.
	return nil, false
}

// dispatchValidationForCustomEntity has a specific matching function for KongCustomEntity resources.
// It returns the first validator if exists. Existing of a reference indexers is not guaranteed,
// so the second return boolean value indicates it.
// On that matching below entities rely:
// - h.handleKongCustomEntity -> ValidateCustomEntity
func (h *RequestHandler) dispatchValidationForCustomEntity(entity configurationv1alpha1.KongCustomEntity) (KongValidator, bool) {
	h.validatorsMut.RLock()
	defer h.validatorsMut.RUnlock()
	for _, v := range h.validators {
		if v.IngressClassMatcher(&entity.ObjectMeta) ||
			v.IngressClassMatcher(&metav1.ObjectMeta{
				Annotations: map[string]string{
					annotations.IngressClassKey: entity.Spec.ControllerName,
				},
			}) {
			return v, true
		}
	}
	// If the spec.controllerName does not match the ingress class name,
	// and the ingress class annotation does not match the ingress class name either,
	// ignore it as it is not managed by any existing and running controller.
	return nil, false
}

// mgrID is an interface that represents a manager ID.
// manager.ID from ingress-controller/pkg/manager/id.go
// is not used directly to avoid import cycle.
type mgrID interface {
	String() string
}

// RegisterValidator adds a new validator to the request handler. An instance of
// validator is created per KIC instance in KO.
func (h *RequestHandler) RegisterValidator(id mgrID, validator KongValidator) {
	h.validatorsMut.Lock()
	defer h.validatorsMut.Unlock()
	if h.validators == nil {
		h.validators = make(map[string]KongValidator)
	}
	h.validators[id.String()] = validator
}

// UnregisterValidator removes a validator from the request handler.
// An instance of validator is removed when a particular KIC instance
// is removed from KO.
func (h *RequestHandler) UnregisterValidator(id mgrID) {
	h.validatorsMut.Lock()
	defer h.validatorsMut.Unlock()
	delete(h.validators, id.String())
}

// ServeHTTP parses AdmissionReview requests and responds back
// with the validation result of the entity.
func (h *RequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		h.Logger.Error(nil, "Received request with empty body")
		http.Error(w, "Admission review object is missing",
			http.StatusBadRequest)
		return
	}

	review := admissionv1.AdmissionReview{}
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		h.Logger.Error(err, "Failed to decode admission review")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response, err := h.handleValidation(r.Context(), *review.Request)
	if err != nil {
		h.Logger.Error(err, "Failed to run validation")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !response.Allowed {
		h.Logger.Info(
			"Object admission request not allowed",
			"name", review.Request.Name,
			"kind", review.Request.Kind.Kind,
			"namespace", review.Request.Namespace,
			"message", response.Result.Message,
		)
	}

	review.Response = response

	if err := json.NewEncoder(w).Encode(&review); err != nil {
		h.Logger.Error(err, "Failed to encode response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var (
	consumerGVResource = metav1.GroupVersionResource{
		Group:    configurationv1.SchemeGroupVersion.Group,
		Version:  configurationv1.SchemeGroupVersion.Version,
		Resource: "kongconsumers",
	}
	consumerGroupGVResource = metav1.GroupVersionResource{
		Group:    configurationv1beta1.SchemeGroupVersion.Group,
		Version:  configurationv1beta1.SchemeGroupVersion.Version,
		Resource: "kongconsumergroups",
	}
	pluginGVResource = metav1.GroupVersionResource{
		Group:    configurationv1.SchemeGroupVersion.Group,
		Version:  configurationv1.SchemeGroupVersion.Version,
		Resource: "kongplugins",
	}
	clusterPluginGVResource = metav1.GroupVersionResource{
		Group:    configurationv1.SchemeGroupVersion.Group,
		Version:  configurationv1.SchemeGroupVersion.Version,
		Resource: "kongclusterplugins",
	}
	kongVaultGVResource = metav1.GroupVersionResource{
		Group:    configurationv1alpha1.SchemeGroupVersion.Group,
		Version:  configurationv1alpha1.SchemeGroupVersion.Version,
		Resource: "kongvaults",
	}
	kongCustomEntityGVResource = metav1.GroupVersionResource{
		Group:    configurationv1alpha1.SchemeGroupVersion.Group,
		Version:  configurationv1alpha1.SchemeGroupVersion.Version,
		Resource: "kongcustomentities",
	}
	secretGVResource = metav1.GroupVersionResource{
		Group:    corev1.SchemeGroupVersion.Group,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "secrets",
	}
	ingressGVResource = metav1.GroupVersionResource{
		Group:    netv1.SchemeGroupVersion.Group,
		Version:  netv1.SchemeGroupVersion.Version,
		Resource: "ingresses",
	}
	serviceGVResource = metav1.GroupVersionResource{
		Group:    corev1.SchemeGroupVersion.Group,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "services",
	}
)

func (h *RequestHandler) handleValidation(ctx context.Context, request admissionv1.AdmissionRequest) (
	*admissionv1.AdmissionResponse, error,
) {
	responseBuilder := NewResponseBuilder(request.UID)

	switch request.Resource {
	case consumerGVResource:
		return h.handleKongConsumer(ctx, request, responseBuilder)
	case consumerGroupGVResource:
		return h.handleKongConsumerGroup(ctx, request, responseBuilder)
	case pluginGVResource:
		return h.handleKongPlugin(ctx, request, responseBuilder)
	case clusterPluginGVResource:
		return h.handleKongClusterPlugin(ctx, request, responseBuilder)
	case secretGVResource:
		return h.handleSecret(ctx, request, responseBuilder)
	case gatewayapi.V1GatewayGVResource, gatewayapi.V1beta1GatewayGVResource:
		return h.handleGateway(ctx, request, responseBuilder)
	case gatewayapi.V1HTTPRouteGVResource, gatewayapi.V1beta1HTTPRouteGVResource:
		return h.handleHTTPRoute(ctx, request, responseBuilder)
	case kongVaultGVResource:
		return h.handleKongVault(ctx, request, responseBuilder)
	case kongCustomEntityGVResource:
		return h.handleKongCustomEntity(ctx, request, responseBuilder)
	case serviceGVResource:
		return h.handleService(request, responseBuilder)
	case ingressGVResource:
		return h.handleIngress(ctx, request, responseBuilder)
	default:
		return nil, fmt.Errorf("unknown resource type to validate: %s/%s %s",
			request.Resource.Group, request.Resource.Version,
			request.Resource.Resource)
	}
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongconsumers,versions=v1,name=kongconsumers.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongConsumer(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	consumer := configurationv1.KongConsumer{}
	deserializer := codecs.UniversalDeserializer()
	_, _, err := deserializer.Decode(request.Object.Raw, nil, &consumer)
	if err != nil {
		return nil, err
	}
	// ignore consumers that are not having corresponding controller
	v, ok := h.dispatchValidationIngressClassMatcher(consumer.ObjectMeta)
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	switch request.Operation {
	case admissionv1.Create:
		ok, msg, err := v.ValidateConsumer(ctx, consumer)
		if err != nil {
			return nil, err
		}
		return responseBuilder.Allowed(ok).WithMessage(msg).Build(), nil
	case admissionv1.Update:
		var oldConsumer configurationv1.KongConsumer
		_, _, err = deserializer.Decode(request.OldObject.Raw, nil, &oldConsumer)
		if err != nil {
			return nil, err
		}
		// validate only if the username is being changed
		if consumer.Username == oldConsumer.Username {
			return responseBuilder.Allowed(true).Build(), nil
		}
		ok, message, err := v.ValidateConsumer(ctx, consumer)
		if err != nil {
			return nil, err
		}
		return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
	default:
		return nil, fmt.Errorf("unknown operation %q", string(request.Operation))
	}
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongconsumergroups,versions=v1beta1,name=kongconsumergroups.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongConsumerGroup(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	var consumerGroup configurationv1beta1.KongConsumerGroup
	if _, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &consumerGroup); err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationIngressClassMatcher(consumerGroup.ObjectMeta)
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}
	ok, message, err := v.ValidateConsumerGroup(ctx, consumerGroup)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongplugins,versions=v1,name=kongplugins.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongPlugin(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	plugin := configurationv1.KongPlugin{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &plugin)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationNoMatcher()
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	ok, message, err := v.ValidatePlugin(ctx, plugin, nil)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongclusterplugins,versions=v1,name=kongclusterplugins.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongClusterPlugin(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	plugin := configurationv1.KongClusterPlugin{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &plugin)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationNoMatcher()
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	ok, message, err := v.ValidateClusterPlugin(ctx, plugin, nil)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// NOTE this handler _does not_ use a kubebuilder directive, as our Secret handling requires webhook features
// kubebuilder does not support (objectSelector). Instead, config/webhook/additional_secret_hooks.yaml includes
// handwritten webhook rules that we patch into the webhook manifest.
// See https://github.com/kubernetes-sigs/controller-tools/issues/553 for further info.

func (h *RequestHandler) handleSecret(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	secret := corev1.Secret{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &secret)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationNoMatcher()
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	switch request.Operation {
	case admissionv1.Update, admissionv1.Create:
		// credential secrets
		// Run ValidateCredential if the secret has the `konghq.com/credential` label and its value is one of supported credential type.
		if _, err := util.ExtractKongCredentialType(&secret); err == nil {
			ok, message := v.ValidateCredential(ctx, secret)
			if !ok {
				return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
			}
		}

		// TODO https://github.com/Kong/kubernetes-ingress-controller/issues/5876
		// This catch-all block handles Secrets referenced by KongPlugin and KongClusterPlugin configuration. As of 3.2,
		// these Secrets should use a "konghq.com/validate: plugin" label, but the original unfiltered behavior is still
		// supported. It is slated for removal in 4.0. Once it is removed (or if we add additional Secret validation cases
		// other than "plugin") this needs to change to a case that only applies if the valdiate label is present with the
		// "plugin" value, probably using a 'switch validate := secret.Labels[labels.ValidateLabel]; labels.ValidateType(validate)'
		// statement.
		ok, count, message, err := h.checkReferrersOfSecret(ctx, &secret)
		if count > 0 {
			if secret.Labels[labels.ValidateLabel] != string(labels.PluginValidate) {
				h.Logger.Info("Warning: Secret used in Kong(Cluster)Plugin, but missing 'konghq.com/validate: plugin' label."+
					"This label will be required in a future release", "namespace", secret.Namespace, "name", secret.Name)
			}
		}
		if err != nil {
			return responseBuilder.Allowed(false).WithMessage(fmt.Sprintf("failed to validate other objects referencing the secret: %v", err)).Build(), err
		}
		if !ok {
			return responseBuilder.Allowed(false).WithMessage(message).Build(), nil
		}

		// no reference found in the blanket block, this is some random unrelated Secret and KIC should ignore it.
		return responseBuilder.Allowed(true).Build(), nil

	default:
		return nil, fmt.Errorf("unknown operation %q", string(request.Operation))
	}
}

// checkReferrersOfSecret validates all referrers (KongPlugins and KongClusterPlugins) of the secret
// and rejects the secret if it generates invalid configurations for any of the referrers.
func (h *RequestHandler) checkReferrersOfSecret(ctx context.Context, secret *corev1.Secret) (bool, int, string, error) {
	ri, ok := h.pickReferenceIndexers()
	if !ok {
		return true, 0, "", nil
	}
	referrers, err := ri.ListReferrerObjectsByReferent(secret)
	if err != nil {
		return false, 0, "", fmt.Errorf("failed to list referrers of secret: %w", err)
	}

	v, validatorAvailable := h.dispatchValidationNoMatcher()
	count := 0
	for _, obj := range referrers {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Group == configurationv1.GroupVersion.Group && gvk.Version == configurationv1.GroupVersion.Version && gvk.Kind == KindKongPlugin {
			count++
			plugin := obj.(*configurationv1.KongPlugin)
			if validatorAvailable {
				ok, message, err := v.ValidatePlugin(ctx, *plugin, []*corev1.Secret{secret})
				if err != nil {
					return false, count, "", fmt.Errorf("failed to run validation on KongPlugin %s/%s: %w",
						plugin.Namespace, plugin.Name, err,
					)
				}
				if !ok {
					return false, count,
						fmt.Sprintf("Change on secret will generate invalid configuration for KongPlugin %s/%s: %s",
							plugin.Namespace, plugin.Name, message,
						), nil
				}
			}
		}
		if gvk.Group == configurationv1.GroupVersion.Group && gvk.Version == configurationv1.GroupVersion.Version && gvk.Kind == KindKongClusterPlugin {
			count++
			plugin := obj.(*configurationv1.KongClusterPlugin)
			if validatorAvailable {
				ok, message, err := v.ValidateClusterPlugin(ctx, *plugin, []*corev1.Secret{secret})
				if err != nil {
					return false, count, "", fmt.Errorf("failed to run validation on KongClusterPlugin %s: %w",
						plugin.Name, err,
					)
				}
				if !ok {
					return false, count,
						fmt.Sprintf("Change on secret will generate invalid configuration for KongClusterPlugin %s: %s",
							plugin.Name, message,
						), nil
				}
			}
		}
	}
	return true, count, "", nil
}

// +kubebuilder:webhook:verbs=create;update,groups=gateway.networking.k8s.io,resources=gateways,versions=v1;v1beta1,name=gateways.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleGateway(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	gateway := gatewayapi.Gateway{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &gateway)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationNoMatcher()
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	ok, message, err := v.ValidateGateway(ctx, gateway)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=gateway.networking.k8s.io,resources=httproutes,versions=v1;v1beta1,name=httproutes.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleHTTPRoute(
	ctx context.Context,
	request admissionv1.AdmissionRequest,
	responseBuilder *ResponseBuilder,
) (*admissionv1.AdmissionResponse, error) {
	httproute := gatewayapi.HTTPRoute{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &httproute)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationNoMatcher()
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}
	ok, message, err := v.ValidateHTTPRoute(ctx, httproute)
	if err != nil {
		return nil, err
	}
	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

const (
	serviceWarning = "%s is deprecated and will be removed in a future release. Use Service annotations " +
		"for the 'proxy' section and %s with a KongUpstreamPolicy resource instead."
)

// +kubebuilder:webhook:verbs=create;update,groups=core,resources=services,versions=v1,name=services.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleService(request admissionv1.AdmissionRequest, responseBuilder *ResponseBuilder) (*admissionv1.AdmissionResponse, error) {
	service := corev1.Service{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &service)
	if err != nil {
		return nil, err
	}

	if annotations.ExtractConfigurationName(service.Annotations) != "" {
		warning := fmt.Sprintf(serviceWarning, annotations.AnnotationPrefix+annotations.ConfigurationKey,
			configurationv1beta1.KongUpstreamPolicyAnnotationKey)

		responseBuilder = responseBuilder.WithWarning(warning)
	}

	return responseBuilder.Allowed(true).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=networking.k8s.io,resources=ingresses,versions=v1,name=ingresses.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleIngress(ctx context.Context, request admissionv1.AdmissionRequest, responseBuilder *ResponseBuilder) (*admissionv1.AdmissionResponse, error) {
	ingress := netv1.Ingress{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &ingress)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationForIngress(ingress)
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}
	ok, message, err := v.ValidateIngress(ctx, ingress)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongvaults,versions=v1alpha1,name=kongvaults.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongVault(ctx context.Context, request admissionv1.AdmissionRequest, responseBuilder *ResponseBuilder) (*admissionv1.AdmissionResponse, error) {
	kongVault := configurationv1alpha1.KongVault{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &kongVault)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationIngressClassMatcher(kongVault.ObjectMeta)
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}
	ok, message, err := v.ValidateVault(ctx, kongVault)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}

// +kubebuilder:webhook:verbs=create;update,groups=configuration.konghq.com,resources=kongcustomentities,versions=v1alpha1,name=kongcustomentities.validation.ingress-controller.konghq.com,path=/,webhookVersions=v1,matchPolicy=equivalent,mutating=false,failurePolicy=fail,sideEffects=None,admissionReviewVersions=v1

func (h *RequestHandler) handleKongCustomEntity(ctx context.Context, request admissionv1.AdmissionRequest, responseBuilder *ResponseBuilder) (*admissionv1.AdmissionResponse, error) {
	kongCustomEntity := configurationv1alpha1.KongCustomEntity{}
	_, _, err := codecs.UniversalDeserializer().Decode(request.Object.Raw, nil, &kongCustomEntity)
	if err != nil {
		return nil, err
	}
	v, ok := h.dispatchValidationForCustomEntity(kongCustomEntity)
	if !ok {
		return responseBuilder.Allowed(true).Build(), nil
	}

	ok, message, err := v.ValidateCustomEntity(ctx, kongCustomEntity)
	if err != nil {
		return nil, err
	}

	return responseBuilder.Allowed(ok).WithMessage(message).Build(), nil
}
