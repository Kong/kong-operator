package ipfamily

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// kubernetesServiceKey is the Service Kubernetes always creates for the
// apiserver.
var kubernetesServiceKey = types.NamespacedName{Namespace: "default", Name: "kubernetes"}

// detectBackoff is the retry policy used when detecting the cluster's IP
// family at startup. It absorbs transient API server errors; a persistent
// failure is reported as an error instead of assuming a concrete family,
// because a wrong assumption (e.g. falling back to IPv4 on an IPv6-only
// cluster) would render DataPlanes' Kong listens unreachable.
var detectBackoff = wait.Backoff{
	Duration: 100 * time.Millisecond,
	Factor:   2,
	Cap:      time.Second,
	Steps:    6,
}

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
// always wins). Otherwise, the cluster's IP family is detected via reader,
// retrying on transient errors; if detection ultimately fails (RBAC denied,
// Service missing, API server unavailable), an error is returned so the
// operator can fail fast: assuming a concrete family (e.g. IPv4) would
// silently break DataPlanes on clusters of a different family.
func Resolve(ctx context.Context, configured IPFamily, reader client.Reader, log logr.Logger) (IPFamily, error) {
	if configured != Auto {
		return configured, nil
	}

	var (
		detected IPFamily
		lastErr  error
	)
	if err := wait.ExponentialBackoffWithContext(ctx, detectBackoff, func(ctx context.Context) (bool, error) {
		d, err := Detect(ctx, reader)
		if err != nil {
			lastErr = err
			log.Error(err, "failed to detect cluster IP family, retrying")
			return false, nil
		}
		detected = d
		return true, nil
	}); err != nil {
		if lastErr == nil {
			// The condition never ran: the context was canceled before the
			// first attempt. Wrap the wait error (ctx.Err()) itself instead of
			// the unset lastErr.
			return "", fmt.Errorf("failed to detect cluster IP family: %w", err)
		}
		return "", fmt.Errorf("failed to detect cluster IP family after %d attempts: %w", detectBackoff.Steps, lastErr)
	}

	log.Info("detected cluster IP family for DataPlane listens", "ipFamily", detected)
	return detected, nil
}
