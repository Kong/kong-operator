package mcpserver

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// ensureKongEntities fetches the Kong entities (services, routes) that Konnect
// has preallocated for the given MCPServer, each with an already-assigned UUID.
// It ensures the corresponding Kubernetes custom resources are created using
// those exact UUIDs and cleans up stale resources that are no longer expected.
func (r *MCPServerReconciler) ensureKongEntities(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	sdk sdkops.SDKWrapper,
) error {
	logger := log.GetLogger(ctx, "mcpserver", r.LoggingMode)

	cpID := mcpServer.GetControlPlaneID()
	mcpServerID := mcpServer.GetKonnectID()

	resp, err := sdk.GetMCPServersSDK().GetMcpServerKongEntities(ctx, sdkkonnectops.GetMcpServerKongEntitiesRequest{
		ControlPlaneID: cpID,
		McpServerID:    mcpServerID,
	})
	if err != nil {
		return fmt.Errorf("failed to get kong entities for MCPServer %s/%s: %w",
			mcpServer.Namespace, mcpServer.Name, err)
	}
	if resp == nil || resp.KongEntitiesResponse == nil {
		return fmt.Errorf("got nil kong entities response for MCPServer %s/%s",
			mcpServer.Namespace, mcpServer.Name)
	}

	entities := resp.KongEntitiesResponse

	// Build a mapping from Konnect service ID -> KongService CR name so that
	// routes can reference their parent service.
	svcIDToName := make(map[string]string, len(entities.Services))

	// ------------------------------------------------------------------
	// Ensure KongService CRs
	// ------------------------------------------------------------------
	desiredServiceNames := make(map[string]struct{}, len(entities.Services))
	for _, svc := range entities.Services {
		res, svcNN, err := r.ensureKongService(ctx, mcpServer, svc)
		if err != nil {
			return err
		}
		if res != op.Noop {
			log.Info(logger, fmt.Sprintf("%s KongService for MCPServer", res),
				"namespace", mcpServer.Namespace, "name", mcpServer.Name, "service", svcNN.Name)
		}
		desiredServiceNames[svcNN.Name] = struct{}{}
		if svc.ID != nil {
			svcIDToName[*svc.ID] = svcNN.Name
		}
	}

	// Delete stale KongService CRs that are owned by this MCPServer but no
	// longer present in the remote response.
	if err := r.deleteStaleResources(ctx, mcpServer, &configurationv1alpha1.KongServiceList{}, desiredServiceNames); err != nil {
		return err
	}

	// ------------------------------------------------------------------
	// Ensure KongRoute CRs
	// ------------------------------------------------------------------
	desiredRouteNames := make(map[string]struct{}, len(entities.Routes))
	for _, route := range entities.Routes {
		res, routeNN, err := r.ensureKongRoute(ctx, mcpServer, route, svcIDToName)
		if err != nil {
			return err
		}
		if res != op.Noop {
			log.Info(logger, fmt.Sprintf("%s KongRoute for MCPServer", res),
				"namespace", mcpServer.Namespace, "name", mcpServer.Name, "route", routeNN.Name)
		}
		desiredRouteNames[routeNN.Name] = struct{}{}
	}

	// Delete stale KongRoute CRs.
	if err := r.deleteStaleResources(ctx, mcpServer, &configurationv1alpha1.KongRouteList{}, desiredRouteNames); err != nil {
		return err
	}

	// ------------------------------------------------------------------
	// Ensure KongPlugin and KongPluginBinding CRs
	// ------------------------------------------------------------------
	if _, err := r.ensureKongPlugins(ctx, mcpServer); err != nil {
		return err
	}

	if err := r.ensureKongPluginBindings(ctx, mcpServer, desiredServiceNames); err != nil {
		return err
	}

	return nil
}

// ----------------------------------------------------------------------------
// KongService
// ----------------------------------------------------------------------------

func (r *MCPServerReconciler) ensureKongService(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	svc sdkkonnectcomp.KongService,
) (op.Result, client.ObjectKey, error) {
	desired := generateKongService(mcpServer, svc, r.ClusterDomain)
	nn := client.ObjectKeyFromObject(desired)

	k8sutils.SetOwnerForObject(desired, mcpServer)
	k8sresources.LabelObjectAsMCPServerManaged(desired)

	existing := &configurationv1alpha1.KongService{}
	err := r.Get(ctx, nn, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nn, fmt.Errorf("failed to get KongService %s: %w", nn, err)
		}

		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nn, fmt.Errorf("failed to create KongService %s: %w", nn, err)
		}
		return op.Created, nn, nil
	}

	// TODO: enforce the KongService Spec

	return op.Noop, nn, nil
}

