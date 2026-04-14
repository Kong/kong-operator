package ops

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	xkonnectv1alpha1 "github.com/kong/kong-operator/v2/api/x-konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

func createDCRProvider(
	ctx context.Context,
	sdk sdkops.DCRProvidersSDK,
	provider *xkonnectv1alpha1.DcrProvider,
) error {
	req, err := provider.Spec.APISpec.ToCreateDcrProviderRequest()
	if err != nil {
		return fmt.Errorf("failed creating %s SDK request: %w", provider.GetTypeName(), err)
	}

	if err := setCreateDCRProviderRequestLabels(req, WithKubernetesMetadataLabels(provider, createDCRProviderRequestLabels(req))); err != nil {
		return err
	}

	resp, err := sdk.CreateDcrProvider(ctx, *req)
	if errWrap := wrapErrIfKonnectOpFailed(err, CreateOp, provider); errWrap != nil {
		return errWrap
	}

	id := dcrProviderCreateResponseID(resp)
	if id == "" {
		return fmt.Errorf("failed creating %s: %w", provider.GetTypeName(), ErrNilResponse)
	}

	provider.SetKonnectID(id)
	return nil
}

func updateDCRProvider(
	ctx context.Context,
	sdk sdkops.DCRProvidersSDK,
	provider *xkonnectv1alpha1.DcrProvider,
) error {
	req, err := provider.Spec.APISpec.ToUpdateDcrProviderRequest()
	if err != nil {
		return fmt.Errorf("failed updating %s SDK request: %w", provider.GetTypeName(), err)
	}

	req.Labels = toOptionalStringMap(WithKubernetesMetadataLabels(provider, fromOptionalStringMap(req.Labels)))

	resp, err := sdk.UpdateDcrProvider(ctx, provider.GetKonnectID(), *req)
	if errWrap := wrapErrIfKonnectOpFailed(err, UpdateOp, provider); errWrap != nil {
		return handleUpdateError(ctx, err, provider, func(ctx context.Context) error {
			return createDCRProvider(ctx, sdk, provider)
		})
	}

	id := dcrProviderResponseID(resp.GetDcrProviderResponse())
	if id == "" {
		return fmt.Errorf("failed updating %s: %w", provider.GetTypeName(), ErrNilResponse)
	}

	provider.SetKonnectID(id)
	return nil
}

func deleteDCRProvider(
	ctx context.Context,
	sdk sdkops.DCRProvidersSDK,
	provider *xkonnectv1alpha1.DcrProvider,
) error {
	_, err := sdk.DeleteDcrProvider(ctx, provider.GetKonnectID())
	if errWrap := wrapErrIfKonnectOpFailed(err, DeleteOp, provider); errWrap != nil {
		return handleDeleteError(ctx, err, provider)
	}

	return nil
}

