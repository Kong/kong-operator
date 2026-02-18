package konnect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/v2/controller/pkg/extensions"
	managercfg "github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/config"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// KonnectExtensionConfig holds the configuration for a KIC instance based on a KonnectExtension resource.
// It includes the KonnectConfig that should be enabled.
type KonnectExtensionConfig struct {
	// KonnectConfig contains the configuration for the Konnect instance.
	KonnectConfig *managercfg.KonnectConfig
}

// ControlPlaneKonnectExtensionProcessor processes Konnect extensions for ControlPlane resources.
type ControlPlaneKonnectExtensionProcessor struct {
	KonnectExtensionConfig *KonnectExtensionConfig
}

// Compile-time check to ensure ControlPlaneKonnectExtensionProcessor implements the extensions.ExtensionProcessor interface.
var _ extensions.Processor = (*ControlPlaneKonnectExtensionProcessor)(nil)

// Process extracts the KonnectExtension from the ControlPlane and generates the KonnectExtensionConfig.
// It returns true if a KonnectExtension was found and processed, false otherwise.
// If an error occurs during processing, it returns false and the error.
// It captures configuration from the KonnectExtension resource and prepares it for use in KIC instances.
// This implementation of the ExtensionProcessor interface has side effects, as it modifies the KonnectExtensionConfig field of the processor.
func (p *ControlPlaneKonnectExtensionProcessor) Process(ctx context.Context, cl client.Client, object client.Object) (bool, error) {
	var konnectExtension *konnectv1alpha2.KonnectExtension

	// First thing we do is check if the object is a ControlPlane.
	cp, ok := object.(*gwtypes.ControlPlane)
	if !ok {
		return false, fmt.Errorf("object is not a ControlPlane: %T", object)
	}

	for _, extensionRef := range cp.Spec.Extensions {
		extension, err := getExtension(ctx, cl, cp.Namespace, extensionRef)
		if err != nil {
			return false, err
		}
		if extension != nil {
			konnectExtension = extension
			break
		}
	}
	if konnectExtension == nil {
		return false, nil
	}

	config, err := kicInKonnectDefaults(ctx, cl, konnectExtension)
	if err != nil {
		return false, fmt.Errorf("failed to generate configuration from KonnectExtension %s for ControlPlane %s: %w",
			client.ObjectKeyFromObject(konnectExtension), client.ObjectKeyFromObject(cp), err)
	}
	p.KonnectExtensionConfig = config

	// Apply the FillIDs feature gate to the ControlPlane.
	applyFeatureGatesToControlPlane(cp)

	return true, nil
}

// GetKonnectConfig returns the KonnectConfig from the KonnectExtensionConfig.
func (p *ControlPlaneKonnectExtensionProcessor) GetKonnectConfig() *managercfg.KonnectConfig {
	if p.KonnectExtensionConfig == nil {
		return nil
	}
	return p.KonnectExtensionConfig.KonnectConfig
}

// kicInKonnectDefaults generates the KonnectExtensionConfig for a KIC instance based on the KonnectExtension resource.
func kicInKonnectDefaults(ctx context.Context, cl client.Client, konnectExtension *konnectv1alpha2.KonnectExtension) (*KonnectExtensionConfig, error) {
	switch konnectExtension.Status.Konnect.ClusterType {
	case konnectv1alpha2.ClusterTypeK8sIngressController:
		// Get the secret for the TLS client cert and key.
		tlsClientSecretName := konnectExtension.Status.DataPlaneClientAuth.CertificateSecretRef.Name
		if tlsClientSecretName == "" {
			return nil, fmt.Errorf("missing TLS client secret name in KonnectExtensionStatus")
		}
		TLSClientCert, TLSClientKey, err := getTLSClientCertAndKey(ctx, cl, tlsClientSecretName, konnectExtension.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get TLS client cert and key from secret %s/%s: %w", konnectExtension.Namespace, tlsClientSecretName, err)
		}

		// Build the configuration and return it.
		return &KonnectExtensionConfig{
			KonnectConfig: &managercfg.KonnectConfig{
				Address:        buildKonnectAddress(konnectExtension.Status.Konnect.Endpoints.ControlPlaneEndpoint),
				ControlPlaneID: konnectExtension.Status.Konnect.ControlPlaneID,
				TLSClient: managercfg.TLSClientConfig{
					Cert: TLSClientCert,
					Key:  TLSClientKey,
				},
				LicenseSynchronizationEnabled: true,
				ConfigSynchronizationEnabled:  true,
				UploadConfigPeriod:            time.Second * 10,
				InitialLicensePollingPeriod:   time.Second * 10,
				LicensePollingPeriod:          time.Second * 10,
			},
		}, nil

	case konnectv1alpha2.ClusterTypeControlPlane:
		return nil, fmt.Errorf("unsupported Konnect cluster type: %s", konnectExtension.Status.Konnect.ClusterType)
	default:
		// default never happens as the validation is at the CRD level
		panic(fmt.Sprintf("unsupported Konnect cluster type: %s", konnectExtension.Status.Konnect.ClusterType))
	}
}

// buildKonnectAddress builds the Konnect address out of the control plane endpoint.
// input: "https://7b46471d3b.us.tp.konghq.tech:443"
// output: "https://us.kic.api.konghq.tech"
func buildKonnectAddress(endpoint string) string {
	portlessEndpoint := strings.TrimSuffix(endpoint, ":443")
	addressSlice := strings.Split(portlessEndpoint, ".")
	addressSlice = lo.Filter(addressSlice, func(_ string, i int) bool {
		// remove 7b46471d3b and tp0
		if i == 0 || i == 2 {
			return false
		}
		return true
	})
	return fmt.Sprintf("https://%s.kic.api.%s", addressSlice[0], strings.Join(addressSlice[1:], "."))
}

// getTLSClientCertAndKey retrieves the TLS client certificate and key from the specified secret.
// Returns the certificate and key as strings, or an error if the secret is not found or if the data is missing.
// The secret must contain "tls.crt" and "tls.key" keys.
// This function is used to extract the TLS client cert and key for Konnect configuration.
func getTLSClientCertAndKey(ctx context.Context, cl client.Client, secretName, secretNamespace string) (tlsClientCert, tlsClientKey string, err error) {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, secret); err != nil {
		return "", "", fmt.Errorf("failed to get TLS client secret %s/%s: %w", secretNamespace, secretName, err)
	}

	tlsCert, ok := secret.Data["tls.crt"]
	if !ok {
		return "", "", fmt.Errorf("TLS certificate not found in secret %s/%s", secretNamespace, secretName)
	}

	tlsKey, ok := secret.Data["tls.key"]
	if !ok {
		return "", "", fmt.Errorf("TLS key not found in secret %s/%s", secretNamespace, secretName)
	}

	return string(tlsCert), string(tlsKey), nil
}

// applyFeatureGatesToControlPlane applies the feature gates to the ControlPlane.
func applyFeatureGatesToControlPlane(cp *gwtypes.ControlPlane) {
	fillIDsFeatureGate := gwtypes.ControlPlaneFeatureGate{
		Name:  managercfg.FillIDsFeature,
		State: gwtypes.FeatureGateStateEnabled,
	}

	found := false
	for i, fg := range cp.Spec.FeatureGates {
		if fg.Name == fillIDsFeatureGate.Name {
			found = true
			if fg.State != gwtypes.FeatureGateStateEnabled {
				cp.Spec.FeatureGates[i].State = gwtypes.FeatureGateStateEnabled
			}
			break
		}
	}
	if !found {
		cp.Spec.FeatureGates = append(cp.Spec.FeatureGates, fillIDsFeatureGate)
	}
}
