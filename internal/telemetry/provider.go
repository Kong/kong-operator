package telemetry

import (
	"context"

	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"github.com/kong/kubernetes-telemetry/pkg/types"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

const (
	// DataPlaneK8sResourceName is the registered name of resource in kubernetes for dataplanes.
	DataPlaneK8sResourceName = "dataplanes"
	// DataPlaneCountKind is the kind of provider reporting number of dataplanes.
	DataPlaneCountKind = provider.Kind("dataplanes_count")

	// ControlPlaneK8sResourceName is the registered name of resource in kubernetes for controlplanes.
	ControlPlaneK8sResourceName = "controlplanes"
	// ControlPlaneCountKind is the kind of provider reporting number of controlplanes.
	ControlPlaneCountKind = provider.Kind("controlplanes_count")

	// AIGatewayK8sResourceName is the registered name of resource in kubernetes for AIgateways.
	AIGatewayK8sResourceName = "aigateways"
	// AIGatewayCountKind is the kind of provider reporting number of AIGateways.
	AIGatewayCountKind = provider.Kind("aigateways_count")

	// StandaloneDataPlaneCountProviderName is the name of the standalone dataplane count provider.
	StandaloneDataPlaneCountProviderName = "standalone_dataplanes"

	// StandaloneControlPlaneCountProviderName is the name of the standalone controlplane count provider.
	StandaloneControlPlaneCountProviderName = "standalone_controlplanes"

	// RequestedDataPlaneReplicasCountProviderName is the name of the provider reporting requested replicas count for dataplanes.
	RequestedDataPlaneReplicasCountProviderName = "requested_dataplanes_replicas"

	// RequestedControlPlaneReplicasCountProviderName is the name of the provider reporting requested replicas count for controlplanes.
	RequestedControlPlaneReplicasCountProviderName = "requested_controlplanes_replicas"
)

// NewDataPlaneCountProvider creates a provider for number of dataplanes in the cluster.
func NewDataPlaneCountProvider(dyn dynamic.Interface, restMapper meta.RESTMapper) (provider.Provider, error) {
	return provider.NewK8sObjectCountProviderWithRESTMapper(
		DataPlaneK8sResourceName, DataPlaneCountKind, dyn, operatorv1beta1.DataPlaneGVR(), restMapper,
	)
}

// NewControlPlaneCountProvider creates a provider for number of dataplanes in the cluster.
func NewControlPlaneCountProvider(dyn dynamic.Interface, restMapper meta.RESTMapper) (provider.Provider, error) {
	return provider.NewK8sObjectCountProviderWithRESTMapper(
		ControlPlaneK8sResourceName, ControlPlaneCountKind, dyn, gwtypes.ControlPlaneGVR(), restMapper,
	)
}

// NewAIgatewayCountProvider creates a provider for number of dataplanes in the cluster.
func NewAIgatewayCountProvider(dyn dynamic.Interface, restMapper meta.RESTMapper) (provider.Provider, error) {
	return provider.NewK8sObjectCountProviderWithRESTMapper(
		AIGatewayK8sResourceName, AIGatewayCountKind, dyn, operatorv1alpha1.AIGatewayGVR(), restMapper,
	)
}

func NewStandaloneDataPlaneCountProvider(cl client.Client) (provider.Provider, error) {
	return provider.NewFunctorProvider(StandaloneDataPlaneCountProviderName, func(ctx context.Context) (types.ProviderReport, error) {
		dataPlanes := operatorv1beta1.DataPlaneList{}
		if err := cl.List(ctx, &dataPlanes); err != nil {
			return types.ProviderReport{}, err
		}
		count := lo.CountBy(dataPlanes.Items, func(dp operatorv1beta1.DataPlane) bool {
			return len(dp.GetOwnerReferences()) == 0
		})
		return types.ProviderReport{
			"k8s_standalone_dataplanes_count": count,
		}, nil
	})
}

func NewStandaloneControlPlaneCountProvider(cl client.Client) (provider.Provider, error) {
	return provider.NewFunctorProvider(StandaloneControlPlaneCountProviderName, func(ctx context.Context) (types.ProviderReport, error) {
		controlPlanes := gwtypes.ControlPlaneList{}
		if err := cl.List(ctx, &controlPlanes); err != nil {
			return types.ProviderReport{}, err
		}
		count := lo.CountBy(controlPlanes.Items, func(cp gwtypes.ControlPlane) bool {
			return len(cp.GetOwnerReferences()) == 0
		})
		return types.ProviderReport{
			"k8s_standalone_controlplanes_count": count,
		}, nil
	})
}

func NewDataPlaneRequestedReplicasCountProvider(cl client.Client) (provider.Provider, error) {
	return provider.NewFunctorProvider(RequestedDataPlaneReplicasCountProviderName, func(ctx context.Context) (types.ProviderReport, error) {
		dataPlanes := operatorv1beta1.DataPlaneList{}
		if err := cl.List(ctx, &dataPlanes); err != nil {
			return types.ProviderReport{}, err
		}
		count := 0
		for _, dp := range dataPlanes.Items {
			if dp.Spec.Deployment.Replicas != nil {
				count += int(*dp.Spec.Deployment.Replicas)
				continue
			}
			if scaling := dp.Spec.Deployment.Scaling; scaling != nil {
				if scaling.HorizontalScaling != nil {
					// Take the upper bound of the scaling range as the requested replicas count.
					count += int(scaling.HorizontalScaling.MaxReplicas)
					continue
				}
			}
			// No replicas nor scaling defined, count it as 1 replica.
			count++
		}
		return types.ProviderReport{
			"k8s_dataplanes_requested_replicas_count": count,
		}, nil
	})
}
