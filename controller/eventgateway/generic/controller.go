/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package generic provides a generic reconciler for EventGateway kinds
// (Listener, BackendCluster, VirtualCluster). The reconciler is a no-op stub:
// it fetches the object and logs a message.
package generic

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// Object is the constraint for any EventGateway object pointer type the
// generic reconciler can handle. It must be a pointer to a concrete type T
// and satisfy client.Object.
type Object[T any] interface {
	*T
	client.Object
}

// Reconciler is a generic, no-op reconciler for EventGateway kinds. It fetches
// the requested object and emits a log line. It is intended as a scaffolding
// stub for future EventGateway controllers.
//
// Cache is shared across reconciler instances when the caller passes the same
// *ObjectCache to multiple Reconcilers; if nil, each reconciler creates its
// own.
type Reconciler[T any, PT Object[T]] struct {
	Client      client.Client
	LoggingMode logging.Mode
	Cache       *ObjectCache
}

// gatewayReferencer is implemented by EventGateway child kinds that carry a
// reference to their parent Gateway (Listener, BackendCluster, VirtualCluster).
type gatewayReferencer interface {
	GetGatewayRef() commonv1alpha1.ObjectRef
}

// listenerReferencer is implemented by kinds that reference a parent
// EventGatewayListener (e.g. EventGatewayListenerPolicy). Their parent Gateway
// is reached via the Listener's own GetGatewayRef.
type listenerReferencer interface {
	GetEventGatewayListenerRef() commonv1alpha1.ObjectRef
}

// SetupWithManager registers the reconciler with the controller manager for
// the type parameterised by PT.
func (r *Reconciler[T, PT]) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	if r.Cache == nil {
		r.Cache = NewObjectCache(nil)
	}
	if err := r.Cache.AddTo(mgr); err != nil {
		return fmt.Errorf("add object cache to manager: %w", err)
	}

	var obj T
	return ctrl.NewControllerManagedBy(mgr).
		Named(fmt.Sprintf("eventgateway-%s", reflect.TypeOf(obj).Name())).
		For(PT(&obj)).
		WithEventFilter(predicate.NewPredicateFuncs(r.parentGatewayIsOnPremise)).
		Complete(r)
}

// parentGatewayIsOnPremise returns true when the object's parent
// KonnectEventGateway runs on-premise, so only on-premise children reach this
// reconciler and Konnect-managed ones are filtered out. Objects whose parent
// cannot be resolved are admitted to be handled by Reconcile.
func (r *Reconciler[T, PT]) parentGatewayIsOnPremise(obj client.Object) bool {
	gwRef, ok, err := r.resolveGatewayRef(context.Background(), obj)
	if err != nil || !ok {
		return true
	}
	if gwRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || gwRef.NamespacedRef == nil {
		return false
	}
	namespace := obj.GetNamespace()
	if gwRef.NamespacedRef.Namespace != nil && *gwRef.NamespacedRef.Namespace != "" {
		namespace = *gwRef.NamespacedRef.Namespace
	}
	var eg konnectv1alpha1.KonnectEventGateway
	if err := r.Client.Get(context.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      gwRef.NamespacedRef.Name,
	}, &eg); err != nil {
		return true
	}
	return eg.Spec.Environment == konnectv1alpha1.EventGatewayEnvironmentOnPremise
}

// Reconcile fetches the object referenced by req and logs that it was seen.
func (r *Reconciler[T, PT]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	var obj T
	pObj := PT(&obj)
	if err := r.Client.Get(ctx, req.NamespacedName, pObj); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cache.Pop(pObj, req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling EventGateway resource",
		"kind", reflect.TypeOf(obj).Name(),
		"namespace", req.Namespace,
		"name", req.Name,
	)

	gatewayID, err := r.parentGatewayID(ctx, pObj)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve parent gateway id: %w", err)
	}
	if gatewayID == "" {
		logger.Info("parent EventGateway has no Status.ID yet; requeueing")
		return ctrl.Result{Requeue: true}, nil
	}

	r.Cache.Push(gatewayID, pObj)
	return ctrl.Result{}, nil
}

// parentGatewayID returns the Status.ID of the parent KonnectEventGateway
// referenced by obj, or an empty string if the parent reference is not a
// namespaced ref or the parent has not been programmed yet.
func (r *Reconciler[T, PT]) parentGatewayID(ctx context.Context, obj client.Object) (string, error) {
	gwRef, ok, err := r.resolveGatewayRef(ctx, obj)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}
	if gwRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || gwRef.NamespacedRef == nil {
		return "", nil
	}
	namespace := obj.GetNamespace()
	if gwRef.NamespacedRef.Namespace != nil && *gwRef.NamespacedRef.Namespace != "" {
		namespace = *gwRef.NamespacedRef.Namespace
	}
	var eg konnectv1alpha1.KonnectEventGateway
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      gwRef.NamespacedRef.Name,
	}, &eg); err != nil {
		return "", err
	}
	return eg.Status.ID, nil
}

// resolveGatewayRef returns the namespaced ObjectRef pointing at the parent
// KonnectEventGateway for obj. Kinds that reference a Listener (e.g.
// EventGatewayListenerPolicy) are resolved with one extra hop via the
// Listener's GetGatewayRef. The bool is false when obj has neither a gateway
// nor a listener reference.
func (r *Reconciler[T, PT]) resolveGatewayRef(ctx context.Context, obj client.Object) (commonv1alpha1.ObjectRef, bool, error) {
	if ref, ok := obj.(gatewayReferencer); ok {
		return ref.GetGatewayRef(), true, nil
	}
	ref, ok := obj.(listenerReferencer)
	if !ok {
		return commonv1alpha1.ObjectRef{}, false, nil
	}
	listenerRef := ref.GetEventGatewayListenerRef()
	if listenerRef.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || listenerRef.NamespacedRef == nil {
		return commonv1alpha1.ObjectRef{}, true, nil
	}
	namespace := obj.GetNamespace()
	if listenerRef.NamespacedRef.Namespace != nil && *listenerRef.NamespacedRef.Namespace != "" {
		namespace = *listenerRef.NamespacedRef.Namespace
	}
	var listener konnectv1alpha1.EventGatewayListener
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      listenerRef.NamespacedRef.Name,
	}, &listener); err != nil {
		return commonv1alpha1.ObjectRef{}, true, err
	}
	return listener.GetGatewayRef(), true, nil
}
