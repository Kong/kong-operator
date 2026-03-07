package converter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	kcfggateway "github.com/kong/kong-operator/v2/api/gateway-operator/gateway"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	secretref "github.com/kong/kong-operator/v2/controller/pkg/secrets/ref"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

var _ APIConverter[gwtypes.Gateway] = &gatewayConverter{}
var _ OrphanedResourceHandler = &gatewayConverter{}

// gatewayConverter is a concrete implementation of the APIConverter interface for Gateway.
type gatewayConverter struct {
	client.Client

	gateway         *gwtypes.Gateway
	controlPlaneRef *commonv1alpha1.ControlPlaneRef
	outputStore     []client.Object
	expectedGVKs    []schema.GroupVersionKind
}

// newGatewayConverter returns a new instance of gatewayConverter.
func newGatewayConverter(gateway *gwtypes.Gateway, cl client.Client) APIConverter[gwtypes.Gateway] {
	return &gatewayConverter{
		Client:      cl,
		gateway:     gateway,
		outputStore: []client.Object{},
		expectedGVKs: []schema.GroupVersionKind{
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongCertificate"},
			{Group: configurationv1alpha1.GroupVersion.Group, Version: configurationv1alpha1.GroupVersion.Version, Kind: "KongSNI"},
		},
	}
}

// GetRootObject implements APIConverter.
//
// Returns the Gateway resource that this converter is managing.
func (c *gatewayConverter) GetRootObject() gwtypes.Gateway {
	return *c.gateway
}

// Translate implements APIConverter.
//
// Performs the translation of a Gateway resource into Kong-specific resources.
// This is the main entry point for the conversion process.
//
// For each TLS listener in the Gateway:
//   - Creates a KongCertificate from the referenced Secret
//   - Creates a KongSNI resource for the listener's hostname
//
// If errors occur while processing individual listeners, they are accumulated and
// returned as a joined error. Successfully processed listeners will still have their
// Kong resources created in the output store.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//
// Returns:
//   - int: Number of Kong resources created during translation
//   - error: Accumulated errors from listener processing, or nil if all successful
func (c *gatewayConverter) Translate(ctx context.Context, logger logr.Logger) (int, error) {
	logger = logger.WithValues("phase", "gateway-translate")
	log.Debug(logger, "Starting Gateway translation")

	// Check if the gateway is handled by this controller.
	// It could happen when the GatewayClass is changed to an unsupported one.
	// This check prevents the translation from proceeding in such cases and allows
	// the reconciler to clean up any previously created resources.
	isInKonnect, err := refs.IsGatewayInKonnect(ctx, c.Client, c.gateway)
	if err != nil {
		return 0, fmt.Errorf("failed to check if Gateway is supported: %w", err)
	}

	if !isInKonnect {
		log.Debug(logger, "Gateway is not supported by this controller, skipping translation", "gateway", client.ObjectKeyFromObject(c.gateway))
		return 0, nil
	}

	// Get the ControlPlaneRef for this Gateway from its KonnectExtension.
	controlPlaneRef, err := refs.GetControlPlaneRefByGateway(ctx, c.Client, c.gateway)
	if err != nil {
		return 0, fmt.Errorf("failed to get ControlPlaneRef for Gateway: %w", err)
	}
	c.controlPlaneRef = controlPlaneRef
	// Clear namespace to indicate same namespace as Gateway. Currently, cross-namespace references are not supported.
	c.controlPlaneRef.KonnectNamespacedRef.Namespace = ""

	var listenerErrors []error

	for i := range c.gateway.Spec.Listeners {
		listener := &c.gateway.Spec.Listeners[i]

		// Only process listeners with TLS configuration.
		if listener.TLS == nil {
			log.Debug(logger, "Skipping listener without TLS config", "listener", listener)
			continue
		}

		// Process each certificate reference in the listener
		for _, certRef := range listener.TLS.CertificateRefs {
			if err := c.processListenerCertificate(ctx, logger, listener, certRef); err != nil {
				listenerErr := fmt.Errorf("failed to process certificate for listener %s (port %d): %w", listener.Name, listener.Port, err)
				listenerErrors = append(listenerErrors, listenerErr)
				log.Error(logger, err, "Failed to process listener certificate",
					"listener", listener.Name,
					"port", listener.Port,
					"certificateRef", certRef.Name)
			}
		}
	}

	// Return accumulated errors if any occurred
	if len(listenerErrors) > 0 {
		return len(c.outputStore), fmt.Errorf("failed to process %d listener(s): %w", len(listenerErrors), errors.Join(listenerErrors...))
	}

	return len(c.outputStore), nil
}

