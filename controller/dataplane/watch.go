package dataplane

import (
	"context"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
)

// DataPlaneWatchBuilder creates a controller builder pre-configured with
// the necessary watches for DataPlane resources that are managed by
// the operator.
func DataPlaneWatchBuilder(mgr ctrl.Manager) *builder.Builder {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch DataPlane objects.
		For(&operatorv1beta1.DataPlane{}).
		// Watch for changes in Secrets created by the dataplane controller.
		Owns(&corev1.Secret{}).
		// Watch for changes in Services created by the dataplane controller.
		Owns(&corev1.Service{}).
		// Watch for changes in Deployments created by the dataplane controller.
		Owns(&appsv1.Deployment{}).
		// Watch for changes in HPA created by the dataplane controller.
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		// Watch for changes in PodDisruptionBudgets created by the dataplane controller.
		Owns(&policyv1.PodDisruptionBudget{}).
		// Watch for changes in ConfigMaps created by the dataplane controller.
		Owns(&corev1.ConfigMap{}).
		// Watch for changes in ConfigMaps that are mapped to KongPluginInstallation objects.
		// They may trigger reconciliation of DataPlane resources.
		WatchesRawSource(
			source.Kind(
				mgr.GetCache(),
				&corev1.ConfigMap{},
				handler.TypedEnqueueRequestsFromMapFunc(listDataPlanesReferencingKongPluginInstallation(mgr.GetClient())),
			),
		).
		WatchesRawSource(
			source.Kind(
				mgr.GetCache(),
				&operatorv1alpha1.KonnectExtension{},
				handler.TypedEnqueueRequestsFromMapFunc(listDataPlanesReferencingKonnectExtension(mgr.GetClient())),
			),
		)
}

func listDataPlanesReferencingKongPluginInstallation(
	c client.Client,
) handler.TypedMapFunc[*corev1.ConfigMap, reconcile.Request] {
	return func(
		ctx context.Context, kpiCM *corev1.ConfigMap,
	) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		// Find all DataPlane resources referencing KongPluginInstallation
		// that maps to the ConfigMap enqueued for reconciliation.
		kpiToFind := kpiCM.Annotations[consts.AnnotationMappedToKongPluginInstallation]
		if kpiToFind == "" {
			return nil
		}
		var dataPlaneList operatorv1beta1.DataPlaneList
		if err := c.List(ctx, &dataPlaneList, client.MatchingFields{
			index.KongPluginInstallationsIndex: kpiToFind,
		}); err != nil {
			logger.Error(err, "Failed to list DataPlanes in watch", "KongPluginInstallation", kpiToFind)
			return nil
		}
		return lo.Map(dataPlaneList.Items, func(dp operatorv1beta1.DataPlane, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&dp),
			}
		})
	}
}

func listDataPlanesReferencingKonnectExtension(
	c client.Client,
) handler.TypedMapFunc[*operatorv1alpha1.KonnectExtension, reconcile.Request] {
	return func(
		ctx context.Context, ext *operatorv1alpha1.KonnectExtension,
	) []reconcile.Request {
		logger := ctrllog.FromContext(ctx)

		// Find all DataPlane resources referencing KonnectExtension
		// that maps to the KonnectExtension enqueued for reconciliation.
		var dataPlaneList operatorv1beta1.DataPlaneList
		if err := c.List(ctx, &dataPlaneList, client.MatchingFields{
			index.KonnectExtensionIndex: ext.Namespace + "/" + ext.Name,
		}); err != nil {
			logger.Error(err, "Failed to list DataPlanes in watch", "extensionKind", operatorv1alpha1.KonnectExtensionKind)
			return nil
		}
		return lo.Map(dataPlaneList.Items, func(dp operatorv1beta1.DataPlane, _ int) reconcile.Request {
			return reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&dp),
			}
		})
	}
}
