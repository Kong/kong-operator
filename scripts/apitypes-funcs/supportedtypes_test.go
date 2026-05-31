package main

import "testing"

func collectTypesForVersion(supportedTypes []supportedTypesT, packageVersion string) map[string]struct{} {
	types := make(map[string]struct{})
	for _, supported := range supportedTypes {
		if supported.PackageVersion != packageVersion {
			continue
		}
		for _, typ := range supported.Types {
			types[typ.Type] = struct{}{}
		}
	}

	return types
}

func TestSupportedKonnectPackageTypesWithListExcludesGeneratedOASTypes(t *testing.T) {
	t.Parallel()

	got := collectTypesForVersion(supportedKonnectPackageTypesWithList, "v1alpha1")

	for _, want := range []string{
		"Portal",
		"PortalCustomDomain",
		"PortalCustomization",
		"PortalEmailConfig",
		"PortalIdentityProviderRequest",
		"PortalIPAllowList",
		"PortalPage",
		"PortalTeam",
		"KonnectEventGateway",
	} {
		if _, ok := got[want]; ok {
			t.Fatalf("supportedKonnectPackageTypesWithList should not include %s", want)
		}
	}

	for _, want := range []string{
		"KonnectGatewayControlPlane",
		"KonnectAPIAuthConfiguration",
		"KonnectCloudGatewayNetwork",
		"KonnectCloudGatewayDataPlaneGroupConfiguration",
		"MCPServer",
		"KonnectCloudGatewayTransitGateway",
	} {
		if _, ok := got[want]; !ok {
			t.Fatalf("supportedKonnectPackageTypesWithList should include %s", want)
		}
	}
}

func TestSupportedConfigurationPackageTypesWithListExcludesGeneratedOASTypes(t *testing.T) {
	t.Parallel()

	got := collectTypesForVersion(supportedConfigurationPackageTypesWithList, "v1alpha1")

	for _, want := range []string{
		"EventGatewayListener",
		"EventGatewayListenerPolicy",
		"EventGatewayBackendCluster",
		"EventGatewayVirtualCluster",
		"EventGatewayVirtualClusterPolicy",
		"EventGatewayVirtualClusterConsumePolicy",
		"EventGatewayVirtualClusterProducePolicy",
		"EventGatewayDataPlaneCertificate",
	} {
		if _, ok := got[want]; ok {
			t.Fatalf("supportedConfigurationPackageTypesWithList should not include %s", want)
		}
	}

	for _, want := range []string{
		"KongKey",
		"KongPluginBinding",
		"KongService",
		"KongRoute",
		"KongDataPlaneClientCertificate",
	} {
		if _, ok := got[want]; !ok {
			t.Fatalf("supportedConfigurationPackageTypesWithList should include %s", want)
		}
	}
}