// GetOutputStore implements APIConverter.
//
// Converts all objects in the outputStore to unstructured format for use by the caller.
// Each typed object (KongCertificate, KongSNI) is converted to unstructured.Unstructured
// using the runtime scheme. If any conversions fail, partial results are returned along
// with an aggregated error containing all conversion failures.
//
// Parameters:
//   - ctx: The context for the conversion operation
//   - logger: Logger for structured logging
//
// Returns:
//   - []unstructured.Unstructured: Converted objects (may be partial if errors occurred)
//   - error: Aggregated conversion errors or nil if all conversions succeeded
func (c *gatewayConverter) GetOutputStore(ctx context.Context, logger logr.Logger) ([]unstructured.Unstructured, error) {
	logger = logger.WithValues("phase", "output-store-conversion")
	log.Debug(logger, "Starting output store conversion")

	var conversionErrors []error

	objects := make([]unstructured.Unstructured, 0, len(c.outputStore))
	for _, obj := range c.outputStore {
		unstr, err := utils.ToUnstructured(obj, c.Scheme())
		if err != nil {
			conversionErr := fmt.Errorf("failed to convert %T %s to unstructured: %w", obj, obj.GetName(), err)
			conversionErrors = append(conversionErrors, conversionErr)
			log.Error(logger, err, "Failed to convert object to unstructured",
				"objectName", obj.GetName())
			continue
		}
		objects = append(objects, unstr)
	}

	// Check if any conversion errors occurred and return aggregated error.
	if len(conversionErrors) > 0 {
		log.Error(logger, nil, "Output store conversion completed with errors",
			"totalObjectsAttempted", len(c.outputStore),
			"successfulConversions", len(objects),
			"conversionErrors", len(conversionErrors))

		// Join all errors using errors.Join for better error handling.
		return objects, fmt.Errorf("output store conversion failed with %d errors: %w", len(conversionErrors), errors.Join(conversionErrors...))
	}

	log.Debug(logger, "Successfully converted all objects in output store",
		"totalObjectsConverted", len(objects))

	return objects, nil
}

// GetExpectedGVKs returns the list of GroupVersionKinds that this converter expects to manage for Gateway resources.
func (c *gatewayConverter) GetExpectedGVKs() []schema.GroupVersionKind {
	return c.expectedGVKs
}

