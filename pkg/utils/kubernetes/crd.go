package kubernetes

import (
	"errors"
	"fmt"
	"net/url"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// CRDExists returns true if the apiserver supports the specified group/version/resource.
func CRDExists(r meta.RESTMapper, gvr schema.GroupVersionResource) (bool, error) {
	_, err := r.KindFor(gvr)
	if meta.IsNoMatchError(err) {
		return false, nil
	}

	if errD, ok := errors.AsType[*discovery.ErrGroupDiscoveryFailed](err); ok {
		// Only the errors of the requested group decide the answer: the
		// discovery of an unrelated group failing (e.g. a broken aggregated
		// apiserver elsewhere on the cluster) says nothing about this group.
		for gv, e := range errD.Groups {
			if gv != gvr.GroupVersion() {
				continue
			}

			// If this is an API StatusError:
			if errS, ok := errors.AsType[*apierrors.StatusError](e); ok {
				switch errS.ErrStatus.Code {
				case 404:
					// If it's a 404 status code then we're sure that it's just
					// a missing CRD. Don't report an error, just false.
					return false, nil
				default:
					return false, fmt.Errorf("unexpected API error status code when looking up CRD (%v): %w", gvr, err)
				}
			}

			// It is a network error.
			if _, ok := errors.AsType[*url.Error](e); ok {
				return false, fmt.Errorf("unexpected network error when looking up CRD (%v): %w", gvr, err)
			}
		}

		// The requested group is not among the failed ones, or failed with an
		// error of an unexpected type: whether the group is served is unknown.
		return false, err
	}

	// Any other error leaves the existence of the CRD unknown: reporting it as
	// installed would let callers set up watches for a type the apiserver may
	// not serve.
	if err != nil {
		return false, fmt.Errorf("unexpected error when looking up CRD (%v): %w", gvr, err)
	}

	return true, nil
}
