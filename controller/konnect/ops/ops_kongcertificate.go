package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// createCertificate creates a KongCertificate in Konnect.
// It sets the KonnectID in the KongCertificate status.
func createCertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	// NOTE: This is a workaround for the fact that the Konnect SDK does not
	// return a conflict error when creating a Certificate as there are no criteria
	// that would prevent the creation of a Certificate with the same spec fields.
	// This can be expanded to other entities by changing the order of get -> list
	// to list -> get in ops.go's Create() function.

	// Check if the Certificate already exists in Konnect: it is possible that
	// the Certificate was already created and cache the controller is using
	// is outdated, so we can't rely on the status.konnect.id.
	// If it does, set the KonnectID in the KongCertificate status and return.

	respList, err := sdk.ListCertificate(ctx, sdkkonnectops.ListCertificateRequest{
		ControlPlaneID: cpID,
		Tags:           lo.ToPtr(strings.Join(GenerateTagsForObject(cert, cert.Spec.Tags...), ",")),
	})
	if err != nil {
		return fmt.Errorf("failed to list: %w", err)
	}
	if respList.Object != nil && len(respList.Object.Data) > 0 {
		certList := respList.Object.Data[0]
		if certList.ID == nil {
			return fmt.Errorf("failed listing: found a cert without ID")
		}
		cert.SetKonnectID(*certList.ID)
		return nil
	}

	// Generate the input for the creation.
	// This may fetch the Secret if the Certificate source type is SecretRef.
	input, err := kongCertificateToCertificateInput(ctx, cl, cert)
	if err != nil {
		return err
	}

	resp, err := sdk.CreateCertificate(ctx,
		cpID,
		input,
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.Certificate == nil || resp.Certificate.ID == nil || *resp.Certificate.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	// At this point, the Certificate has been created successfully.
	cert.SetKonnectID(*resp.Certificate.ID)

	return nil
}

