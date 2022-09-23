package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) createDataPlane(ctx context.Context,
	gateway *gatewayDecorator,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
) error {
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: fmt.Sprintf("%s-", gateway.Name),
		},
	}
	if gatewayConfig.Spec.DataPlaneDeploymentOptions != nil {
		dataplane.Spec.DataPlaneDeploymentOptions = *gatewayConfig.Spec.DataPlaneDeploymentOptions
	}
	k8sutils.SetOwnerForObject(dataplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(dataplane)
	return r.Client.Create(ctx, dataplane)
}

func (r *GatewayReconciler) createControlPlane(
	ctx context.Context,
	gatewayClass *gatewayv1beta1.GatewayClass,
	gateway *gatewayDecorator,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplaneName string,
) error {
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: fmt.Sprintf("%s-", gateway.Name),
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			GatewayClass: (*gatewayv1beta1.ObjectName)(&gatewayClass.Name),
		},
	}
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions != nil {
		controlplane.Spec.ControlPlaneDeploymentOptions = *gatewayConfig.Spec.ControlPlaneDeploymentOptions
	}
	if controlplane.Spec.DataPlane == nil {
		controlplane.Spec.DataPlane = &dataplaneName
	}
	k8sutils.SetOwnerForObject(controlplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(controlplane)
	return r.Client.Create(ctx, controlplane)
}

func (r *GatewayReconciler) ensureGatewayConnectivityStatus(ctx context.Context, gateway *gatewayDecorator, dataplane *operatorv1alpha1.DataPlane) (err error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return err
	}

	count := len(services)
	if count > 1 {
		return fmt.Errorf("found %d services for DataPlane currently unsupported: expected 1 or less", count)
	}

	if count == 0 {
		return fmt.Errorf("no services found for dataplane %s/%s", dataplane.Namespace, dataplane.Name)
	}
	svc := services[0]
	if svc.Spec.ClusterIP == "" {
		gateway.Status.Addresses = []gatewayv1beta1.GatewayAddress{}
		return fmt.Errorf("service %s doesn't have a ClusterIP yet, not ready", svc.Name)
	}

	// start collecting network addresses where the gateway is reachable
	// at for ingress traffic.
	gatewayAddrs := make(gwaddrs, 0)
	if len(svc.Status.LoadBalancer.Ingress) > 0 {
		switch {
		case svc.Status.LoadBalancer.Ingress[0].IP != "":
			gatewayAddrs = append(gatewayAddrs, gwaddr{
				addr:     svc.Status.LoadBalancer.Ingress[0].IP,
				addrType: ipaddrT,
				isLB:     true,
			})
		case svc.Status.LoadBalancer.Ingress[0].Hostname != "":
			gatewayAddrs = append(gatewayAddrs, gwaddr{
				addr:     svc.Status.LoadBalancer.Ingress[0].Hostname,
				addrType: hostAddrT,
				isLB:     true,
			})
		default:
			return fmt.Errorf("missing loadbalancer address in service %s/%s", svc.Namespace, svc.Name)
		}
	}

	// combine all addresses, including the ClusterIP and sort them
	// according to priority (LoadBalancer addresses have the highest
	// priority).
	newAddresses := make([]gatewayv1beta1.GatewayAddress, 0, len(gatewayAddrs))
	allAddrs := append(gatewayAddrs, gwaddr{
		addr:     svc.Spec.ClusterIP,
		addrType: ipaddrT,
	})
	sort.Sort(allAddrs)

	for _, addr := range allAddrs {
		newAddresses = append(newAddresses, gatewayv1beta1.GatewayAddress{
			Type:  &(addr.addrType),
			Value: addr.addr,
		})
	}

	gateway.Status.Addresses = newAddresses

	return nil
}

