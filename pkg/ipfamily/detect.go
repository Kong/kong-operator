package ipfamily

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// kubernetesServiceKey is the Service Kubernetes always creates for the
// apiserver.
var kubernetesServiceKey = types.NamespacedName{Namespace: "default", Name: "kubernetes"}

// Detect determines the cluster's IP family by reading the ipFamilies of the
// default/kubernetes Service. reader should be an uncached client (e.g.
// manager.GetAPIReader()) so it works regardless of which namespaces the
// manager's cache is scoped to.
func Detect(ctx context.Context, reader client.Reader) (IPFamily, error) {
	var svc corev1.Service
	if err := reader.Get(ctx, kubernetesServiceKey, &svc); err != nil {
		return "", fmt.Errorf("failed to get %s Service to detect cluster IP family: %w", kubernetesServiceKey, err)
	}

	var hasIPv4, hasIPv6 bool
	for _, family := range svc.Spec.IPFamilies {
		switch family {
		case corev1.IPv4Protocol:
			hasIPv4 = true
		case corev1.IPv6Protocol:
			hasIPv6 = true
		case corev1.IPFamilyUnknown:
		}
	}

	switch {
	case hasIPv4 && hasIPv6:
		return Dual, nil
	case hasIPv6:
		return IPv6, nil
	case hasIPv4:
		return IPv4, nil
	default:
		return "", fmt.Errorf("%s Service reported no recognized IP families: %v", kubernetesServiceKey, svc.Spec.IPFamilies)
	}
}

// Resolve returns the IPFamily the operator should use. If configured is
// anything other than Auto, it is returned as-is (explicit configuration
// always wins). Otherwise, the cluster's IP family is detected via reader; if
// detection fails for any reason (RBAC denied, Service missing, timeout), the
// error is logged and IPv4 is returned.
func Resolve(ctx context.Context, configured IPFamily, reader client.Reader, log logr.Logger) IPFamily {
	if configured != Auto {
		return configured
	}

	detected, err := Detect(ctx, reader)
	if err != nil {
		log.Error(err, "failed to detect cluster IP family, falling back to IPv4-only DataPlane listens")
		return IPv4
	}

	log.Info("detected cluster IP family for DataPlane listens", "ipFamily", detected)
	return detected
}