// updateCertificate updates a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the KongCertificate does not have a KonnectID.
func updateCertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: UpdateOp}
	}

	// Generate the input for the update.
	// This may fetch the Secret if the Certificate source type is SecretRef.
	input, err := kongCertificateToCertificateInput(ctx, cl, cert)
	if err != nil {
		return err
	}

	_, err = sdk.UpsertCertificate(ctx,
		sdkkonnectops.UpsertCertificateRequest{
			ControlPlaneID: cpID,
			CertificateID:  cert.GetKonnectStatus().GetKonnectID(),
			Certificate:    input,
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteCertificate deletes a KongCertificate in Konnect.
// The KongCertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCertificate(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func adoptCertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}
	adoptOptions := cert.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetCertificate(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.Certificate == nil {
		return fmt.Errorf("failed to adopt %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.Certificate.Tags)
	if hasUIDTag && extractUIDFromTag(uidTag) != string(cert.UID) {
		return KonnectEntityAdoptionUIDTagConflictError{
			KonnectID:    konnectID,
			ActualUIDTag: extractUIDFromTag(uidTag),
		}
	}

	adoptMode := adoptOptions.Mode
	if adoptMode == "" {
		adoptMode = commonv1alpha1.AdoptModeOverride
	}

	switch adoptMode {
	case commonv1alpha1.AdoptModeOverride:
		certCopy := cert.DeepCopy()
		certCopy.SetKonnectID(konnectID)
		if err = updateCertificate(ctx, cl, sdk, certCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !certificateMatch(resp.Certificate, cert) {
			return KonnectEntityAdoptionNotMatchError{
				KonnectID: konnectID,
			}
		}
	default:
		return fmt.Errorf("failed to adopt: adopt mode %q not supported", adoptMode)
	}

	cert.SetKonnectID(konnectID)
	return nil
}

func fetchTLSDataFromSecret(ctx context.Context, cl client.Client, parentNamespace string, secretRef *commonv1alpha1.NamespacedRef) (certData, keyData string, err error) {
	if secretRef == nil {
		return "", "", fmt.Errorf("secretRef is nil")
	}
	ns := parentNamespace
	if secretRef.Namespace != nil && *secretRef.Namespace != "" {
		ns = *secretRef.Namespace
	}
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretRef.Name,
	}, secret); err != nil {
		return "", "", fmt.Errorf("failed to fetch Secret %s/%s: %w", ns, secretRef.Name, err)
	}

	certBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return "", "", fmt.Errorf("secret %s/%s is missing key 'tls.crt'", ns, secretRef.Name)
	}
	keyBytes, ok := secret.Data["tls.key"]
	if !ok {
		return "", "", fmt.Errorf("secret %s/%s is missing key 'tls.key'", ns, secretRef.Name)
	}

	return string(certBytes), string(keyBytes), nil
}

func kongCertificateToCertificateInput(ctx context.Context, cl client.Client, cert *configurationv1alpha1.KongCertificate) (sdkkonnectcomp.Certificate, error) {
	var certData, keyData, certAltData, keyAltData string

	// Check the certificate data type.
	if cert.Spec.Type != nil && *cert.Spec.Type == configurationv1alpha1.KongCertificateSourceTypeSecretRef {
		// Fetch the Secret.
		secretRef := cert.Spec.SecretRef
		if secretRef == nil {
			// This should not happen due to validation, but just in case.
			return sdkkonnectcomp.Certificate{}, fmt.Errorf("secretRef is nil")
		}

		var err error
		parentNamespace := cert.GetNamespace()
		certData, keyData, err = fetchTLSDataFromSecret(ctx, cl, parentNamespace, secretRef)
		if err != nil {
			return sdkkonnectcomp.Certificate{}, err
		}

		// Optional alternative cert/key.
		if cert.Spec.SecretRefAlt != nil {
			certAltData, keyAltData, err = fetchTLSDataFromSecret(ctx, cl, parentNamespace, cert.Spec.SecretRefAlt)
			if err != nil {
				return sdkkonnectcomp.Certificate{}, err
			}
		}
	} else {
		// Use inline certificate data.
		certData = cert.Spec.Cert
		keyData = cert.Spec.Key
		certAltData = cert.Spec.CertAlt
		keyAltData = cert.Spec.KeyAlt
	}

	input := sdkkonnectcomp.Certificate{
		Cert: certData,
		Key:  keyData,
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}
	if certAltData != "" {
		input.CertAlt = lo.ToPtr(certAltData)
	}
	if keyAltData != "" {
		input.KeyAlt = lo.ToPtr(keyAltData)
	}

	return input, nil
}

func getKongCertificateForUID(
	ctx context.Context,
	sdk sdkops.CertificatesSDK,
	cert *configurationv1alpha1.KongCertificate,
) (string, error) {
	resp, err := sdk.ListCertificate(ctx, sdkkonnectops.ListCertificateRequest{
		ControlPlaneID: cert.GetControlPlaneID(),
		Tags:           lo.ToPtr(UIDLabelForObject(cert)),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list %s: %w", cert.GetTypeName(), err)
	}

	if resp == nil || resp.Object == nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	return getMatchingEntryFromListResponseData(sliceToEntityWithIDPtrSlice(resp.Object.Data), cert)
}

func certificateMatch(
	konnectCert *sdkkonnectcomp.Certificate,
	cert *configurationv1alpha1.KongCertificate,
) bool {
	if konnectCert == nil {
		return false
	}
	spec := cert.Spec
	return konnectCert.Cert == spec.Cert &&
		konnectCert.Key == spec.Key &&
		equalWithDefault(konnectCert.CertAlt, stringPtrOrNil(spec.CertAlt), "") &&
		equalWithDefault(konnectCert.KeyAlt, stringPtrOrNil(spec.KeyAlt), "")
}

func stringPtrOrNil(val string) *string {
	if val == "" {
		return nil
	}
	return lo.ToPtr(val)
}
