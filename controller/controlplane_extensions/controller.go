package controlplane_extensions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	configurationv1 "github.com/kong/kong-operator/apis/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/apis/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/controller/pkg/extensions"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	osslogging "github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/consts"
)

const (

	// GatewayOperatorControlPlaneNameManagingPluginsLabel is the label set on
	// Services to indicate that the ControlPlane is managing plugins for the
	// Service.
	GatewayOperatorControlPlaneNameManagingPluginsLabel = consts.OperatorLabelPrefix + "control-plane-managing-plugins-name"

	// GatewayOperatorControlPlaneNamespaceManagingPluginsLabel is the label set on
	// Services to indicate that the ControlPlane's namespace that is managing plugins
	// for the Service.
	GatewayOperatorControlPlaneNamespaceManagingPluginsLabel = consts.OperatorLabelPrefix + "control-plane-managing-plugins-namespace"

	// GatewayOperatorControlPlaneManagedPluginsAnnotation is the annotationset on
	// Services to indicate which plugins attached to this Service are managed
	// by the ControlPlane.
	// The annotation value is set to a comma separated list of KongPlugin names
	// that are managed by the ControlPlane.
	GatewayOperatorControlPlaneManagedPluginsAnnotation = consts.OperatorAnnotationPrefix + "control-plane-managed-plugins"
)

// ScrapeUpdateNotifier is an interface for notifying the scrapers manager
// about the need to add or remove a scraper for a DataPlane associated with
// the provided ControlPlane.
type ScrapeUpdateNotifier interface {
	NotifyAdd(ctx context.Context, cp *gwtypes.ControlPlane)
	NotifyRemove(ctx context.Context, cp types.NamespacedName)
}

