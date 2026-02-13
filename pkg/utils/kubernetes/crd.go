package kubernetes

import (
	"errors"
	"fmt"
	"net/url"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CRDChecker verifies whether the resource type defined by GVR is supported by the k8s apiserver.
type CRDChecker struct {
	Client client.Client
}

// CRDExists returns true if the apiserver supports the specified group/version/resource.
func (c CRDChecker) CRDExists(gvr schema.GroupVersionResource) (bool, error) {
	_, err := c.Client.RESTMapper().KindFor(gvr)

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	if errD := (&discovery.ErrGroupDiscoveryFailed{}); errors.As(err, &errD) {
		for _, e := range errD.Groups {

			// If this is an API StatusError:
			if errS := (&apierrors.StatusError{}); errors.As(e, &errS) {
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
			if errU := (&url.Error{}); errors.As(e, &errU) {
				return false, fmt.Errorf("unexpected network error when looking up CRD (%v): %w", gvr, err)
			}
		}

		// Otherwise it's a different error, report a missing CRD.
		return false, err
	}

	return true, nil
}
