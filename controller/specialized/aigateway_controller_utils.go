package specialized

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/metadata"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ----------------------------------------------------------------------------
// AIGateway - AI Inference Authentication
// ----------------------------------------------------------------------------

var (
	aiCloudPRoviderAuthHeaderMapInitializer sync.Once
	aiCloudProviderAuthHeaders              map[operatorv1alpha1.AICloudProviderName]map[string]string
)

func getAuthHeaderForInference(provider operatorv1alpha1.AICloudProvider) (map[string]string, error) {
	aiCloudPRoviderAuthHeaderMapInitializer.Do(func() {
		aiCloudProviderAuthHeaders = map[operatorv1alpha1.AICloudProviderName]map[string]string{
			operatorv1alpha1.AICloudProviderOpenAI: {
				"HeaderName":    "Authorization",
				"HeaderPattern": "Bearer %s",
			},
			operatorv1alpha1.AICloudProviderCohere: {
				"HeaderName":    "Authorization",
				"HeaderPattern": "Bearer %s",
			},
			operatorv1alpha1.AICloudProviderAzure: {
				"HeaderName":    "api-key",
				"HeaderPattern": "%s",
			},
			operatorv1alpha1.AICloudProviderMistral: {
				"HeaderName":    "Authorization",
				"HeaderPattern": "Bearer %s",
			},
		}
	})

	headerInfo, ok := aiCloudProviderAuthHeaders[provider.Name]
	if !ok {
		return nil, fmt.Errorf("%s is not a valid provider (valid providers: %+v)", provider, aiCloudProviderAuthHeaders)
	}

	return maps.Clone(headerInfo), nil
}

// ----------------------------------------------------------------------------
// AIGateway - Generators
// ----------------------------------------------------------------------------

// aiGatewayToGateway takes an accepted/validated v1alpha1.AIGateway struct and produces a v1.Gateway (k8sig resources)
// and a v1beta1.GatewayConfiguration (kong extensions) that will host the Large Language Model deployments
func aiGatewayToGateway(
	aigateway *operatorv1alpha1.AIGateway,
) *gatewayv1.Gateway {
	gateway := &gatewayv1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: gatewayv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigateway.Name,
			Namespace: aigateway.Namespace,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(aigateway.Spec.GatewayClassName),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Protocol: "HTTP",
					Port:     gatewayv1.PortNumber(AIGatewayEgressServicePort),
				},
			},
		},
	}

	// Hierarchy is: AIGateway owns the Gateway
	k8sutils.SetOwnerForObject(gateway, aigateway)

	return gateway
}

// aiCloudGatewayToDecoratorPlugin take an accepted/validated vXalphaY.CloudHostedLargeLanguageModel struct
// and produces an ai-prompt-decorator vX.KongPlugin if required
func aiCloudGatewayToKongPromptDecoratorPlugin(
	aiCloudGateway *operatorv1alpha1.CloudHostedLargeLanguageModel,
	aigateway *operatorv1alpha1.AIGateway,
) (*configurationv1.KongPlugin, error) {
	var thisDecoratorPlugin *configurationv1.KongPlugin

	if len(aiCloudGateway.DefaultPrompts) > 0 {
		thisPluginConfig := AICloudPromptDecoratorConfig{
			&AICloudPromptDecoratorPrompts{
				Prepend: aiCloudGateway.DefaultPrompts,
			},
		}

		thisPluginConfBytes, err := json.Marshal(&thisPluginConfig)
		if err != nil {
			return nil, fmt.Errorf(
				"ai cloud gateway with Identifier '%s' resource could not be parsed into a ai-prompt-decorator KongPlugin configuration, check object",
				aiCloudGateway.Identifier,
			)
		}

		thisDecoratorPlugin = &configurationv1.KongPlugin{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongPlugin",
				APIVersion: configurationv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-ai-prompt-decorator", aiCloudGateway.Identifier),
				Namespace: aigateway.Namespace,
			},

			PluginName:   "ai-prompt-decorator",
			Protocols:    configurationv1.StringsToKongProtocols([]string{"http", "https"}),
			InstanceName: fmt.Sprintf("%s-ai-prompt-decorator", aiCloudGateway.Identifier),
			Config: v1.JSON{
				Raw: thisPluginConfBytes,
			},
		}

		k8sutils.SetOwnerForObject(thisDecoratorPlugin, aigateway)

		return thisDecoratorPlugin, nil
	}

	return nil, nil
}

// aiCloudGatewayToKubeSvc take an accepted/validated vXalphaY.CloudHostedLargeLanguageModel struct
// and produces a Kubernetes Service, used for a "sink" to ensure all HttpRoutes and KongPlugins
// actually get created in the Kong gateway.
// A later revision of the whole stack, will probably remove this necessity.
func aiCloudGatewayToKubeSvc(aiGateway *operatorv1alpha1.AIGateway) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ai-sink", aiGateway.Name),
			Namespace: aiGateway.Namespace,
			Annotations: map[string]string{
				"konghq.com/protocol": "https",
				"konghq.com/retries":  "1",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "localhost",
			Ports: []corev1.ServicePort{
				{
					Name:       "proxy-sink",
					Protocol:   "TCP",
					Port:       int32(AIGatewayEgressServicePort),
					TargetPort: intstr.FromInt(AIGatewayEgressServicePort),
				},
			},
		},
	}

	k8sutils.SetOwnerForObject(svc, aiGateway)

	return svc
}

