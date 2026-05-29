package konnect

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

func getAPIAuthConfigurationRefNN(
	ctx context.Context,
	cl client.Client,
	from client.Object,
	name string,
	namespace *string,
) (types.NamespacedName, error) {
	apiAuthNamespace := from.GetNamespace()
	if namespace != nil && *namespace != "" {
		apiAuthNamespace = *namespace
	}
	if apiAuthNamespace != from.GetNamespace() {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx,
			cl,
			from.GetNamespace(),
			apiAuthNamespace,
			name,
			metav1.GroupVersionKind(from.GetObjectKind().GroupVersionKind()),
			metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind("KonnectAPIAuthConfiguration")),
		); err != nil {
			return types.NamespacedName{}, err
		}
	}

	return types.NamespacedName{
		Name:      name,
		Namespace: apiAuthNamespace,
	}, nil
}

func getCPAuthRefForRef(
	ctx context.Context,
	cl client.Client,
	cpRef commonv1alpha1.ControlPlaneRef,
	namespace string,
) (types.NamespacedName, error) {
	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, namespace)
	if err != nil {
		return types.NamespacedName{}, err
	}

	ref := cp.GetKonnectAPIAuthConfigurationRef()
	return getAPIAuthConfigurationRefNN(ctx, cl, cp, ref.Name, ref.Namespace)
}

// GetAPIAuthRefNN returns the NamespacedName of the KonnectAPIAuthConfiguration referenced by the entity.
func GetAPIAuthRefNN[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	// If the entity has a KonnectAPIAuthConfigurationRef, return it.
	if ref, ok := any(ent).(constraints.EntityWithKonnectAPIAuthConfigurationRef); ok {
		authRef := ref.GetKonnectAPIAuthConfigurationRef()
		return getAPIAuthConfigurationRefNN(ctx, cl, ent, authRef.Name, authRef.Namespace)
	}

	// If the entity has a ControlPlaneRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced ControlPlane.
	cpRef, ok := controlplane.GetControlPlaneRef(ent).Get()
	if ok {
		cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, ent.GetNamespace())
		if err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get ControlPlane for %s: %w", client.ObjectKeyFromObject(ent), err)
		}

		return getCPAuthRefForRef(ctx, cl, cpRef, cp.Namespace)
	}

	// If the entity has a KongServiceRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongService.
	svcRef, ok := getServiceRef(ent).Get()
	if ok {
		if svcRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
			return types.NamespacedName{}, fmt.Errorf("unsupported KongService ref type %q", svcRef.Type)
		}
		// Cross-namespace grant validation for the serviceRef is performed by
		// handleKongServiceRef before this function is reached; see reconciler_generic.go.
		svcNamespace := ent.GetNamespace()
		if svcRef.NamespacedRef.Namespace != nil {
			svcNamespace = *svcRef.NamespacedRef.Namespace
		}
		nn := types.NamespacedName{
			Name:      svcRef.NamespacedRef.Name,
			Namespace: svcNamespace,
		}

		var svc configurationv1alpha1.KongService
		if err := cl.Get(ctx, nn, &svc); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongService %s", nn)
		}

		cpRef, ok := controlplane.GetControlPlaneRef(&svc).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongService %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, svc.Namespace)
	}

	// If the entity has a KongConsumerRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongConsumer.
	consumerRef, ok := getConsumerRef(ent).Get()
	if ok {
		// TODO(pmalek): handle cross namespace refs
		nn := types.NamespacedName{
			Name:      consumerRef.Name,
			Namespace: ent.GetNamespace(),
		}

		var consumer configurationv1.KongConsumer
		if err := cl.Get(ctx, nn, &consumer); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongConsumer %s", nn)
		}

		cpRef, ok := controlplane.GetControlPlaneRef(&consumer).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongConsumer %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	// If the entity has a KongUpstreamRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongUpstream.
	upstreamRef, ok := getKongUpstreamRef(ent).Get()
	if ok {
		upstreamNamespace := ent.GetNamespace()
		if upstreamRef.Namespace != nil && *upstreamRef.Namespace != "" {
			upstreamNamespace = *upstreamRef.Namespace
		}
		nn := types.NamespacedName{
			Name:      upstreamRef.Name,
			Namespace: upstreamNamespace,
		}

		var upstream configurationv1alpha1.KongUpstream
		if err := cl.Get(ctx, nn, &upstream); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongUpstream %s", nn)
		}

		cpRef, ok := controlplane.GetControlPlaneRef(&upstream).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongUpstream %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, upstreamNamespace)
	}

	// If the entity has a KongCertificateRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongUpstream.
	certificateRef, ok := getKongCertificateRef(ent).Get()
	if ok {
		certNamespace := ent.GetNamespace()
		if certificateRef.Namespace != nil && *certificateRef.Namespace != "" {
			certNamespace = *certificateRef.Namespace
		}
		nn := types.NamespacedName{
			Name:      certificateRef.Name,
			Namespace: certNamespace,
		}

		var cert configurationv1alpha1.KongCertificate
		if err := cl.Get(ctx, nn, &cert); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongCertificate %s", nn)
		}

		cpRef, ok := controlplane.GetControlPlaneRef(&cert).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongCertificate %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, certNamespace)
	}

	// If the entity has a NetworkRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KonnectCloudGatewayNetwork.
	networkRefs, _ := getKonnectNetworkRefs(ent).Get()
	for _, networkRef := range networkRefs {
		if networkRef.NamespacedRef == nil {
			continue
		}
		namespace := ent.GetNamespace()
		if networkRef.NamespacedRef.Namespace != nil {
			namespace = *networkRef.NamespacedRef.Namespace
		}
		nn := types.NamespacedName{
			Name:      networkRef.NamespacedRef.Name,
			Namespace: namespace,
		}

		var network konnectv1alpha1.KonnectCloudGatewayNetwork

		err := cl.Get(ctx, nn, &network)
		if err != nil {
			continue
		}

		authRef := network.Spec.KonnectConfiguration.APIAuthConfigurationRef
		return types.NamespacedName{
			Name: authRef.Name,
			// TODO: enable if cross namespace refs are allowed
			Namespace: network.GetNamespace(),
		}, nil
	}

	nn, err := getAPIAuthRef(ctx, cl, ent)
	if err != nil {
		return types.NamespacedName{}, fmt.Errorf(
			"cannot get KonnectAPIAuthConfiguration for entity %T %s: %w",
			ent, client.ObjectKeyFromObject(ent), err,
		)
	}

	return nn, nil
}
