package konnect

import (
	"context"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// setSecretReferenceWatches sets up watches for Konnect resources that reference secrets.
func setSecretReferenceWatches(b *builder.Builder) {
	b.Watches(
		&konnectv1alpha1.KonnectAPIAuthConfiguration{},
		handler.Funcs{
			CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				enqueueSecretsFromAPIAuthConfiguration(e.Object, q)
			},
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				// Enqueue secrets from both old and new objects to handle reference changes.
				enqueueSecretsFromAPIAuthConfiguration(e.ObjectOld, q)
				enqueueSecretsFromAPIAuthConfiguration(e.ObjectNew, q)
			},
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
				enqueueSecretsFromAPIAuthConfiguration(e.Object, q)
			},
		},
	)
}

// enqueueSecretsFromAPIAuthConfiguration enqueues Secret reconcile requests.
// for secrets referenced by the given KonnectAPIAuthConfiguration.
func enqueueSecretsFromAPIAuthConfiguration(obj client.Object, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	apiAuth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
	if !ok {
		return
	}

	// Only process if the auth config references a secret.
	if apiAuth.Spec.Type != konnectv1alpha1.KonnectAPIAuthTypeSecretRef || apiAuth.Spec.SecretRef == nil {
		return
	}

	secretNamespace := apiAuth.Spec.SecretRef.Namespace
	if secretNamespace == "" {
		secretNamespace = apiAuth.Namespace
	}

	req := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: secretNamespace,
			Name:      apiAuth.Spec.SecretRef.Name,
		},
	}

	q.Add(req)
}
