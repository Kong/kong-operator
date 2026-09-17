package kubernetes

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// v1GroupVersion and v1beta1GroupVersion are the gateway-api GroupVersions
// converted to schema.GroupVersion for comparison purposes.
var (
	v1GroupVersion      = schema.GroupVersion(gatewayv1.GroupVersion)
	v1beta1GroupVersion = schema.GroupVersion(gatewayv1beta1.GroupVersion)
)

// ErrReferenceGrantCRDNotFound is returned by DetectReferenceGrantVersion when the
// cluster serves neither the v1 nor the v1beta1 ReferenceGrant CRD.
var ErrReferenceGrantCRDNotFound = errors.New("neither v1 nor v1beta1 ReferenceGrant CRD found")

// DetectReferenceGrantVersion returns the GroupVersion of whichever ReferenceGrant
// API version is served by the cluster, preferring v1 and falling back to v1beta1
// (ReferenceGrant was promoted from v1beta1 to v1 in gateway-api v1.5.0, older
// clusters only serve v1beta1). It returns ErrReferenceGrantCRDNotFound if neither
// is installed, and a wrapped lookup error if the lookup itself failed.
func DetectReferenceGrantVersion(restMapper meta.RESTMapper) (schema.GroupVersion, error) {
	for _, gv := range []schema.GroupVersion{
		v1GroupVersion,
		v1beta1GroupVersion,
	} {
		exists, err := CRDExists(restMapper, gv.WithResource("referencegrants"))
		if err != nil {
			return schema.GroupVersion{}, fmt.Errorf("failed to detect the ReferenceGrant API version: %w", err)
		}
		if exists {
			return gv, nil
		}
	}
	return schema.GroupVersion{}, ErrReferenceGrantCRDNotFound
}

// NewReferenceGrant returns an empty ReferenceGrant object of the given API
// version. Any GroupVersion other than v1beta1 (including the zero value)
// resolves to v1.
func NewReferenceGrant(gv schema.GroupVersion) client.Object {
	if gv == v1beta1GroupVersion {
		return &gatewayv1beta1.ReferenceGrant{}
	}
	return &gwtypes.ReferenceGrant{}
}

// NewReferenceGrantList returns an empty ReferenceGrantList object of the
// given API version. Any GroupVersion other than v1beta1 (including the zero
// value) resolves to v1.
func NewReferenceGrantList(gv schema.GroupVersion) client.ObjectList {
	if gv == v1beta1GroupVersion {
		return &gatewayv1beta1.ReferenceGrantList{}
	}
	return &gwtypes.ReferenceGrantList{}
}

// AsReferenceGrant normalizes a ReferenceGrant received from a watch or
// predicate (which may be v1 or v1beta1) into the common v1 type. This
// conversion is safe because v1beta1.ReferenceGrant is defined as
// `type ReferenceGrant v1.ReferenceGrant` - an identical underlying type
// under a distinct name.
func AsReferenceGrant(obj client.Object) (*gwtypes.ReferenceGrant, bool) {
	switch rg := obj.(type) {
	case *gwtypes.ReferenceGrant:
		return rg, true
	case *gatewayv1beta1.ReferenceGrant:
		return (*gwtypes.ReferenceGrant)(rg), true
	default:
		return nil, false
	}
}

// ReferenceGrantItems normalizes every item in a v1 or v1beta1
// ReferenceGrantList into the common v1 type, the same way AsReferenceGrant
// does for a single object. Any other list type yields no items, which callers
// read as "no grant" and therefore deny the reference - the same fail-closed
// outcome as a namespace that genuinely has no ReferenceGrants.
func ReferenceGrantItems(list client.ObjectList) []*gwtypes.ReferenceGrant {
	switch l := list.(type) {
	case *gwtypes.ReferenceGrantList:
		grants := make([]*gwtypes.ReferenceGrant, len(l.Items))
		for i := range l.Items {
			grants[i] = &l.Items[i]
		}
		return grants
	case *gatewayv1beta1.ReferenceGrantList:
		grants := make([]*gwtypes.ReferenceGrant, len(l.Items))
		for i := range l.Items {
			grants[i] = (*gwtypes.ReferenceGrant)(&l.Items[i])
		}
		return grants
	default:
		return nil
	}
}