// UpdateRootObjectStatus updates the status of the Gateway resource.
//
// Parameters:
//   - ctx: The context for the operation, used for cancellation and timeouts
//   - logger: A logger instance for recording operational information and errors
//
// Returns:
//   - updated: true if the status was modified
//   - stop: true if reconciliation should halt
//   - err: any error encountered during status update processing
func (c *gatewayConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (updated bool, stop bool, err error) {
	logger = logger.WithValues("phase", "gateway-status")
	log.Debug(logger, "Starting UpdateRootObjectStatus")

	oldStatus := c.gateway.Status

	if err := c.setGatewayStatus(ctx); err != nil {
		return false, false, fmt.Errorf("failed to build gateway status: %w", err)
	}

	if equality.Semantic.DeepEqual(oldStatus, c.gateway.Status) {
		log.Debug(logger, "No status update required for Gateway")
		return false, false, nil
	}

	log.Debug(logger, "Updating Gateway status in cluster", "status", c.gateway.Status)
	if err := c.Status().Update(ctx, c.gateway); err != nil {
		if apierrors.IsConflict(err) {
			return false, true, err
		}
		return false, false, fmt.Errorf("failed to update Gateway status: %w", err)
	}

	log.Debug(logger, "Finished UpdateRootObjectStatus", "updated", true)
	return true, false, nil
}

type gatewayConditionsAndListenersAware struct {
	*gwtypes.Gateway
}

// GetConditions returns the conditions of the Gateway status.
func (g gatewayConditionsAndListenersAware) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

// SetConditions sets the conditions of the Gateway status.
func (g gatewayConditionsAndListenersAware) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

// GetListenersConditions returns the listener conditions of the Gateway status.
func (g gatewayConditionsAndListenersAware) GetListenersConditions() []gatewayv1.ListenerStatus {
	return g.Status.Listeners
}

// SetListenersConditions sets the listener conditions of the Gateway status.
func (g gatewayConditionsAndListenersAware) SetListenersConditions(listeners []gatewayv1.ListenerStatus) {
	g.Status.Listeners = listeners
}

type listenerConditionsAware struct {
	*gatewayv1.ListenerStatus
}

// GetConditions returns the conditions of the Listener status.
func (l listenerConditionsAware) GetConditions() []metav1.Condition {
	return l.Conditions
}

// SetConditions sets the conditions of the Listener status.
func (l listenerConditionsAware) SetConditions(conditions []metav1.Condition) {
	l.Conditions = conditions
}

func (c *gatewayConverter) setGatewayStatus(ctx context.Context) error {
	existingStatuses := make(map[gatewayv1.SectionName]gatewayv1.ListenerStatus, len(c.gateway.Status.Listeners))
	for _, status := range c.gateway.Status.Listeners {
		existingStatuses[status.Name] = status
	}

	listenerStatuses := make([]gatewayv1.ListenerStatus, 0, len(c.gateway.Spec.Listeners))
	for _, listener := range c.gateway.Spec.Listeners {
		listenerStatus := existingStatuses[listener.Name]
		listenerStatus.Name = listener.Name
		listenerStatus.AttachedRoutes = 0

		supportedKinds, resolvedRefsCondition, err := c.getSupportedKindsWithResolvedRefsCondition(ctx, listener)
		if err != nil {
			return fmt.Errorf("failed to build listener status for %s: %w", listener.Name, err)
		}
		listenerStatus.SupportedKinds = supportedKinds

		acceptedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonAccepted),
			Message:            "Listener is accepted.",
			ObservedGeneration: c.gateway.Generation,
			LastTransitionTime: metav1.Now(),
		}
		if !hybridGatewayListenerProtocolSupported(listener.Protocol) {
			acceptedCondition.Status = metav1.ConditionFalse
			acceptedCondition.Reason = string(gatewayv1.ListenerReasonUnsupportedProtocol)
			acceptedCondition.Message = fmt.Sprintf("Protocol %s is not supported.", listener.Protocol)
		}

		listenerAware := listenerConditionsAware{ListenerStatus: &listenerStatus}
		k8sutils.SetCondition(acceptedCondition, listenerAware)
		k8sutils.SetCondition(resolvedRefsCondition, listenerAware)

		programmedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonProgrammed),
			Message:            "Listener is programmed.",
			ObservedGeneration: c.gateway.Generation,
			LastTransitionTime: metav1.Now(),
		}
		if acceptedCondition.Status != metav1.ConditionTrue || resolvedRefsCondition.Status != metav1.ConditionTrue {
			programmedCondition.Status = metav1.ConditionFalse
			programmedCondition.Reason = string(gatewayv1.ListenerReasonPending)
			programmedCondition.Message = "Listener is not ready for programming."
		}
		k8sutils.SetCondition(programmedCondition, listenerAware)

		listenerStatuses = append(listenerStatuses, listenerStatus)
	}

	c.gateway.Status.Listeners = listenerStatuses

	gatewayAware := gatewayConditionsAndListenersAware{Gateway: c.gateway}
	k8sutils.SetAcceptedConditionOnGateway(gatewayAware)
	k8sutils.SetCondition(c.buildGatewayProgrammedCondition(), gatewayAware)

	return nil
}

func (c *gatewayConverter) buildGatewayProgrammedCondition() metav1.Condition {
	condition := metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonProgrammed),
		Message:            "All listeners are programmed.",
		ObservedGeneration: c.gateway.Generation,
		LastTransitionTime: metav1.Now(),
	}

	for _, listenerStatus := range c.gateway.Status.Listeners {
		programmed := false
		for _, cond := range listenerStatus.Conditions {
			if cond.Type == string(gatewayv1.ListenerConditionProgrammed) {
				programmed = cond.Status == metav1.ConditionTrue
				break
			}
		}
		if !programmed {
			condition.Status = metav1.ConditionFalse
			condition.Reason = string(gatewayv1.GatewayReasonPending)
			condition.Message = "At least one listener is not programmed."
			break
		}
	}

	return condition
}