// Reconciler reconciles ControlPlane plugins as specified in
// ControlPlane's spec.controlPlaneOptions.dataplanePlugins field.
// It ensures that the KongPlugin instances are created and their configuration
// is up to date.
// It also ensures that the Services that have their plugins managed by the
// ControlPlane have the correct annotation set.
type Reconciler struct {
	client.Client
	CacheSyncTimeout                time.Duration
	LoggingMode                     osslogging.Mode
	DataPlaneScraperManagerNotifier ScrapeUpdateNotifier
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			CacheSyncTimeout: r.CacheSyncTimeout,
		}).
		// Watch for changes to owned ControlPlane that had DataPlane.
		For(&gwtypes.ControlPlane{},
			builder.WithPredicates(
				ControlPlaneDataPlanePluginsSpecChangedPredicate{},
			),
		).
		// Enqueue requests for KongPlugins that are owned by ControlPlanes.
		Watches(&configurationv1.KongPlugin{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueControlPlaneOwningKongPluginFunc(r.Client),
			),
		).
		// Enqueue requests when Services that have their plugins managed
		// by the ControlPlane change.
		Watches(&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueControlPlaneForServicesThatHavePluginsManaged(),
			),
		).
		// Enqueue requests when Services that should have their plugins managed
		// by the ControlPlane are created in the cluster and at that point do not
		// have the backreference through the means of
		// "gateway-operator.konghq.com/control-plane-managing-plugins-name" label
		Watches(&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueControlPlaneForServicesThatHavePluginsConfigured(r.Client),
			),
			builder.WithPredicates(
				predicate.Funcs{
					CreateFunc: func(event.CreateEvent) bool {
						return true
					},
				},
			),
		).
		// Enqueue requests when DataPlaneMetricsExtensions have association from
		// a ControlPlane. At that point, the ControlPlane should be enqueued.
		Watches(&operatorv1alpha1.DataPlaneMetricsExtension{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueControlPlaneForDataPlaneMetricsExtension(r.Client),
			),
		).
		// Enqueue requests when DataPlanes change. This is important to ensure that
		// the scrapers are up to date and are scraping the correct targets.
		Watches(&operatorv1beta1.DataPlane{},
			handler.EnqueueRequestsFromMapFunc(
				enqueueControlPlaneForDataPlane(r.Client),
			),
		).
		Complete(r)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "controlplane_extensions", r.LoggingMode)

	log.Trace(logger, "reconciling ControlPlane extensions", "req", req)
	controlplane := new(gwtypes.ControlPlane)

	if err := r.Get(ctx, req.NamespacedName, controlplane); err != nil {
		if k8serrors.IsNotFound(err) {
			r.DataPlaneScraperManagerNotifier.NotifyRemove(ctx, types.NamespacedName{
				Name:      req.Name,
				Namespace: req.Namespace,
			})

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !controlplane.DeletionTimestamp.IsZero() {
		if controlplane.DeletionTimestamp.After(time.Now()) {
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Until(controlplane.DeletionTimestamp.Time),
			}, nil
		}
		return ctrl.Result{}, nil
	}

	// DataPlaneMetricsExtension
	if err := r.ensureDataPlaneMetricsExtensions(ctx, controlplane); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureDataPlaneMetricsExtensions ensures that the metrics plugin is enabled for the services
// specified in the DataPlanePluginOptions.Metrics.ServiceSelector.MatchNames
// and that the plugin config is up to date.
// It also ensures that the plugin is disabled for services that are not in the
// ServiceSelector.MatchNames.
func (r *Reconciler) ensureDataPlaneMetricsExtensions(ctx context.Context, controlplane *gwtypes.ControlPlane) error {
	logger := log.GetLogger(ctx, "controlplane_dataplanemetrics_extension", r.LoggingMode)
	extensions, err := extensions.GetAllDataPlaneMetricExtensionsForControlPlane(ctx, r.Client, controlplane)
	// In case there is an error, we don't want to return early because we still want to perform the cleanup.
	if err != nil {
		log.Error(logger, err, "failed to get DataPlaneMetricsExtensions for ControlPlane", "controlplane")
	}

	if len(extensions) > 0 {
		r.DataPlaneScraperManagerNotifier.NotifyAdd(ctx, controlplane)
	} else {
		r.DataPlaneScraperManagerNotifier.NotifyRemove(ctx, client.ObjectKeyFromObject(controlplane))
	}

	svcToExt := make(map[types.NamespacedName]*operatorv1alpha1.DataPlaneMetricsExtension)
	for _, ext := range extensions {
		for _, svcSS := range ext.Spec.ServiceSelector.MatchNames {
			svcNN := types.NamespacedName{
				Name:      svcSS.Name,
				Namespace: controlplane.Namespace,
			}
			if v, ok := svcToExt[svcNN]; ok {
				err := fmt.Errorf(
					"DataPlaneMetricsExtension %v contains service ref %v that is already managed by DataPlaneMetricsExtension %v",
					client.ObjectKeyFromObject(&ext), svcNN, client.ObjectKeyFromObject(v),
				)
				logger.Error(err, "failed to ensure metrics extension", "extension", client.ObjectKeyFromObject(&ext))
				return err
			}
			svcToExt[svcNN] = &ext
		}
	}

	svcListWithManagedLabel, err := listServicesThatHavePluginsManagedByControlPlane(ctx, controlplane, r.Client)
	if err != nil {
		return err
	}

	// Find all services with GatewayOperatorControlPlaneManagingPluginsLabel
	// label set to the name of currently reconciled ControlPlane and if they
	// are not in DataPlanePluginOptions.Metrics.ServiceSelector.MatchNames,
	// remove the label, annotation and delete the plugin if it still exits.
	for _, svc := range svcListWithManagedLabel {
		if _, ok := svcToExt[client.ObjectKeyFromObject(&svc)]; ok {
			// If the Service name is in one of the extensions ServiceSelector.MatchNames we don't do anything.
			// We'll enforce the plugin config below where we iterate over matchNames.
			continue
		}

		prometheusPluginName := prometheusPluginNameForSvc(&svc)
		old := svc.DeepCopy()
		if svc.Labels != nil {
			delete(svc.Labels, GatewayOperatorControlPlaneNameManagingPluginsLabel)
			delete(svc.Labels, GatewayOperatorControlPlaneNamespaceManagingPluginsLabel)
		}
		ensureKongPluginsAnnotationIsUnsetForPrometheusPlugin(&svc, prometheusPluginName)
		if err := r.Patch(ctx, &svc, client.MergeFrom(old)); err != nil {
			return fmt.Errorf("failed to Service %s: %w", client.ObjectKeyFromObject(&svc), err)
		}

		prometheusPlugin := configurationv1.KongPlugin{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prometheusPluginName,
				Namespace: svc.Namespace,
			},
		}
		if err := r.Delete(ctx, &prometheusPlugin); err != nil {
			return fmt.Errorf("failed to delete Prometheus KongPlugin for Service %s: %w", client.ObjectKeyFromObject(&svc), err)
		}
	}

	// For each service in DataPlaneMetricsExtensions' ServiceSelector.MatchNames,
	// ensure the Kong Plugin exists and its config is up to date.
	for svcNN, ext := range svcToExt {
		svc := corev1.Service{}
		if err := r.Get(ctx, svcNN, &svc); err != nil {
			logger.Error(err, "failed to get Service to enable metrics plugin on", "service", svcNN)
			continue
		}

		prometheusPlugin, err := r.ensurePrometheusPlugin(ctx, &svc, controlplane, ext)
		if err != nil {
			logger.Error(err, "failed to ensure Prometheus Plugin for Service", "service", svcNN)
			continue
		}

		old := svc.DeepCopy()

		if svc.Labels == nil {
			svc.Labels = make(map[string]string)
		}
		svc.Labels[GatewayOperatorControlPlaneNameManagingPluginsLabel] = controlplane.Name
		svc.Labels[GatewayOperatorControlPlaneNamespaceManagingPluginsLabel] = controlplane.Namespace
		ensureKongPluginsAnnotationIsSetForPrometheusPlugin(&svc, prometheusPlugin)
		if err := r.Patch(ctx, &svc, client.MergeFrom(old)); err != nil {
			logger.Error(err, "failed to patch Service to enable metrics plugin on", "service", svcNN)
			continue
		}
	}
	return err
}

