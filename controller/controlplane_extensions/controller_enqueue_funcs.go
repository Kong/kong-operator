package controlplane_extensions

import (
	"context"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// enqueueControlPlaneOwningKongPluginFunc returns a function that enqueues
// ControlPlane reconciliation for events that impact KongPlugins, that are managed
// by that ControlPlane.
func enqueueControlPlaneOwningKongPluginFunc(
	cl client.Client,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		if obj == nil {
			return nil
		}

		p, ok := obj.(*configurationv1.KongPlugin)
		if !ok {
			return nil
		}

		managedBy, ok := p.Labels[consts.GatewayOperatorManagedByLabel]
		if !ok {
			return nil
		}
		if managedBy != "controlplane" {
			return nil
		}

		name, ok := p.Labels[consts.GatewayOperatorManagedByNameLabel]
		if !ok {
			return nil
		}

		namespace, ok := p.Labels[consts.GatewayOperatorManagedByNamespaceLabel]
		if !ok {
			return nil
		}

		nn := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}

		cp := gwtypes.ControlPlane{}
		if err := cl.Get(ctx, nn, &cp); err != nil {
			return nil
		}

		return []ctrl.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      cp.Name,
					Namespace: cp.Namespace,
				},
			},
		}
	}
}

// enqueueControlPlaneForServicesThatHavePluginsManaged returns a function that
// enqueues ControlPlane reconciliation for events impacting Services which have
// its plugins managed by a ControlPlane. The relationship is established by
// the presence of the label `gateway-operator.konghq.com/control-plane-managing-plugins`.
func enqueueControlPlaneForServicesThatHavePluginsManaged() handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		if obj == nil {
			return nil
		}

		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}
		if svc.Annotations == nil {
			return nil
		}
		name, ok := svc.Labels[GatewayOperatorControlPlaneNameManagingPluginsLabel]
		if !ok {
			return nil
		}

		namespace, ok := svc.Labels[GatewayOperatorControlPlaneNamespaceManagingPluginsLabel]
		if !ok {
			return nil
		}

		return []ctrl.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      name,
					Namespace: namespace,
				},
			},
		}
	}
}

// enqueueControlPlaneForServicesThatHavePluginsConfigured enqueue ControlPlane
// reconciliation for events impacting Services which have its plugins configured
// through ControlPlane's DataPlanePluginOptions.
//
// This is only triggered when the Services are created which at that point
// do not contain the backreference through the means of
// `gateway-operator.konghq.com/control-plane-managing-plugins-name` and
// `gateway-operator.konghq.com/control-plane-managing-plugins-namespace` labels.
func enqueueControlPlaneForServicesThatHavePluginsConfigured(
	cl client.Client,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		if obj == nil {
			return nil
		}

		svc, ok := obj.(*corev1.Service)
		if !ok {
			return nil
		}

		var controlplanes gwtypes.ControlPlaneList
		if err := cl.List(ctx, &controlplanes); err != nil {
			return nil
		}

		for _, controlplane := range controlplanes.Items {
			filteredRefs := lo.Filter(controlplane.Spec.Extensions, func(e commonv1alpha1.ExtensionRef, _ int) bool {
				return e.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind && e.Group == operatorv1alpha1.SchemeGroupVersion.Group
			})

			// NOTE: code below assumes 1:1 mapping between a Service and ControlPlane
			// extension. The reason for this stems from the fact that there should be only 1
			// plugin configuration per Service.
			var exts []*operatorv1alpha1.DataPlaneMetricsExtension
			for _, e := range filteredRefs {
				nn := types.NamespacedName{
					Name:      e.Name,
					Namespace: controlplane.Namespace,
				}
				if e.Namespace != nil {
					nn.Namespace = *e.Namespace
				}

				dpMetricExt := operatorv1alpha1.DataPlaneMetricsExtension{}
				if err := cl.Get(ctx, nn, &dpMetricExt); err != nil {
					continue
				}
				exts = append(exts, &dpMetricExt)
			}
			for _, ext := range exts {
				for _, svcMatchName := range ext.Spec.ServiceSelector.MatchNames {
					if svcMatchName.Name == svc.Name && ext.Namespace == svc.Namespace {
						return []ctrl.Request{
							{
								NamespacedName: types.NamespacedName{
									Name:      controlplane.Name,
									Namespace: controlplane.Namespace,
								},
							},
						}
					}
				}
			}

		}

		return nil
	}
}

func enqueueControlPlaneForDataPlaneMetricsExtension(
	cl client.Client,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		if obj == nil {
			return nil
		}

		ext, ok := obj.(*operatorv1alpha1.DataPlaneMetricsExtension)
		if !ok {
			return nil
		}

		var controlplanes gwtypes.ControlPlaneList
		if err := cl.List(ctx, &controlplanes); err != nil {
			return nil
		}

		// NOTE: code below assumes 1:1 mapping between a DataPlaneMetricsExtension and ControlPlane
		// and that there can only be one ControlPlane associated with a given DataPlaneMetricsExtension.
		var recs []ctrl.Request
		for _, controlplane := range controlplanes.Items {
			for _, e := range controlplane.Spec.Extensions {
				if e.Kind != operatorv1alpha1.DataPlaneMetricsExtensionKind &&
					e.Group != operatorv1alpha1.SchemeGroupVersion.Group {
					continue
				}

				if e.Name != ext.Name {
					continue
				}

				if e.Namespace != nil && *e.Namespace != ext.Namespace {
					continue
				}

				if controlplane.Namespace != ext.Namespace {
					continue
				}
				return []ctrl.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      controlplane.Name,
							Namespace: controlplane.Namespace,
						},
					},
				}
			}
		}

		return recs
	}
}

func enqueueControlPlaneForDataPlane(
	cl client.Client,
) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		if obj == nil {
			return nil
		}

		dp, ok := obj.(*operatorv1beta1.DataPlane)
		if !ok {
			return nil
		}

		// If the DataPlane is owned by a Gateway, we want to enqueue the ControlPlane
		// is also part of the same Gateway.
		var owner *metav1.OwnerReference
		for _, ref := range dp.OwnerReferences {
			if ref.APIVersion != gatewayv1.GroupVersion.String() ||
				ref.Kind != "Gateway" {
				owner = ref.DeepCopy()
				break
			}
		}

		var controlplanes gwtypes.ControlPlaneList
		if err := cl.List(ctx, &controlplanes, &client.ListOptions{
			Namespace: dp.Namespace,
		}); err != nil {
			return nil
		}

		for _, controlplane := range controlplanes.Items {
			if owner != nil {
				for _, ref := range controlplane.OwnerReferences {
					if ref.Name != owner.Name {
						continue
					}
					if ref.APIVersion != gatewayv1.GroupVersion.String() ||
						ref.Kind != "Gateway" {
						continue
					}
				}
			}

			switch controlplane.Spec.DataPlane.Type {
			case gwtypes.ControlPlaneDataPlaneTargetRefType:
				if controlplane.Spec.DataPlane.Ref == nil ||
					controlplane.Spec.DataPlane.Ref.Name != dp.Name {
					continue
				}

			case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
				if controlplane.Status.DataPlane == nil ||
					controlplane.Status.DataPlane.Name != dp.Name {
					continue
				}
			default:
				continue
			}

			return []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      controlplane.Name,
						Namespace: controlplane.Namespace,
					},
				},
			}
		}

		return nil
	}
}