func (c *gatewayConverter) getSupportedKindsWithResolvedRefsCondition(ctx context.Context, listener gwtypes.Listener) ([]gatewayv1.RouteGroupKind, metav1.Condition, error) {
	supportedKinds := make([]gatewayv1.RouteGroupKind, 0)
	resolvedRefsCondition := metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
		Message:            "Listeners' references are accepted.",
		ObservedGeneration: c.gateway.Generation,
		LastTransitionTime: metav1.Now(),
	}

	message := ""
	if listener.TLS != nil {
		if listener.TLS.Mode != nil && *listener.TLS.Mode != gatewayv1.TLSModeTerminate {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
			message = conditionMessage(message, "Only Terminate mode is supported")
		}

		if len(listener.TLS.CertificateRefs) != 1 {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(kcfggateway.ListenerReasonTooManyTLSSecrets)
			message = conditionMessage(message, "Only one certificate per listener is supported")
		} else {
			certificateRef := listener.TLS.CertificateRefs[0]
			secretref.EnsureNamespaceInSecretRef(&certificateRef, gatewayv1.Namespace(c.gateway.Namespace))

			if err := secretref.DoesFieldReferenceCoreV1Secret(certificateRef, "CertificateRef"); err != nil {
				resolvedRefsCondition.Status = metav1.ConditionFalse
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
				message = conditionMessage(message, err.Error())
			} else {
				whyNotGranted, isGranted, err := secretref.CheckReferenceGrantForSecret(ctx, c.Client, c.gateway, certificateRef)
				if err != nil {
					return nil, metav1.Condition{}, fmt.Errorf("failed to resolve certificate reference: %w", err)
				}
				if !isGranted {
					resolvedRefsCondition.Status = metav1.ConditionFalse
					resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonRefNotPermitted)
					message = conditionMessage(message, whyNotGranted)
				} else {
					secret := &corev1.Secret{}
					if err := c.Get(ctx, types.NamespacedName{Namespace: string(*certificateRef.Namespace), Name: string(certificateRef.Name)}, secret); err != nil {
						if !apierrors.IsNotFound(err) {
							return nil, metav1.Condition{}, fmt.Errorf("failed to get Secret: %w", err)
						}
						resolvedRefsCondition.Status = metav1.ConditionFalse
						resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
						message = conditionMessage(message, fmt.Sprintf("Referenced secret %s/%s does not exist", *certificateRef.Namespace, certificateRef.Name))
					} else if !secrets.IsTLSSecretValid(secret) {
						resolvedRefsCondition.Status = metav1.ConditionFalse
						resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
						message = conditionMessage(message, "Referenced secret does not contain a valid TLS certificate")
					}
				}
			}
		}
	}

	if listener.AllowedRoutes == nil || len(listener.AllowedRoutes.Kinds) == 0 {
		supportedKinds = defaultSupportedKindsForProtocol(listener.Protocol)
	} else {
		validKinds := validRouteKindsForProtocol(listener.Protocol)
		for _, routeGK := range listener.AllowedRoutes.Kinds {
			if routeGK.Group == nil || *routeGK.Group != gatewayv1.Group(gatewayv1.GroupVersion.Group) {
				resolvedRefsCondition.Status = metav1.ConditionFalse
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidRouteKinds)
				message = conditionMessage(message, fmt.Sprintf("Route %s not supported", routeGK.Kind))
				continue
			}
			if _, ok := validKinds[routeGK.Kind]; !ok {
				resolvedRefsCondition.Status = metav1.ConditionFalse
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidRouteKinds)
				message = conditionMessage(message, fmt.Sprintf("Route %s not supported", routeGK.Kind))
				continue
			}

			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: routeGK.Group,
				Kind:  routeGK.Kind,
			})
		}
	}

	if resolvedRefsCondition.Status == metav1.ConditionFalse {
		resolvedRefsCondition.Message = message
	}

	return supportedKinds, resolvedRefsCondition, nil
}