func (r *GatewayReconciler) verifyGatewayClassSupport(ctx context.Context, gateway *gatewayv1beta1.Gateway) (*gatewayClassDecorator, error) {
	if gateway.Spec.GatewayClassName == "" {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	gwc := newGatewayClass()
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, gwc.GatewayClass); err != nil {
		return nil, err
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	return gwc, nil
}

func (r *GatewayReconciler) getOrCreateGatewayConfiguration(ctx context.Context, gatewayClass *gatewayv1beta1.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
	gatewayConfig, err := r.getGatewayConfigForGatewayClass(ctx, gatewayClass)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrObjectMissingParametersRef) {
			return new(operatorv1alpha1.GatewayConfiguration), nil
		}
		return nil, err
	}

	return gatewayConfig, nil
}

func (r *GatewayReconciler) getGatewayConfigForGatewayClass(ctx context.Context, gatewayClass *gatewayv1beta1.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
	if gatewayClass.Spec.ParametersRef == nil {
		return nil, fmt.Errorf("%w, gatewayClass = %s", operatorerrors.ErrObjectMissingParametersRef, gatewayClass.Name)
	}

	if string(gatewayClass.Spec.ParametersRef.Group) != operatorv1alpha1.SchemeGroupVersion.Group ||
		string(gatewayClass.Spec.ParametersRef.Kind) != "GatewayConfiguration" {
		return nil, &k8serrors.StatusError{
			ErrStatus: metav1.Status{
				Status: metav1.StatusFailure,
				Code:   http.StatusBadRequest,
				Reason: metav1.StatusReasonInvalid,
				Details: &metav1.StatusDetails{
					Kind: string(gatewayClass.Spec.ParametersRef.Kind),
					Causes: []metav1.StatusCause{{
						Type: metav1.CauseTypeFieldValueNotSupported,
						Message: fmt.Sprintf("controller only supports %s %s resources for GatewayClass parametersRef",
							operatorv1alpha1.SchemeGroupVersion.Group, "GatewayConfiguration"),
					}},
				},
			}}
	}

	if gatewayClass.Spec.ParametersRef.Namespace == nil ||
		*gatewayClass.Spec.ParametersRef.Namespace == "" ||
		gatewayClass.Spec.ParametersRef.Name == "" {
		return nil, fmt.Errorf("GatewayClass %s has invalid ParametersRef: both namespace and name must be provided", gatewayClass.Name)
	}

	gatewayConfig := new(operatorv1alpha1.GatewayConfiguration)
	return gatewayConfig, r.Client.Get(ctx, client.ObjectKey{
		Namespace: string(*gatewayClass.Spec.ParametersRef.Namespace),
		Name:      gatewayClass.Spec.ParametersRef.Name,
	}, gatewayConfig)
}

func (r *GatewayReconciler) ensureDataPlaneHasNetworkPolicy(
	ctx context.Context,
	gateway *gatewayDecorator,
	dataplane *operatorv1alpha1.DataPlane,
	controlplane *operatorv1alpha1.ControlPlane,
) (createdOrUpdate bool, err error) {
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway.Gateway)
	if err != nil {
		return false, err
	}

	count := len(networkPolicies)
	if count > 1 {
		return false, fmt.Errorf("%w, got: %d, expected 1", operatorerrors.ErrTooManyDataPlaneNetworkPolicies, count)
	}

	generatedPolicy := generateDataPlaneNetworkPolicy(gateway.Namespace, dataplane, controlplane)
	k8sutils.SetOwnerForObject(generatedPolicy, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(generatedPolicy)

	if count == 1 {
		var updated bool
		existingPolicy := &networkPolicies[0]
		updated, existingPolicy.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingPolicy.ObjectMeta, generatedPolicy.ObjectMeta)
		if updated {
			return true, r.Client.Update(ctx, existingPolicy)
		}
		return false, nil
	}

	return true, r.Client.Create(ctx, generatedPolicy)
}

