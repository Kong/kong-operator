package gatewayapi

import (
	"slices"
	"testing"

	"sigs.k8s.io/gateway-api/pkg/features"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGetSupportedFeaturesReturnsMutableCopy(t *testing.T) {
	first, err := GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(first, second) {
		t.Fatalf("expected returned feature lists to be equal, got %v and %v", first, second)
	}

	first[0] = features.FeatureName("mutated")

	third, err := GetSupportedFeatures(consts.RouterFlavorTraditionalCompatible)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(first, third) {
		t.Fatalf("mutating returned feature list affected subsequent calls: %v", third)
	}
	if !slices.Equal(second, third) {
		t.Fatalf("expected subsequent feature list to match original, got %v and %v", second, third)
	}
}