func hybridGatewayListenerProtocolSupported(protocol gatewayv1.ProtocolType) bool {
	_, ok := validRouteKindsForProtocol(protocol)[gatewayv1.Kind("HTTPRoute")]
	return ok
}

func validRouteKindsForProtocol(protocol gatewayv1.ProtocolType) map[gatewayv1.Kind]struct{} {
	switch protocol {
	case gatewayv1.HTTPProtocolType, gatewayv1.HTTPSProtocolType:
		return map[gatewayv1.Kind]struct{}{"HTTPRoute": {}}
	default:
		return map[gatewayv1.Kind]struct{}{}
	}
}

func defaultSupportedKindsForProtocol(protocol gatewayv1.ProtocolType) []gatewayv1.RouteGroupKind {
	validKinds := validRouteKindsForProtocol(protocol)
	supportedKinds := make([]gatewayv1.RouteGroupKind, 0, len(validKinds))
	for kind := range validKinds {
		group := gatewayv1.Group(gatewayv1.GroupVersion.Group)
		supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
			Group: &group,
			Kind:  kind,
		})
	}
	return supportedKinds
}

func conditionMessage(oldStr, newStr string) string {
	if len(newStr) > 0 && !strings.HasSuffix(newStr, ".") {
		newStr += "."
	}
	if oldStr == "" {
		return newStr
	}
	return fmt.Sprintf("%s %s", oldStr, newStr)
}

// HandleOrphanedResource implements OrphanedResourceHandler.
//
// Determines whether an orphaned resource should be deleted or preserved during cleanup.
// Resources are only deleted if they are owned by this specific Gateway instance.
// This prevents accidental deletion of resources that may be owned by other Gateways.
//
// Parameters:
//   - ctx: The context for the operation
//   - logger: Logger for structured logging
//   - resource: The orphaned resource to evaluate
//
// Returns:
//   - skipDelete: true if the resource should be preserved, false if it should be deleted
//   - err: any error encountered during evaluation
func (c *gatewayConverter) HandleOrphanedResource(ctx context.Context, logger logr.Logger, resource *unstructured.Unstructured) (skipDelete bool, err error) {
	// Check if the resource is owned by this gateway.
	if k8sutils.IsOwnedByRefUID(resource, c.gateway.UID) {
		// Resource is owned by this gateway, allow deletion.
		log.Debug(logger, "Resource is owned by this gateway, allowing deletion",
			"resource", resource.GetName(),
			"kind", resource.GetKind(),
			"gateway", c.gateway.Name)
		return false, nil
	}

	// Resource is not owned by this gateway, skip deletion
	log.Debug(logger, "Resource is not owned by this gateway, skipping deletion",
		"resource", resource.GetName(),
		"kind", resource.GetKind(),
		"gateway", c.gateway.Name)
	return true, nil
}

