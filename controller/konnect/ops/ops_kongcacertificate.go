package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

// createCACertificate creates a KongCACertificate in Konnect.
// It sets the KonnectID and the Programmed condition in the KongCACertificate status.
func createCACertificate(ctx context.Context, sdk CACertificatesSDK, cert *configurationv1alpha1.KongCACertificate) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't create %T %s without a Konnect ControlPlane ID", cert, client.ObjectKeyFromObject(cert))
	}

	resp, err := sdk.CreateCaCertificate(ctx,
		cpID,
		kongCACertificateToCACertificateInput(cert),
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, CreateOp, cert); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToCreate",
				errWrapped.Error(),
				cert.GetGeneration(),
			),
			cert,
		)
		return errWrapped
	}

	cert.Status.Konnect.SetKonnectID(*resp.CACertificate.ID)
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			cert.GetGeneration(),
		),
		cert,
	)

	return nil
}

// updateCACertificate updates a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the KongCACertificate does not have a KonnectID.
func updateCACertificate(ctx context.Context, sdk CACertificatesSDK, cert *configurationv1alpha1.KongCACertificate) error {
	cpID := cert.GetControlPlaneID()
	if cpID == "" {
		return fmt.Errorf("can't update %T without a ControlPlaneID", cert)
	}

	_, err := sdk.UpsertCaCertificate(ctx,
		sdkkonnectops.UpsertCaCertificateRequest{
			ControlPlaneID:  cpID,
			CACertificateID: cert.GetKonnectStatus().GetKonnectID(),
			CACertificate:   kongCACertificateToCACertificateInput(cert),
		},
	)

	// TODO: handle already exists
	// Can't adopt it as it will cause conflicts between the controller
	// that created that entity and already manages it, hm
	if errWrapped := wrapErrIfKonnectOpFailed(err, UpdateOp, cert); errWrapped != nil {
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				conditions.KonnectEntityProgrammedConditionType,
				metav1.ConditionFalse,
				"FailedToUpdate",
				errWrapped.Error(),
				cert.GetGeneration(),
			),
			cert,
		)
		return errWrapped
	}

	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditions.KonnectEntityProgrammedConditionType,
			metav1.ConditionTrue,
			conditions.KonnectEntityProgrammedReasonProgrammed,
			"",
			cert.GetGeneration(),
		),
		cert,
	)

	return nil
}

// deleteCACertificate deletes a KongCACertificate in Konnect.
// The KongCACertificate must have a KonnectID set in its status.
// It returns an error if the operation fails.
func deleteCACertificate(ctx context.Context, sdk CACertificatesSDK, cert *configurationv1alpha1.KongCACertificate) error {
	id := cert.Status.Konnect.GetKonnectID()
	_, err := sdk.DeleteCaCertificate(ctx, cert.GetControlPlaneID(), id)
	if errWrapped := wrapErrIfKonnectOpFailed(err, DeleteOp, cert); errWrapped != nil {
		var sdkError *sdkkonnecterrs.SDKError
		if errors.As(errWrapped, &sdkError) {
			if sdkError.StatusCode == 404 {
				ctrllog.FromContext(ctx).
					Info("entity not found in Konnect, skipping delete",
						"op", DeleteOp, "type", cert.GetTypeName(), "id", id,
					)
				return nil
			}
			return FailedKonnectOpError[configurationv1alpha1.KongCACertificate]{
				Op:  DeleteOp,
				Err: sdkError,
			}
		}
		return FailedKonnectOpError[configurationv1alpha1.KongCACertificate]{
			Op:  DeleteOp,
			Err: errWrapped,
		}
	}

	return nil
}

func kongCACertificateToCACertificateInput(cert *configurationv1alpha1.KongCACertificate) sdkkonnectcomp.CACertificateInput {
	var (
		annotationTags = metadata.ExtractTags(cert)
		specTags       = cert.Spec.Tags
		k8sMetaTags    = GenerateKubernetesMetadataTags(cert)
	)
	return sdkkonnectcomp.CACertificateInput{
		Cert: lo.ToPtr(cert.Spec.Cert),
		// Deduplicate tags to avoid rejection by Konnect.
		Tags: lo.Uniq(slices.Concat(annotationTags, specTags, k8sMetaTags)),
	}
}
