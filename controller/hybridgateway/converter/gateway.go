package converter

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	secretref "github.com/kong/kong-operator/controller/pkg/secrets/ref"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

var _ APIConverter[gwtypes.Gateway] = &gatewayConverter{}

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
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//
// Returns:
//   - bool: true if the status was updated, false if no changes were made
//   - error: Any error that occurred during status processing
func (c *gatewayConverter) UpdateRootObjectStatus(ctx context.Context, logger logr.Logger) (bool, error) {
	// TODO: implement status update logic

	return false, nil
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