// processListenerCertificate processes a certificate reference from a listener,
// creating a KongCertificate and associated KongSNI resources.
func (c *gatewayConverter) processListenerCertificate(
	ctx context.Context,
	logger logr.Logger,
	listener *gwtypes.Listener,
	certRef gatewayv1.SecretObjectReference,
) error {
	// Validate that the certificate reference is for a core/v1 Secret.
	if certRef.Group != nil && string(*certRef.Group) != corev1.GroupName && string(*certRef.Group) != "" {
		log.Debug(logger, "Skipping certificate reference with unsupported group",
			"listener", listener.Name,
			"group", *certRef.Group)
		return nil
	}

	if certRef.Kind != nil && string(*certRef.Kind) != "Secret" {
		log.Debug(logger, "Skipping certificate reference with unsupported kind",
			"listener", listener.Name,
			"kind", *certRef.Kind)
		return nil
	}

	// Determine the namespace for the secret reference.
	secretNamespace := c.gateway.Namespace
	if certRef.Namespace != nil {
		secretNamespace = string(*certRef.Namespace)
	}

	// Ensure namespace is set in the certRef for validation.
	certRefWithNamespace := certRef
	if certRefWithNamespace.Namespace == nil {
		ns := gatewayv1.Namespace(secretNamespace)
		certRefWithNamespace.Namespace = &ns
	}

	// Check if the Gateway is allowed to reference the Secret via ReferenceGrants.
	whyNotGranted, isGranted, err := secretref.CheckReferenceGrantForSecret(ctx, c.Client, c.gateway, certRefWithNamespace)
	if err != nil {
		return fmt.Errorf("failed to check ReferenceGrant for secret %s/%s: %w", secretNamespace, certRef.Name, err)
	}
	if !isGranted {
		log.Info(logger, "Skipping certificate reference not permitted by ReferenceGrant",
			"listener", listener.Name,
			"secret", fmt.Sprintf("%s/%s", secretNamespace, certRef.Name),
			"reason", whyNotGranted)
		return nil
	}

	// Verify the Secret exists and is a valid TLS secret.
	secret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: string(certRef.Name)}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug(logger, "Skipping certificate reference to non-existent secret",
				"listener", listener.Name,
				"secret", fmt.Sprintf("%s/%s", secretNamespace, certRef.Name))
			return nil
		}
		return fmt.Errorf("failed to get secret %s/%s: %w", secretNamespace, certRef.Name, err)
	}

	// Validate that the secret contains valid TLS certificate data.
	if !secrets.IsTLSSecretValid(secret) {
		return fmt.Errorf("invalid TLS secret %s/%s for listener %+v", secretNamespace, certRef.Name, listener)
	}

	// Create the KongCertificate resource.
	kongCert, err := c.buildKongCertificate(listener, certRef, secretNamespace)
	if err != nil {
		return fmt.Errorf("failed to build KongCertificate: %w", err)
	}
	c.outputStore = append(c.outputStore, &kongCert)

	// Create KongSNI resource for the listener's hostname.
	kongSNI, err := c.buildKongSNI(listener, &kongCert)
	if err != nil {
		return fmt.Errorf("failed to build KongSNI: %w", err)
	}
	c.outputStore = append(c.outputStore, &kongSNI)

	log.Debug(logger, "Successfully processed TLS certificate for listener",
		"listener", listener,
		"secret", fmt.Sprintf("%s/%s", secretNamespace, certRef.Name),
		"certificate", kongCert.Name,
		"sni", kongSNI.Name)

	return nil
}

// buildKongCertificate creates a KongCertificate resource from a Gateway listener and certificate reference.
func (c *gatewayConverter) buildKongCertificate(
	listener *gwtypes.Listener,
	certRef gatewayv1.SecretObjectReference,
	secretNamespace string,
) (configurationv1alpha1.KongCertificate, error) {

	// Generate a name for the KongCertificate resource.
	certName := namegen.NewKongCertificateName(c.gateway.Name, strconv.Itoa(int(listener.Port)))
	cert, err := builder.NewKongCertificate().
		WithName(certName).
		WithNamespace(c.gateway.Namespace).
		WithSecretRef(string(certRef.Name), secretNamespace).
		WithControlPlaneRef(*c.controlPlaneRef).
		WithLabels(c.gateway, listener).
		WithAnnotations(c.gateway).
		WithOwner(c.gateway).
		Build()

	if err != nil {
		return configurationv1alpha1.KongCertificate{}, err
	}

	return cert, nil
}

// buildKongSNI creates a KongSNI resource for a Gateway listener.
// If the listener has a hostname, creates an SNI for that hostname.
// If no hostname is specified, creates an SNI with a wildcard (*).
func (c *gatewayConverter) buildKongSNI(
	listener *gwtypes.Listener,
	kongCert *configurationv1alpha1.KongCertificate,
) (configurationv1alpha1.KongSNI, error) {
	// Determine hostname to create SNI for.
	hostname := "*" // Default to wildcard if no hostname specified
	if listener.Hostname != nil && *listener.Hostname != "" {
		hostname = string(*listener.Hostname)
	}

	sni, err := builder.NewKongSNI().
		WithName(kongCert.Name).
		WithNamespace(c.gateway.Namespace).
		WithSNIName(hostname).
		WithCertificateRef(kongCert.Name).
		WithLabels(c.gateway, listener).
		WithAnnotations(c.gateway).
		WithOwner(c.gateway).
		Build()

	if err != nil {
		return configurationv1alpha1.KongSNI{}, err
	}

	return sni, nil
}