func listServicesThatHavePluginsManagedByControlPlane(
	ctx context.Context,
	controlplane *gwtypes.ControlPlane,
	cl client.Client,
) ([]corev1.Service, error) {
	cpNameLabelReq, err := labels.NewRequirement(
		GatewayOperatorControlPlaneNameManagingPluginsLabel, selection.Equals, []string{controlplane.Name},
	)
	if err != nil {
		return nil, err
	}
	cpNamespaceLabelReq, err := labels.NewRequirement(
		GatewayOperatorControlPlaneNamespaceManagingPluginsLabel, selection.Equals, []string{controlplane.Namespace},
	)
	if err != nil {
		return nil, err
	}

	svcListWithManagedLabel := &corev1.ServiceList{}
	err = cl.List(ctx, svcListWithManagedLabel, &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*cpNameLabelReq).Add(*cpNamespaceLabelReq),
	})
	if err != nil {
		return nil, err
	}
	return svcListWithManagedLabel.Items, nil
}

func ensureStringInCommaSeparatedString(v, commaSeparated string) string {
	split := strings.Split(commaSeparated, ",")
	if lo.Contains(split, v) {
		return commaSeparated
	}
	return commaSeparated + "," + v
}

func ensureStringNotInCommaSeparatedString(v, commaSeparated string) string {
	split := strings.Split(commaSeparated, ",")
	if lo.Contains(split, v) {
		return strings.Join(
			lo.Filter(split, func(s string, _ int) bool {
				return s != v
			}),
			",",
		)
	}
	return commaSeparated
}

