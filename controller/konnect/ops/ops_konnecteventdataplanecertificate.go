package ops

// TODO: This file is hand-written and will be replaced with generated code.
// https://github.com/kong/kong-operator/issues/3857

import (
	"context"
	"fmt"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func createKonnectEventDataPlaneCertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) error {
	gatewayID := certGatewayID(cert)
	if gatewayID == "" {
		return CantPerformOperationWithoutEventGatewayIDError{Entity: cert, Op: CreateOp}
	}

	req, err := kongEventDataPlaneCertificateCreateRequest(ctx, cl, cert)
	if err != nil {
		return err
	}

	resp, err := sdk.CreateEventGatewayDataPlaneCertificate(ctx, gatewayID, req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrap != nil {
		return errWrap
	}
	if resp == nil || resp.EventGatewayDataPlaneCertificate == nil || resp.EventGatewayDataPlaneCertificate.ID == "" {
		return fmt.Errorf("failed creating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	cert.SetKonnectID(resp.EventGatewayDataPlaneCertificate.ID)
	return nil
}

func updateKonnectEventDataPlaneCertificate(
	ctx context.Context,
	cl client.Client,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) error {
	gatewayID := certGatewayID(cert)
	if gatewayID == "" {
		return CantPerformOperationWithoutEventGatewayIDError{Entity: cert, Op: UpdateOp}
	}

	req, err := kongEventDataPlaneCertificateUpdateRequest(ctx, cl, cert)
	if err != nil {
		return err
	}

	resp, err := sdk.UpdateEventGatewayDataPlaneCertificate(ctx, sdkkonnectops.UpdateEventGatewayDataPlaneCertificateRequest{
		GatewayID:     gatewayID,
		CertificateID: cert.GetKonnectID(),
		UpdateEventGatewayDataPlaneCertificateRequest: req,
	})
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrap != nil {
		return handleUpdateError(ctx, err, cert, func(ctx context.Context) error {
			return createKonnectEventDataPlaneCertificate(ctx, cl, sdk, cert)
		})
	}
	if resp == nil || resp.EventGatewayDataPlaneCertificate == nil || resp.EventGatewayDataPlaneCertificate.ID == "" {
		return fmt.Errorf("failed updating %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	cert.SetKonnectID(resp.EventGatewayDataPlaneCertificate.ID)
	return nil
}

func deleteKonnectEventDataPlaneCertificate(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) error {
	gatewayID := certGatewayID(cert)
	if gatewayID == "" {
		return CantPerformOperationWithoutEventGatewayIDError{Entity: cert, Op: DeleteOp}
	}

	_, err := sdk.DeleteEventGatewayDataPlaneCertificate(ctx, gatewayID, cert.GetKonnectID())
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrap != nil {
		return handleDeleteError(ctx, err, cert)
	}

	return nil
}

func getKonnectEventDataPlaneCertificateForUID(
	ctx context.Context,
	sdk sdkkonnectgo.EventGatewayDataPlaneCertificatesSDK,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (string, error) {
	gatewayID := certGatewayID(cert)
	if gatewayID == "" {
		return "", CantPerformOperationWithoutEventGatewayIDError{Entity: cert, Op: GetOp}
	}

	resp, err := sdk.ListEventGatewayDataPlaneCertificates(ctx, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
		GatewayID: gatewayID,
	})
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), err)
	}
	if resp == nil || resp.ListEventGatewayDataPlaneCertificatesResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", cert.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.ListEventGatewayDataPlaneCertificatesResponse.Data {
		if !eventGatewayDataPlaneCertificateMatchesSpec(entry, cert) {
			continue
		}
		if entry.ID != "" {
			return entry.ID, nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: cert}
}

func certGatewayID(cert *konnectv1alpha1.KonnectEventDataPlaneCertificate) string {
	if cert.Status.GatewayID == nil {
		return ""
	}
	return cert.Status.GatewayID.ID
}

func kongEventDataPlaneCertificateCreateRequest(
	ctx context.Context,
	cl client.Client,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (*sdkkonnectcomp.CreateEventGatewayDataPlaneCertificateRequest, error) {
	spec, err := kongEventDataPlaneCertificateAPISpec(ctx, cl, cert)
	if err != nil {
		return nil, err
	}
	return spec.ToCreateEventGatewayDataPlaneCertificateRequest()
}

func kongEventDataPlaneCertificateUpdateRequest(
	ctx context.Context,
	cl client.Client,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (*sdkkonnectcomp.UpdateEventGatewayDataPlaneCertificateRequest, error) {
	spec, err := kongEventDataPlaneCertificateAPISpec(ctx, cl, cert)
	if err != nil {
		return nil, err
	}
	return spec.ToUpdateEventGatewayDataPlaneCertificateRequest()
}

func kongEventDataPlaneCertificateAPISpec(
	ctx context.Context,
	cl client.Client,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) (*konnectv1alpha1.KonnectEventDataPlaneCertificateAPISpec, error) {
	apiSpec := cert.Spec.APISpec
	if cert.Spec.Type != nil && *cert.Spec.Type == konnectv1alpha1.SensitiveDataSourceTypeSecretRef {
		certData, err := fetchEventGatewayTLSCertDataFromSecret(ctx, cl, cert.GetNamespace(), cert.Spec.SecretRef)
		if err != nil {
			return nil, err
		}
		apiSpec.Certificate = certData
	}
	return &apiSpec, nil
}

func fetchEventGatewayTLSCertDataFromSecret(
	ctx context.Context,
	cl client.Client,
	parentNamespace string,
	secretRef *commonv1alpha1.NamespacedRef,
) (string, error) {
	if secretRef == nil {
		return "", fmt.Errorf("secretRef is nil")
	}

	ns := parentNamespace
	if secretRef.Namespace != nil && *secretRef.Namespace != "" {
		ns = *secretRef.Namespace
	}

	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: secretRef.Name}, &secret); err != nil {
		return "", fmt.Errorf("failed to fetch Secret %s/%s: %w", ns, secretRef.Name, err)
	}

	certBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return "", fmt.Errorf("secret %s/%s is missing key 'tls.crt'", ns, secretRef.Name)
	}
	return string(certBytes), nil
}

func eventGatewayDataPlaneCertificateMatchesSpec(
	remote sdkkonnectcomp.EventGatewayDataPlaneCertificate,
	cert *konnectv1alpha1.KonnectEventDataPlaneCertificate,
) bool {
	if remote.Certificate != cert.Spec.APISpec.Certificate {
		return false
	}

	remoteName := ""
	if remote.Name != nil {
		remoteName = *remote.Name
	}
	if remoteName != cert.Spec.APISpec.Name {
		return false
	}

	remoteDescription := ""
	if remote.Description != nil {
		remoteDescription = *remote.Description
	}
	return remoteDescription == cert.Spec.APISpec.Description
}
