// Package referencegrant provides test helpers for exercising code against both
// ReferenceGrant API versions.
//
// ReferenceGrant was promoted from v1beta1 to v1 in Gateway API v1.5.0, and the
// operator supports clusters serving either. Tests that read ReferenceGrants should
// therefore run against both versions, seeding their fixtures as the version under
// test.
//
// This package deliberately does not import pkg/utils/kubernetes: that package's own
// tests use these helpers, and importing it here would be an import cycle.
package referencegrant

import (
	"fmt"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// v1 and v1beta1 are the two ReferenceGrant API versions a cluster may serve.
var (
	v1      = schema.GroupVersion(gatewayv1.GroupVersion)
	v1beta1 = schema.GroupVersion(gatewayv1beta1.GroupVersion)
)

// Versions is the set of ReferenceGrant API versions to run a test against. Range
// over it and pass each version to AsVersion and to the code under test.
func Versions() []schema.GroupVersion {
	return []schema.GroupVersion{v1, v1beta1}
}

// V1 is the ReferenceGrant API version for tests that are not sensitive to which
// version the cluster serves, so it just returns v1.
func V1() schema.GroupVersion {
	return v1
}

// IsList reports whether list is a ReferenceGrantList of either served version, for
// interceptors that fail the ReferenceGrant List specifically.
func IsList(list client.ObjectList) bool {
	switch list.(type) {
	case *gatewayv1.ReferenceGrantList, *gatewayv1beta1.ReferenceGrantList:
		return true
	default:
		return false
	}
}

// AsVersion re-types every ReferenceGrant in objs into gv's Go type, leaving every
// other object untouched so that mixed fixture slices can be passed through as-is.
//
// This matters because the fake client stores objects under the GVK of their Go type:
// a fixture left as v1 is invisible to a v1beta1 List, so a test that forgets to
// convert silently exercises an empty grant list and passes for the wrong reason.
//
// The conversion is a plain cast in both directions because v1beta1.ReferenceGrant is
// declared as `type ReferenceGrant v1.ReferenceGrant` - an identical underlying type
// under a distinct name. It panics on any other GroupVersion, since that can only be a
// mistake in the test.
func AsVersion(gv schema.GroupVersion, objs []client.Object) []client.Object {
	if gv != v1 && gv != v1beta1 {
		panic(fmt.Sprintf("unsupported ReferenceGrant GroupVersion %q, want %q or %q", gv, v1, v1beta1))
	}

	return lo.Map(objs, func(o client.Object, _ int) client.Object {
		// Normalize to the v1 Go type first, whichever version the fixture was
		// declared as, then emit it as the version under test.
		var rg *gatewayv1.ReferenceGrant
		switch g := o.(type) {
		case *gatewayv1.ReferenceGrant:
			rg = g
		case *gatewayv1beta1.ReferenceGrant:
			rg = (*gatewayv1.ReferenceGrant)(g)
		default:
			return o
		}

		if gv == v1beta1 {
			return (*gatewayv1beta1.ReferenceGrant)(rg)
		}
		return rg
	})
}
