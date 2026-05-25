package main

import "testing"

func TestSupportedKonnectPackageTypesWithListIncludesGeneratedOASTypes(t *testing.T) {
	t.Parallel()

	got := make(map[string]struct{})
	for _, supported := range supportedKonnectPackageTypesWithList {
		if supported.PackageVersion != "v1alpha1" {
			continue
		}
		for _, typ := range supported.Types {
			got[typ.Type] = struct{}{}
		}
	}

	for _, want := range []string{
		"IdentityProviderRequest",
		"KonnectEventDataPlaneCertificate",
		"KonnectEventGateway",
	} {
		if _, ok := got[want]; !ok {
			t.Fatalf("supportedKonnectPackageTypesWithList is missing %s", want)
		}
	}
}
