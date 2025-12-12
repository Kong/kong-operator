package ops

import (
	"context"
	"errors"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
)

// createCACertificate creates a KongCACertificate in Konnect.
// It sets the KonnectID the KongCACertificate status.
func createCACertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: CreateOp}
	}

	// Generate the input for the creation.
	// This may fetch the Secret if the CACertificate source type is SecretRef.
	input, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
	if err != nil {
		return err
	}

	resp, err := sdk.CreateCaCertificate(ctx,
		cpID,
		input,
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}

	if resp == nil || resp.CACertificate == nil || resp.CACertificate.ID == nil || *resp.CACertificate.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	// At this point, the CACertificate has been created successfully.
	cert.SetKonnectID(*resp.CACertificate.ID)

	return nil
}

// updateCACertificate updates a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the KongCACertificate does not have a KonnectID.
func updateCACertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return CantPerformOperationWithoutControlPlaneIDError{Entity: cert, Op: UpdateOp}
	}

	// Generate the input for the update.
	// This may fetch the Secret if the CACertificate source type is SecretRef.
	input, err := kongCACertificateToCACertificateInput(ctx, cl, cert)
	if err != nil {
		return err
	}

	_, err = sdk.UpsertCaCertificate(ctx,
		sdkkonnectops.UpsertCaCertificateRequest{
			ControlPlaneID:  cpID,
			CACertificateID: cert.GetKonnectStatus().GetKonnectID(),
			CACertificate:   input,
		},
	)

	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		return errWrap
	}

	return nil
}

// deleteCACertificate deletes a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCACertificate(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCaCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func adoptCACertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return errors.New("No Control Plane ID")
	}

	adoptOptions := cert.Spec.Adopt
	konnectID := adoptOptions.Konnect.ID

	resp, err := sdk.GetCaCertificate(ctx, konnectID, cpID)
	if err != nil {
		return KonnectEntityAdoptionFetchError{
			KonnectID: konnectID,
			Err:       err,
		}
	}
	if resp == nil || resp.CACertificate == nil {
		return fmt.Errorf("failed to adopt %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	uidTag, hasUIDTag := findUIDTag(resp.CACertificate.Tags)
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
		if err = updateCACertificate(ctx, cl, sdk, certCopy); err != nil {
			return err
		}
	case commonv1alpha1.AdoptModeMatch:
		if !caCertificateMatch(resp.CACertificate, cert) {
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

func fetchCACertDataFromSecret(ctx context.Context, cl client.Client, parentNamespace string, secretRef *commonv1alpha1.NamespacedRef) (certData string, err error) {
	if secretRef == nil {
		return "", fmt.Errorf("secretRef is nil")
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
		return "", fmt.Errorf("failed to fetch Secret %s/%s: %w", ns, secretRef.Name, err)
	}

	certBytes, ok := secret.Data["ca.crt"]
	if !ok {
		return "", fmt.Errorf("secret %s/%s is missing key 'ca.crt'", ns, secretRef.Name)
	}

	return string(certBytes), nil
}

func kongCACertificateToCACertificateInput(ctx context.Context, cl client.Client, cert *configurationv1alpha1.KongCACertificate) (sdkkonnectcomp.CACertificate, error) {
	var certData string

	// Check the certificate data type.
	if cert.Spec.Type != nil && *cert.Spec.Type == configurationv1alpha1.KongCACertificateSourceTypeSecretRef {
		// Fetch the Secret.
		secretRef := cert.Spec.SecretRef
		if secretRef == nil {
			// This should not happen due to validation, but just in case.
			return sdkkonnectcomp.CACertificate{}, fmt.Errorf("secretRef is nil")
		}

		var err error
		parentNamespace := cert.GetNamespace()
		certData, err = fetchCACertDataFromSecret(ctx, cl, parentNamespace, secretRef)
		if err != nil {
			return sdkkonnectcomp.CACertificate{}, err
		}
	} else {
		// Use inline certificate data.
		certData = cert.Spec.Cert
	}

	return sdkkonnectcomp.CACertificate{
		Cert: certData,
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: GenerateTagsForObject(cert, cert.Spec.Tags...),
	}, nil
}

func getKongCACertificateForUID(
	ctx context.Context,
	sdk sdkops.CACertificatesSDK,
	cert *configurationv1alpha1.KongCACertificate,
) (string, error) {
	resp, err := sdk.ListCaCertificate(ctx, sdkkonnectops.ListCaCertificateRequest{
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

func caCertificateMatch(
	konnectCert *sdkkonnectcomp.CACertificate,
	cert *configurationv1alpha1.KongCACertificate,
) bool {
	if konnectCert == nil {
		return false
	}

	return konnectCert.Cert == cert.Spec.Cert
}