// aiCloudGatewayToHTTPRoute takes an AIGateway, and a CloudHostedLargeLanguageModel,
// and produces an HTTPRoute that will become the egress point for this provider/model combo.
func aiCloudGatewayToHTTPRoute(
	aiCloudLLM *operatorv1alpha1.CloudHostedLargeLanguageModel,
	aigateway *operatorv1alpha1.AIGateway,
	kubeSvc *corev1.Service,
	plugins []string,
) *gatewayv1.HTTPRoute {
	backendKind := "Service"
	matchType := "Exact"
	exactPath := fmt.Sprintf("/%s", aiCloudLLM.Identifier)

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-egress", aiCloudLLM.Identifier),
			Namespace: aigateway.Namespace,
			Annotations: map[string]string{
				metadata.AnnotationKeyPlugins: strings.Join(plugins, ","),
			},
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      gatewayv1.ObjectName(aigateway.Name),
						Namespace: (*gatewayv1.Namespace)(&aigateway.Namespace),
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  (*gatewayv1.PathMatchType)(&matchType),
								Value: &exactPath,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name:      gatewayv1.ObjectName(kubeSvc.Name),
									Namespace: (*gatewayv1.Namespace)(&kubeSvc.Namespace),
									Port:      (&kubeSvc.Spec.Ports[0].Port), // only one port for AI ExternalNameSvc, is predictable
									Kind:      (*gatewayv1.Kind)(&backendKind),
								},
							},
						},
					},
				},
			},
		},
	}

	k8sutils.SetOwnerForObject(httpRoute, aigateway)

	return httpRoute
}

// aiCloudGatewayToKongPlugin takes an accepted/validated vXalphaY.CloudHostedLargeLanguageModel struct
// and transforms it into a vX.KongPlugin from Kong Kubernetes-Ingress-Controller
func aiCloudGatewayToKongPlugin(
	aiCloudLLM *operatorv1alpha1.CloudHostedLargeLanguageModel,
	aigateway *operatorv1alpha1.AIGateway,
	credentialData *[]byte,
) (*configurationv1.KongPlugin, error) {
	providerName := string(aiCloudLLM.AICloudProvider.Name)
	routeType := ""
	switch *aiCloudLLM.PromptType {
	case operatorv1alpha1.LLMPromptTypeChat:
		routeType = "llm/v1/chat"

	case operatorv1alpha1.LLMPromptTypeCompletion:
		routeType = "llm/v1/completions"

	default:
		return nil, fmt.Errorf(
			"ai cloud gateway with Identifier '%s' uses prompt type '%s' but it is not yet supported",
			aiCloudLLM.Identifier,
			string(*aiCloudLLM.PromptType))
	}

	// Find and parse the auth header format
	authHeader, err := getAuthHeaderForInference(aiCloudLLM.AICloudProvider)
	if err != nil {
		return nil, fmt.Errorf(
			"ai cloud gateway with Identifier '%s' does not have auth header info defined, %w",
			aiCloudLLM.Identifier,
			err)
	}
	authHeaderName := authHeader["HeaderName"]
	authHeaderValue := fmt.Sprintf(authHeader["HeaderPattern"], string(*credentialData))

	thisAIProxyPluginConfig := AICloudProviderLLMConfig{
		RouteType: &routeType,
		Auth: &AICloudProviderAuthConfig{
			HeaderName:  &authHeaderName,
			HeaderValue: &authHeaderValue,
		},
		Logging: &AICloudProviderLoggingConfig{
			// TODO make sure this is set(table) in AICloudGateway declaration
			LogStatistics: true,
			LogPayloads:   false,
		},
		Model: &AICloudProviderModelConfig{
			Provider: &providerName,
			Name:     aiCloudLLM.Model,
			Options:  &AICloudProviderOptionsConfig{},
		},
	}

	// Auxiliary config options for model tuning
	if aiCloudLLM.DefaultPromptParams != nil {
		thisAIProxyPluginConfig.Model.Options.MaxTokens = aiCloudLLM.DefaultPromptParams.MaxTokens
		thisAIProxyPluginConfig.Model.Options.Temperature = aiCloudLLM.DefaultPromptParams.Temperature
	}

	thisAIProxyPluginConfigJSON, err := json.Marshal(&thisAIProxyPluginConfig)
	if err != nil {
		return nil, fmt.Errorf(
			"ai cloud gateway with Identifier '%s' resource could not be parsed into a KongPlugin configuration, check object",
			aiCloudLLM.Identifier)
	}

	thisAIProxyPlugin := configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: configurationv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ai-proxy", aiCloudLLM.Identifier),
			Namespace: aigateway.Namespace,
		},

		PluginName:   "ai-proxy",
		Protocols:    configurationv1.StringsToKongProtocols([]string{"http", "https"}),
		InstanceName: fmt.Sprintf("%s-ai-proxy", aiCloudLLM.Identifier),
		Config: v1.JSON{
			Raw: thisAIProxyPluginConfigJSON,
		},
	}

	k8sutils.SetOwnerForObject(&thisAIProxyPlugin, aigateway)

	return &thisAIProxyPlugin, nil
}
