package gatewayapi

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// v1beta1GroupVersion is gatewayv1beta1.GroupVersion converted to
// schema.GroupVersion for comparison purposes.
var v1beta1GroupVersion = schema.GroupVersion(gatewayv1beta1.GroupVersion)

// NewReferenceGrant returns an empty ReferenceGrant object of the given API
// version. Any GroupVersion other than v1beta1 (including the zero value)
// resolves to v1.
func NewReferenceGrant(gv schema.GroupVersion) client.Object {
	if gv == v1beta1GroupVersion {
		return &gatewayv1beta1.ReferenceGrant{}
	}
	return &ReferenceGrant{}
}

// NewReferenceGrantList returns an empty ReferenceGrantList object of the
// given API version. Any GroupVersion other than v1beta1 (including the zero
// value) resolves to v1.
func NewReferenceGrantList(gv schema.GroupVersion) client.ObjectList {
	if gv == v1beta1GroupVersion {
		return &gatewayv1beta1.ReferenceGrantList{}
	}
	return &ReferenceGrantList{}
}

// AsReferenceGrant normalizes a ReferenceGrant received from a watch or
// predicate (which may be v1 or v1beta1) into the common v1 type. This
// conversion is safe because v1beta1.ReferenceGrant is defined as
// `type ReferenceGrant v1.ReferenceGrant` - an identical underlying type
// under a distinct name.
func AsReferenceGrant(obj client.Object) (*ReferenceGrant, bool) {
	switch rg := obj.(type) {
	case *ReferenceGrant:
		return rg, true
	case *gatewayv1beta1.ReferenceGrant:
		return (*ReferenceGrant)(rg), true
	default:
		return nil, false
	}
}

// ReferenceGrantItems normalizes every item in a v1 or v1beta1
// ReferenceGrantList into the common v1 type, the same way AsReferenceGrant
// does for a single object.
func ReferenceGrantItems(list client.ObjectList) []*ReferenceGrant {
	switch l := list.(type) {
	case *ReferenceGrantList:
		grants := make([]*ReferenceGrant, len(l.Items))
		for i := range l.Items {
			grants[i] = &l.Items[i]
		}
		return grants
	case *gatewayv1beta1.ReferenceGrantList:
		grants := make([]*ReferenceGrant, len(l.Items))
		for i := range l.Items {
			grants[i] = (*ReferenceGrant)(&l.Items[i])
		}
		return grants
	default:
		return nil
	}
}