func generateKongService(mcpServer *konnectv1alpha1.MCPServer, svc sdkkonnectcomp.KongService, clusterDomain string) *configurationv1alpha1.KongService {
	nn := generateWorkloadNN(mcpServer)

	// Use the Kubernetes Service DNS name so that Kong routes traffic to the
	// in-cluster workload rather than using the host from the Konnect response.
	host := fmt.Sprintf("%s.%s.svc.%s", nn.Name, nn.Namespace, clusterDomain)

	return &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},

		Spec: configurationv1alpha1.KongServiceSpec{
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				ID:       svc.ID,
				Name:     &nn.Name,
				Host:     host,
				Port:     svc.Port,
				Protocol: sdkkonnectcomp.Protocol(svc.Protocol),
				Path:     &svc.Path,
			},
			ControlPlaneRef: &mcpServer.Spec.ControlPlaneRef,
		},
	}
}

// ----------------------------------------------------------------------------
// KongRoute
// ----------------------------------------------------------------------------

func (r *MCPServerReconciler) ensureKongRoute(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	route sdkkonnectcomp.KongRoute,
	svcIDToName map[string]string,
) (op.Result, client.ObjectKey, error) {
	desired := generateKongRoute(mcpServer, route, svcIDToName)
	nn := client.ObjectKeyFromObject(desired)

	k8sutils.SetOwnerForObject(desired, mcpServer)
	k8sresources.LabelObjectAsMCPServerManaged(desired)

	existing := &configurationv1alpha1.KongRoute{}
	err := r.Get(ctx, nn, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return op.Noop, nn, fmt.Errorf("failed to get KongRoute %s: %w", nn, err)
		}

		if err := r.Create(ctx, desired); err != nil {
			return op.Noop, nn, fmt.Errorf("failed to create KongRoute %s: %w", nn, err)
		}
		return op.Created, nn, nil
	}

	// TODO: enforce the KongRoute Spec

	return op.Noop, nn, nil
}

func generateKongRoute(
	mcpServer *konnectv1alpha1.MCPServer,
	route sdkkonnectcomp.KongRoute,
	svcIDToName map[string]string,
) *configurationv1alpha1.KongRoute {
	nn := generateWorkloadNN(mcpServer)

	kr := &configurationv1alpha1.KongRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				ID:      route.ID,
				Name:    &nn.Name,
				Paths:   route.Paths,
				Methods: route.Methods,
			},
		},
	}

	// If the route references a service, set the ServiceRef.
	// Otherwise, set the ControlPlaneRef for a serviceless route.
	if route.Service != nil && route.Service.ID != nil {
		if svcName, ok := svcIDToName[*route.Service.ID]; ok {
			kr.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: svcName,
				},
			}
		}
	}
	if kr.Spec.ServiceRef == nil {
		kr.Spec.ControlPlaneRef = &mcpServer.Spec.ControlPlaneRef
	}

	return kr
}

// ----------------------------------------------------------------------------
// Stale resource cleanup
// ----------------------------------------------------------------------------

// deleteStaleResources lists resources of the given type that are owned by the
// MCPServer and deletes those whose names are not in the desiredNames set.
func (r *MCPServerReconciler) deleteStaleResources(
	ctx context.Context,
	mcpServer *konnectv1alpha1.MCPServer,
	list client.ObjectList,
	desiredNames map[string]struct{},
) error {
	if err := r.List(ctx, list, client.InNamespace(mcpServer.Namespace)); err != nil {
		return fmt.Errorf("failed to list resources for stale cleanup: %w", err)
	}

	items := extractItems(list)
	for _, item := range items {
		if !isOwnedBy(item.GetOwnerReferences(), mcpServer.GetUID()) {
			continue
		}
		if _, ok := desiredNames[item.GetName()]; ok {
			continue
		}
		if err := r.Delete(ctx, item); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete stale resource %s/%s: %w",
				item.GetNamespace(), item.GetName(), err)
		}
	}
	return nil
}

// extractItems returns the items from a typed list as a slice of client.Object.
func extractItems(list client.ObjectList) []client.Object {
	switch l := list.(type) {
	case *configurationv1alpha1.KongServiceList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *configurationv1alpha1.KongRouteList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *configurationv1.KongPluginList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	case *configurationv1alpha1.KongPluginBindingList:
		items := make([]client.Object, len(l.Items))
		for i := range l.Items {
			items[i] = &l.Items[i]
		}
		return items
	}
	return nil
}
