package konnect

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/consts"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// KongPluginReconciler reconciles a KongPlugin object.
type KongPluginReconciler struct {
	developmentMode bool
	client          client.Client
}

// NewKongPluginReconciler creates a new KongPluginReconciler.
func NewKongPluginReconciler(
	developmentMode bool,
	client client.Client,
) *KongPluginReconciler {
	return &KongPluginReconciler{
		developmentMode: developmentMode,
		client:          client,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KongPluginReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("KongPlugin").
		For(&configurationv1.KongPlugin{}).
		Watches(
			&configurationv1alpha1.KongPluginBinding{},
			handler.EnqueueRequestsFromMapFunc(r.mapKongPluginBindings),
		).
		Watches(
			&configurationv1alpha1.KongService{},
			handler.EnqueueRequestsFromMapFunc(r.mapKongServices),
			builder.WithPredicates(
				kongPluginsAnnotationChangedPredicate,
			),
		).
		Watches(
			&configurationv1alpha1.KongRoute{},
			handler.EnqueueRequestsFromMapFunc(r.mapKongRoutes),
			builder.WithPredicates(
				kongPluginsAnnotationChangedPredicate,
			),
		).
		Complete(r)
}

// Reconcile reconciles a KongPlugin object.
// The purpose of this reconciler is to handle annotations on Kong entities objects that reference KongPlugin objects.
// As a result of such annotations, KongPluginBinding objects are created and managed by the controller.
func (r *KongPluginReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var (
		entityTypeName = "KongPlugin"
		logger         = log.GetLogger(ctx, entityTypeName, r.developmentMode)
	)

	// Fetch the KongPlugin instance
	var kongPlugin configurationv1.KongPlugin
	if err := r.client.Get(ctx, req.NamespacedName, &kongPlugin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Debug(logger, "reconciling", kongPlugin)
	clientWithNamespace := client.NewNamespacedClient(r.client, kongPlugin.Namespace)

	// Get the pluginBindings that use this KongPlugin
	kongPluginBindingList := configurationv1alpha1.KongPluginBindingList{}
	err := clientWithNamespace.List(
		ctx,
		&kongPluginBindingList,
		client.MatchingFields{
			IndexFieldKongPluginBindingKongPluginReference: kongPlugin.Namespace + "/" + kongPlugin.Name,
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// TODO(mlavacca): So far we are supporting only KongService targets here. We need to implement
	// the same logic for KongRoute, KongConsumer, and KongConsumerGroup as well.
	// https://github.com/Kong/gateway-operator/issues/583

	kongServiceList := configurationv1alpha1.KongServiceList{}
	err = clientWithNamespace.List(ctx, &kongServiceList,
		client.MatchingFields{
			IndexFieldKongServiceOnReferencedPluginNames: kongPlugin.Namespace + "/" + kongPlugin.Name,
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed listing KongServices referencing %s KongPlugin: %w", client.ObjectKeyFromObject(kongPlugin), err)
	}

	kongRouteList := configurationv1alpha1.KongRouteList{}
	err = clientWithNamespace.List(ctx, &kongRouteList,
		client.MatchingFields{
			IndexFieldKongRouteOnReferencedPluginNames: kongPlugin.Namespace + "/" + kongPlugin.Name,
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed listing KongRoutes referencing %s KongPlugin: %w", client.ObjectKeyFromObject(kongPlugin), err)
	}

	foreignRelations := ForeignRelations{
		Service: lo.Filter(kongServiceList.Items,
			func(s configurationv1alpha1.KongService, _ int) bool { return s.DeletionTimestamp.IsZero() },
		),
		Route: lo.Filter(kongRouteList.Items,
			func(s configurationv1alpha1.KongRoute, _ int) bool { return s.DeletionTimestamp.IsZero() },
		),
		// TODO consumers and consumer groups
	}
	grouped, err := foreignRelations.GroupByControlPlane(ctx, r.client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// pluginReferenceFound represents whether the plugin is referenced by any resource.
	var pluginReferenceFound bool
	groupedCombinations := grouped.GetCombinations()

	// Delete the KongPluginBindings that are not used anymore.
	if err := deleteUnusedKongPluginBindings(ctx, logger, clientWithNamespace, &kongPlugin, groupedCombinations, kongPluginBindingList.Items); err != nil {
		return ctrl.Result{}, err
	}

	for cpNN, relations := range groupedCombinations {
		for _, rel := range relations {
			pluginReferenceFound = true

			builder := NewKongPluginBindingBuilder().
				WithGenerateName(kongPlugin.Name + "-").
				WithNamespace(kongPlugin.Namespace).
				WithPluginRef(kongPlugin.Name).
				WithControlPlaneRef(&configurationv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Namespace: cpNN.Namespace,
						Name:      cpNN.Name,
					},
				})

			l := kongPluginBindingList.Items

			if rel.Service != "" {
				l = lo.Filter(l, func(pb configurationv1alpha1.KongPluginBinding, _ int) bool {
					return pb.Spec.Targets.ServiceReference != nil &&
						pb.Spec.Targets.ServiceReference.Name == rel.Service
				})
				builder.WithServiceTarget(rel.Service)
			}
			if rel.Route != "" {
				l = lo.Filter(l, func(pb configurationv1alpha1.KongPluginBinding, _ int) bool {
					return pb.Spec.Targets.RouteReference != nil &&
						pb.Spec.Targets.RouteReference.Name == rel.Route
				})
				builder.WithRouteTarget(rel.Route)
			}

			// TODO consumers and consumer groups

			kongPluginBinding := builder.Build()
			if err := controllerutil.SetOwnerReference(&kongPlugin, kongPluginBinding, clientWithNamespace.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
			}

			switch len(l) {
			case 0:
				if err = clientWithNamespace.Create(ctx, kongPluginBinding); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to create KongPluginBinding: %w", err)
				}
				log.Debug(logger, "Managed KongPluginBinding created", kongPluginBinding)

			case 1:
				existing := l[0]
				if reflect.DeepEqual(existing.Spec.Targets.ServiceReference, kongPluginBinding.Spec.Targets.ServiceReference) &&
					reflect.DeepEqual(existing.Spec.Targets.RouteReference, kongPluginBinding.Spec.Targets.RouteReference) {
					// TODO consumers and consumer groups checks
					continue
				}

				existing.Spec.Targets = kongPluginBinding.Spec.Targets

				if err = clientWithNamespace.Update(ctx, &existing); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to update KongPluginBinding: %w", err)
				}
				log.Debug(logger, "Managed KongPluginBinding updated", kongPluginBinding)

			default:
				// TODO: remove surplus KongPluginBindings
			}

		}
	}

	pluginReferenceFound = pluginReferenceFound || len(kongPluginBindingList.Items) > 0

	// If an entity is using the plugin, add a finalizer on the plugin.
	// The KongPlugin cannot be deleted until all objects that reference it are
	// deleted or do not reference it anymore.
	if pluginReferenceFound {
		if controllerutil.AddFinalizer(&kongPlugin, consts.PluginInUseFinalizer) {
			if err = r.client.Update(ctx, &kongPlugin); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
			log.Debug(logger, "KongPlugin finalizer added", kongPlugin, "finalizer", consts.PluginInUseFinalizer)
			return ctrl.Result{}, nil
		}
	} else {
		if controllerutil.RemoveFinalizer(&kongPlugin, consts.PluginInUseFinalizer) {
			if err = r.client.Update(ctx, &kongPlugin); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
			log.Debug(logger, "KongPlugin finalizer removed", kongPlugin, "finalizer", consts.PluginInUseFinalizer)
			return ctrl.Result{}, nil
		}
	}

	log.Debug(logger, "reconciliation completed", kongPlugin)
	return ctrl.Result{}, nil
}

func deleteKongPluginBindings(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	pluginBindingsToDelete map[types.NamespacedName]configurationv1alpha1.KongPluginBinding,
) error {
	for _, pb := range pluginBindingsToDelete {
		// NOTE: we check the deletion timestamp here because attempting to delete
		// and return here would prevent KongPlugin finalizer update.
		if !pb.DeletionTimestamp.IsZero() {
			continue
		}
		if err := cl.Delete(ctx, &pb); err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}
			return err
		}
		log.Info(logger, "KongPluginBinding deleted", pb)
	}
	return nil
}

func ownerRefIsPlugin(kongPlugin *configurationv1.KongPlugin) func(ownerRef metav1.OwnerReference) bool {
	return func(ownerRef metav1.OwnerReference) bool {
		return ownerRef.Kind == "KongPlugin" &&
			ownerRef.Name == kongPlugin.Name &&
			ownerRef.UID == kongPlugin.UID
	}
}

func deleteUnusedKongPluginBindings(
	ctx context.Context,
	logger logr.Logger,
	clientWithNamespace client.Client,
	kongPlugin *configurationv1.KongPlugin,
	groupedCombinations map[types.NamespacedName][]Rel,
	kongPluginBindings []configurationv1alpha1.KongPluginBinding,
) error {
	kongServiceList := configurationv1alpha1.KongServiceList{}
	if err := clientWithNamespace.List(ctx, &kongServiceList); err != nil {
		return err
	}
	serviceMap := lo.SliceToMap(
		kongServiceList.Items,
		func(obj configurationv1alpha1.KongService) (string, configurationv1alpha1.KongService) {
			return obj.Name, obj
		},
	)

	kongRouteList := configurationv1alpha1.KongRouteList{}
	if err := clientWithNamespace.List(ctx, &kongRouteList); err != nil {
		return err
	}
	routeMap := lo.SliceToMap(
		kongRouteList.Items,
		func(obj configurationv1alpha1.KongRoute) (string, configurationv1alpha1.KongRoute) {
			return obj.Name, obj
		},
	)

	pluginBindingsToDelete := make(map[types.NamespacedName]configurationv1alpha1.KongPluginBinding)
	for _, pb := range kongPluginBindings {
		// If the KongPluginBinding has a deletion timestamp, do not delete it.
		if !pb.DeletionTimestamp.IsZero() {
			continue
		}

		// If the KongPluginBinding is unmanaged (created not using an annotation), skip it, do not delete it.
		if !lo.ContainsBy(pb.OwnerReferences, ownerRefIsPlugin(kongPlugin)) {
			continue
		}

		cpRef := pb.Spec.ControlPlaneRef
		if cpRef == nil ||
			cpRef.KonnectNamespacedRef == nil ||
			cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
			continue
		}

		combinations, ok := groupedCombinations[types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: pb.Namespace,
			Name:      cpRef.KonnectNamespacedRef.Name,
		}]
		if !ok {
			pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
			continue
		}

		// If the konghq.com/plugins annotation is not present, it doesn't contain
		// the plugin in question or the object referring to the plugin has a non zero deletion timestamp,
		// we need to delete all the managed KongPluginBindings that reference the object.

		serviceRef := pb.Spec.Targets.ServiceReference
		routeRef := pb.Spec.Targets.RouteReference
		switch {

		case serviceRef != nil && routeRef != nil:
			combinationFound := false
			for _, rel := range combinations {
				if rel.Service != serviceRef.Name &&
					rel.Route != routeRef.Name {
					continue
				}
				combinationFound = true
				break
			}

			if !combinationFound {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}

			s, serviceExists := serviceMap[serviceRef.Name]
			r, routeExists := routeMap[routeRef.Name]
			if !serviceExists || !routeExists ||
				!objHasPluginConfigured(&s, kongPlugin.Name) || !s.DeletionTimestamp.IsZero() ||
				!objHasPluginConfigured(&r, kongPlugin.Name) || !r.DeletionTimestamp.IsZero() {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}

		case serviceRef != nil:
			combinationFound := false
			for _, rel := range combinations {
				if rel.Service != serviceRef.Name ||
					rel.Route != "" {
					continue
				}
				combinationFound = true
				break
			}

			if !combinationFound {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}

			s, serviceExists := serviceMap[serviceRef.Name]
			if !serviceExists ||
				!objHasPluginConfigured(&s, kongPlugin.Name) || !s.DeletionTimestamp.IsZero() {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}

		case routeRef != nil:
			combinationFound := false
			for _, rel := range combinations {
				if rel.Route != routeRef.Name ||
					rel.Service != "" {
					continue
				}
				combinationFound = true
				break
			}

			if !combinationFound {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}

			r, routeExists := routeMap[routeRef.Name]
			if !routeExists ||
				!objHasPluginConfigured(&r, kongPlugin.Name) || !r.DeletionTimestamp.IsZero() {
				pluginBindingsToDelete[client.ObjectKeyFromObject(&pb)] = pb
				continue
			}
		}

	}

	if err := deleteKongPluginBindings(ctx, logger, clientWithNamespace, pluginBindingsToDelete); err != nil {
		return err
	}

	return nil
}

func objHasPluginConfigured(obj client.Object, pluginName string) bool {
	plugins, ok := obj.GetAnnotations()[consts.PluginsAnnotationKey]
	if !ok {
		return false
	}
	return slices.Contains(strings.Split(plugins, ","), pluginName)
}