func getDCRProviderForUID(
	ctx context.Context,
	sdk sdkops.DCRProvidersSDK,
	provider *xkonnectv1alpha1.DcrProvider,
) (string, error) {
	req := sdkkonnectops.ListDcrProvidersRequest{}
	if name := dcrProviderName(provider); name != "" {
		req.FilterNameContains = &name
	}

	resp, err := sdk.ListDcrProviders(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", provider.GetTypeName(), err)
	}
	if resp == nil || resp.ListDcrProvidersResponse == nil {
		return "", fmt.Errorf("failed listing %s: %w", provider.GetTypeName(), ErrNilResponse)
	}

	for _, entry := range resp.ListDcrProvidersResponse.Data {
		id, labels := dcrProviderResponseIDAndLabels(entry)
		if labels[KubernetesUIDLabelKey] != string(provider.GetUID()) {
			continue
		}
		if id != "" {
			return id, nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: provider}
}

func createDCRProviderRequestLabels(req *sdkkonnectcomp.CreateDcrProviderRequest) map[string]string {
	switch req.Type {
	case sdkkonnectcomp.CreateDcrProviderRequestTypeAuth0:
		if req.CreateDcrProviderRequestAuth0 != nil {
			return req.CreateDcrProviderRequestAuth0.Labels
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeAzureAd:
		if req.CreateDcrProviderRequestAzureAd != nil {
			return req.CreateDcrProviderRequestAzureAd.Labels
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeCurity:
		if req.CreateDcrProviderRequestCurity != nil {
			return req.CreateDcrProviderRequestCurity.Labels
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeOkta:
		if req.CreateDcrProviderRequestOkta != nil {
			return req.CreateDcrProviderRequestOkta.Labels
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeHTTP:
		if req.CreateDcrProviderRequestHTTP != nil {
			return req.CreateDcrProviderRequestHTTP.Labels
		}
	}

	return nil
}

func setCreateDCRProviderRequestLabels(req *sdkkonnectcomp.CreateDcrProviderRequest, labels map[string]string) error {
	switch req.Type {
	case sdkkonnectcomp.CreateDcrProviderRequestTypeAuth0:
		if req.CreateDcrProviderRequestAuth0 != nil {
			req.CreateDcrProviderRequestAuth0.Labels = labels
			return nil
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeAzureAd:
		if req.CreateDcrProviderRequestAzureAd != nil {
			req.CreateDcrProviderRequestAzureAd.Labels = labels
			return nil
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeCurity:
		if req.CreateDcrProviderRequestCurity != nil {
			req.CreateDcrProviderRequestCurity.Labels = labels
			return nil
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeOkta:
		if req.CreateDcrProviderRequestOkta != nil {
			req.CreateDcrProviderRequestOkta.Labels = labels
			return nil
		}
	case sdkkonnectcomp.CreateDcrProviderRequestTypeHTTP:
		if req.CreateDcrProviderRequestHTTP != nil {
			req.CreateDcrProviderRequestHTTP.Labels = labels
			return nil
		}
	}

	return fmt.Errorf("failed setting labels on %T: unsupported request type %q", req, req.Type)
}

func dcrProviderCreateResponseID(resp *sdkkonnectops.CreateDcrProviderResponse) string {
	if resp == nil || resp.CreateDcrProviderResponse == nil {
		return ""
	}

	switch {
	case resp.GetCreateDcrProviderResponseDcrProviderAuth0() != nil:
		return resp.GetCreateDcrProviderResponseDcrProviderAuth0().GetID()
	case resp.GetCreateDcrProviderResponseDcrProviderAzureAd() != nil:
		return resp.GetCreateDcrProviderResponseDcrProviderAzureAd().GetID()
	case resp.GetCreateDcrProviderResponseDcrProviderCurity() != nil:
		return resp.GetCreateDcrProviderResponseDcrProviderCurity().GetID()
	case resp.GetCreateDcrProviderResponseDcrProviderOkta() != nil:
		return resp.GetCreateDcrProviderResponseDcrProviderOkta().GetID()
	case resp.GetCreateDcrProviderResponseDcrProviderHTTP() != nil:
		return resp.GetCreateDcrProviderResponseDcrProviderHTTP().GetID()
	default:
		return ""
	}
}

func dcrProviderResponseID(resp *sdkkonnectcomp.DcrProviderResponse) string {
	id, _ := dcrProviderResponseIDAndLabelsFromPointer(resp)
	return id
}

func dcrProviderResponseIDAndLabels(resp sdkkonnectcomp.DcrProviderResponse) (string, map[string]string) {
	return dcrProviderResponseIDAndLabelsFromPointer(&resp)
}

func dcrProviderResponseIDAndLabelsFromPointer(resp *sdkkonnectcomp.DcrProviderResponse) (string, map[string]string) {
	if resp == nil {
		return "", nil
	}

	switch {
	case resp.DCRProviderAuth0DCRProviderAuth0 != nil:
		return resp.DCRProviderAuth0DCRProviderAuth0.GetID(), resp.DCRProviderAuth0DCRProviderAuth0.GetLabels()
	case resp.DCRProviderAzureADDCRProviderAzureAD != nil:
		return resp.DCRProviderAzureADDCRProviderAzureAD.GetID(), resp.DCRProviderAzureADDCRProviderAzureAD.GetLabels()
	case resp.DCRProviderCurityDCRProviderCurity != nil:
		return resp.DCRProviderCurityDCRProviderCurity.GetID(), resp.DCRProviderCurityDCRProviderCurity.GetLabels()
	case resp.DCRProviderOKTADCRProviderOKTA != nil:
		return resp.DCRProviderOKTADCRProviderOKTA.GetID(), resp.DCRProviderOKTADCRProviderOKTA.GetLabels()
	case resp.DCRProviderHTTPDCRProviderHTTP != nil:
		return resp.DCRProviderHTTPDCRProviderHTTP.GetID(), resp.DCRProviderHTTPDCRProviderHTTP.GetLabels()
	default:
		return "", nil
	}
}

func dcrProviderName(provider *xkonnectv1alpha1.DcrProvider) string {
	cfg := provider.Spec.APISpec.DcrProviderConfig
	if cfg == nil {
		return ""
	}

	switch {
	case cfg.Auth0 != nil:
		return string(cfg.Auth0.Name)
	case cfg.AzureAd != nil:
		return string(cfg.AzureAd.Name)
	case cfg.Curity != nil:
		return string(cfg.Curity.Name)
	case cfg.Okta != nil:
		return string(cfg.Okta.Name)
	case cfg.HTTP != nil:
		return string(cfg.HTTP.Name)
	default:
		return ""
	}
}

func toOptionalStringMap(in map[string]string) map[string]*string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]*string, len(in))
	for k, v := range in {
		value := v
		out[k] = &value
	}
	return out
}

func fromOptionalStringMap(in map[string]*string) map[string]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		if v != nil {
			out[k] = *v
		}
	}
	return out
}