// ensurePrometheusPlugin ensures that the Prometheus plugin exists for the given
// Service and that it's up to date.
func (r *Reconciler) ensurePrometheusPlugin(
	ctx context.Context, svc *corev1.Service, controlplane *gwtypes.ControlPlane, ext *operatorv1alpha1.DataPlaneMetricsExtension,
) (*configurationv1.KongPlugin, error) {
	generatedPlugin, err := prometheusPluginForSvc(svc, controlplane, ext)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Prometheus KongPlugin: %w", err)
	}

	if err := controllerutil.SetControllerReference(controlplane, generatedPlugin, r.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to set owner reference for Prometheus KongPlugin for Service %s: %w", client.ObjectKeyFromObject(svc), err)
	}

	prometheusPluginActual := configurationv1.KongPlugin{}
	pluginNN := client.ObjectKeyFromObject(generatedPlugin)
	svcNN := client.ObjectKeyFromObject(svc)
	if err = r.Get(ctx, pluginNN, &prometheusPluginActual); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get Prometheus KongPlugin %s for Service %s: %w", pluginNN, svcNN, err)
		}

		// Create the plugin if it doesn't exist.
		if err := r.Create(ctx, generatedPlugin); err != nil {
			return nil, fmt.Errorf("failed to create Prometheus KongPlugin for Service %s: %w", svcNN, err)
		}
		return generatedPlugin, nil
	}

	// If it exists, ensure it's up to date.
	if !cmp.Equal(generatedPlugin.Config, &prometheusPluginActual.Config) ||
		!cmp.Equal(generatedPlugin.Annotations, &prometheusPluginActual.Annotations) ||
		!cmp.Equal(generatedPlugin.OwnerReferences, &prometheusPluginActual.OwnerReferences) ||
		!cmp.Equal(generatedPlugin.Labels, &prometheusPluginActual.Labels) ||
		!cmp.Equal(generatedPlugin.Finalizers, &prometheusPluginActual.Finalizers) ||
		!cmp.Equal(generatedPlugin.InstanceName, &prometheusPluginActual.InstanceName) ||
		!cmp.Equal(generatedPlugin.PluginName, &prometheusPluginActual.PluginName) ||
		!cmp.Equal(generatedPlugin.Disabled, &prometheusPluginActual.Disabled) {
		prometheusPluginActual.Config = generatedPlugin.Config
		prometheusPluginActual.Disabled = generatedPlugin.Disabled
		prometheusPluginActual.Annotations = generatedPlugin.Annotations
		prometheusPluginActual.Labels = generatedPlugin.Labels
		prometheusPluginActual.Finalizers = generatedPlugin.Finalizers
		prometheusPluginActual.InstanceName = generatedPlugin.InstanceName
		prometheusPluginActual.PluginName = generatedPlugin.PluginName
		prometheusPluginActual.OwnerReferences = generatedPlugin.OwnerReferences
		if err := r.Update(ctx, &prometheusPluginActual); err != nil {
			return nil, fmt.Errorf("failed to updated Prometheus KongPlugin %s for Service %s: %w", pluginNN, svcNN, err)
		}
	}

	return &prometheusPluginActual, nil
}

func ensureKongPluginsAnnotationIsSetForPrometheusPlugin(svc *corev1.Service, prometheusPlugin *configurationv1.KongPlugin) {
	if svc.Annotations == nil {
		svc.Annotations = make(map[string]string)
	}

	if ann, ok := svc.Annotations[consts.KongIngressControllerPluginsAnnotation]; !ok {
		svc.Annotations[consts.KongIngressControllerPluginsAnnotation] = prometheusPlugin.Name
	} else if ann != prometheusPlugin.Name {
		pluginNames := strings.Split(ann, ",")
		ok = lo.Contains(pluginNames, prometheusPlugin.Name)
		if !ok {
			svc.Annotations[consts.KongIngressControllerPluginsAnnotation] = ann + "," + prometheusPlugin.Name
		}
	}

	if ann, ok := svc.Annotations[GatewayOperatorControlPlaneManagedPluginsAnnotation]; ok {
		ann = ensureStringInCommaSeparatedString(prometheusPlugin.Name, ann)
		svc.Annotations[GatewayOperatorControlPlaneManagedPluginsAnnotation] = ann
	} else {
		svc.Annotations[GatewayOperatorControlPlaneManagedPluginsAnnotation] = prometheusPlugin.Name
	}
}

func ensureKongPluginsAnnotationIsUnsetForPrometheusPlugin(svc *corev1.Service, prometheusPluginName string) {
	if svc.Annotations == nil {
		return
	}

	if ann, ok := svc.Annotations[consts.KongIngressControllerPluginsAnnotation]; ok {
		if ann == prometheusPluginName {
			// Short circuit if the annotation only contains the managed plugin.
			delete(svc.Annotations, consts.KongIngressControllerPluginsAnnotation)
		} else {
			// If there are other plugins, remove the managed plugin from
			// the comma separated list.
			pluginNames := strings.Split(ann, ",")
			filteredPluginNames := lo.Filter(pluginNames,
				func(pluginName string, _ int) bool {
					return pluginName != prometheusPluginName
				},
			)
			svc.Annotations[consts.KongIngressControllerPluginsAnnotation] = strings.Join(filteredPluginNames, ",")
		}
	}

	if ann, ok := svc.Annotations[GatewayOperatorControlPlaneManagedPluginsAnnotation]; ok {
		ann = ensureStringNotInCommaSeparatedString(prometheusPluginName, ann)
		if ann == "" {
			delete(svc.Annotations, GatewayOperatorControlPlaneManagedPluginsAnnotation)
		} else {
			svc.Annotations[GatewayOperatorControlPlaneManagedPluginsAnnotation] = ann
		}
	}
}