func generateDataPlaneNetworkPolicy(
	namespace string,
	dataplane *operatorv1alpha1.DataPlane,
	controlplane *operatorv1alpha1.ControlPlane,
) *networkingv1.NetworkPolicy {
	var (
		protocolTCP  = corev1.ProtocolTCP
		adminAPIPort = intstr.FromInt(consts.DataPlaneAdminAPIPort)
		proxyPort    = intstr.FromInt(consts.DataPlaneProxyPort)
		proxySSLPort = intstr.FromInt(consts.DataPlaneProxySSLPort)
		metricsPort  = intstr.FromInt(consts.DataPlaneMetricsPort)
	)

	limitAdminAPIIngress := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protocolTCP, Port: &adminAPIPort},
		},
		From: []networkingv1.NetworkPolicyPeer{{
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": controlplane.Name,
				},
			},
			// NamespaceDefaultLabelName feature gate must be enabled for this to work
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": dataplane.Namespace,
				},
			},
		}},
	}

	allowProxyIngress := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protocolTCP, Port: &proxyPort},
			{Protocol: &protocolTCP, Port: &proxySSLPort},
		},
	}

	allowMetricsIngress := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protocolTCP, Port: &metricsPort},
		},
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: fmt.Sprintf("%s-limit-admin-api-", dataplane.Name),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": dataplane.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				limitAdminAPIIngress,
				allowProxyIngress,
				allowMetricsIngress,
			},
		},
	}
}

// ensureOwnedControlPlanesDeleted deletes all controlplanes owned by gateway.
// returns true if at least one controlplane resource is deleted.
func (r *GatewayReconciler) ensureOwnedControlPlanesDeleted(ctx context.Context, gateway *gatewayv1beta1.Gateway) (bool, error) {
	controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	deleted := false
	var deletionErr *multierror.Error
	for i := range controlplanes {
		// skip already deleted controlplanes, because controlplanes may have finalizers
		// to wait for owned cluster wide resources deleted.
		if !controlplanes[i].DeletionTimestamp.IsZero() {
			continue
		}
		err = r.Client.Delete(ctx, &controlplanes[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			deletionErr = multierror.Append(deletionErr, err)
		}
		deleted = true
	}

	return deleted, deletionErr.ErrorOrNil()
}

// ensureOwnedDataPlanesDeleted deleted all dataplanes owned by gateway.
// returns true if at least one dataplane resource is deleted.
func (r *GatewayReconciler) ensureOwnedDataPlanesDeleted(ctx context.Context, gateway *gatewayv1beta1.Gateway) (bool, error) {
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	deleted := false
	var deletionErr *multierror.Error
	for i := range dataplanes {
		err = r.Client.Delete(ctx, &dataplanes[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			deletionErr = multierror.Append(deletionErr, err)
		}
		deleted = true
	}

	return deleted, deletionErr.ErrorOrNil()
}

// ensureOwnedNetworkPoliciesDeleted deleted all network policies owned by gateway.
// returns true if at least one networkPolicy resource is deleted.
func (r *GatewayReconciler) ensureOwnedNetworkPoliciesDeleted(ctx context.Context, gateway *gatewayv1beta1.Gateway) (bool, error) {
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	deleted := false
	var deletionErr *multierror.Error
	for i := range networkPolicies {
		err = r.Client.Delete(ctx, &networkPolicies[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			deletionErr = multierror.Append(deletionErr, err)
		}
		deleted = true
	}

	return deleted, deletionErr.ErrorOrNil()
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Private Network Address Utilities
// -----------------------------------------------------------------------------

var (
	ipaddrT   = gatewayv1beta1.IPAddressType
	hostAddrT = gatewayv1beta1.HostnameAddressType
)

type gwaddr struct {
	addr     string
	addrType gatewayv1beta1.AddressType
	isLB     bool
}

type gwaddrs []gwaddr

func (g gwaddrs) Len() int           { return len(g) }
func (g gwaddrs) Swap(i, j int)      { g[i], g[j] = g[j], g[i] }
func (g gwaddrs) Less(i, j int) bool { return g[i].isLB && !g[j].isLB }
