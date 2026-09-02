package helpers

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// getUpdater is satisfied by every typed client-go / gateway-api clientset accessor,
// namespaced (e.g. TLSRoutes(ns), Gateways(ns)) and cluster-scoped (e.g. GatewayClasses()) alike.
type getUpdater[T any] interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (T, error)
	Update(ctx context.Context, obj T, opts metav1.UpdateOptions) (T, error)
}

// UpdateWithRetry re-fetches the object called name, applies mutate to the fresh copy,
// and updates it, retrying on conflict.
//
// On failure it returns the zero value of T alongside the error. This matters: client-go's
// generated clientsets return a zero-valued object alongside an error on failure, so
// assigning their result straight into a variable that a retry loop later reads back (e.g.
// for its .Name) silently poisons every subsequent iteration. UpdateWithRetry only assigns
// its return value on success, so callers can never be handed a half-written object.
func UpdateWithRetry[T any](ctx context.Context, cl getUpdater[T], name string, mutate func(T)) (T, error) {
	var updated T
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := cl.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		mutate(current)
		result, err := cl.Update(ctx, current, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		updated = result
		return nil
	})
	return updated, err
}
