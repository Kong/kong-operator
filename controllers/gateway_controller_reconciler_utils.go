package controllers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

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
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

var (
	IPAddressType       = gatewayv1beta1.IPAddressType
	HostnameAddressType = gatewayv1beta1.HostnameAddressType
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) createDataPlane(ctx context.Context,
	gateway *gwtypes.Gateway,
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
	gateway *gwtypes.Gateway,
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

func (r *GatewayReconciler) getGatewayAddresses(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) ([]gwtypes.GatewayAddress, error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return []gwtypes.GatewayAddress{}, err
	}

	count := len(services)
	// if too many dataplane services are found here, this is a temporary situation.
	// the number of services will be reduced to 1 by the dataplane controller.
	if count > 1 {
		return []gwtypes.GatewayAddress{}, fmt.Errorf("DataPlane %s/%s has multiple Services", dataplane.Namespace, dataplane.Name)
	}

	if count == 0 {
		return []gwtypes.GatewayAddress{}, fmt.Errorf("no Services found for DataPlane %s/%s", dataplane.Namespace, dataplane.Name)
	}
	return gatewayAddressesFromService(services[0])
}

func gatewayAddressesFromService(svc corev1.Service) ([]gwtypes.GatewayAddress, error) {
	addresses := make([]gwtypes.GatewayAddress, 0, len(svc.Status.LoadBalancer.Ingress))

	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		for _, serviceAddr := range svc.Status.LoadBalancer.Ingress {
			if serviceAddr.IP != "" {
				addresses = append(addresses, gwtypes.GatewayAddress{
					Value: serviceAddr.IP,
					Type:  &IPAddressType,
				})
			}
			if serviceAddr.Hostname != "" {
				addresses = append(addresses, gwtypes.GatewayAddress{
					Value: serviceAddr.Hostname,
					Type:  &HostnameAddressType,
				})
			}
		}
	default:
		// if the Service is not a LoadBalancer, it will never have any public addresses and its status address list
		// will always be empty, so we use its internal IP instead
		if svc.Spec.ClusterIP == "" {
			return addresses, fmt.Errorf("service %s doesn't have a ClusterIP yet, not ready", svc.Name)
		}
		addresses = append(addresses, gwtypes.GatewayAddress{
			Value: svc.Spec.ClusterIP,
			Type:  &IPAddressType,
		})
	}

	return addresses, nil
}

func (r *GatewayReconciler) verifyGatewayClassSupport(ctx context.Context, gateway *gwtypes.Gateway) (*gatewayClassDecorator, error) {
	if gateway.Spec.GatewayClassName == "" {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	gwc := newGatewayClass()
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, gwc.GatewayClass); err != nil {
		return nil, err
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName() {
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
			},
		}
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
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplane *operatorv1alpha1.DataPlane,
	controlplane *operatorv1alpha1.ControlPlane,
) (createdOrUpdate bool, err error) {
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	count := len(networkPolicies)
	if count > 1 {
		if err := k8sreduce.ReduceNetworkPolicies(ctx, r.Client, networkPolicies); err != nil {
			return false, err
		}
		return false, errors.New("number of networkPolicies reduced")
	}

	generatedPolicy, err := generateDataPlaneNetworkPolicy(gateway.Namespace, gatewayConfig, dataplane, controlplane)
	if err != nil {
		return false, fmt.Errorf("failed generating network policy for DataPlane %s: %w", dataplane.Name, err)
	}
	k8sutils.SetOwnerForObject(generatedPolicy, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(generatedPolicy)

	if count == 1 {
		var updated bool
		existingPolicy := &networkPolicies[0]
		updated, existingPolicy.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingPolicy.ObjectMeta, generatedPolicy.ObjectMeta)
		if updated {
			return true, r.Client.Update(ctx, existingPolicy)
		}
		if needsUpdate, updatedPolicy := k8sresources.NetworkPolicyNeedsUpdate(existingPolicy, generatedPolicy); needsUpdate {
			return true, r.Client.Update(ctx, updatedPolicy)
		}
		return false, nil
	}

	return true, r.Client.Create(ctx, generatedPolicy)
}

func generateDataPlaneNetworkPolicy(
	namespace string,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
	dataplane *operatorv1alpha1.DataPlane,
	controlplane *operatorv1alpha1.ControlPlane,
) (*networkingv1.NetworkPolicy, error) {
	var (
		protocolTCP     = corev1.ProtocolTCP
		adminAPISSLPort = intstr.FromInt(consts.DataPlaneAdminAPIPort)
		proxyPort       = intstr.FromInt(consts.DataPlaneProxyPort)
		proxySSLPort    = intstr.FromInt(consts.DataPlaneProxySSLPort)
		metricsPort     = intstr.FromInt(consts.DataPlaneMetricsPort)
	)

	// Check if KONG_PROXY_LISTEN and/or KONG_ADMIN_LISTEN are set in
	// DataPlaneDeploymentOptions and in that's the case then update NetworkPolicy
	// ports accordingly to allow communication on those ports.
	//
	// Note: for now only direct env variable manipulation is allowed (through
	// the .Env field in DataPlaneDeploymentOptions). EnvFrom is not taken into
	// account when updating NetworkPolicy ports.
	dpOpts := gatewayConfig.Spec.DataPlaneDeploymentOptions
	if proxyListen := envValueByName(dpOpts.Env, "KONG_PROXY_LISTEN"); proxyListen != "" {
		kongListenConfig, err := parseKongListenEnv(proxyListen)
		if err != nil {
			return nil, fmt.Errorf("failed parsing KONG_PROXY_LISTEN env: %w", err)
		}
		if kongListenConfig.Endpoint != nil {
			proxyPort = intstr.FromInt(kongListenConfig.Endpoint.Port)
		}
		if kongListenConfig.SSLEndpoint != nil {
			proxySSLPort = intstr.FromInt(kongListenConfig.SSLEndpoint.Port)
		}
	}
	if adminListen := envValueByName(dpOpts.Env, "KONG_ADMIN_LISTEN"); adminListen != "" {
		kongListenConfig, err := parseKongListenEnv(adminListen)
		if err != nil {
			return nil, fmt.Errorf("failed parsing KONG_ADMIN_LISTEN env: %w", err)
		}
		if kongListenConfig.SSLEndpoint != nil {
			adminAPISSLPort = intstr.FromInt(kongListenConfig.SSLEndpoint.Port)
		}
	}

	limitAdminAPIIngress := networkingv1.NetworkPolicyIngressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &protocolTCP, Port: &adminAPISSLPort},
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
					"kubernetes.io/metadata.name": controlplane.Namespace,
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
	}, nil
}

// ensureOwnedControlPlanesDeleted deletes all controlplanes owned by gateway.
// returns true if at least one controlplane resource is deleted.
func (r *GatewayReconciler) ensureOwnedControlPlanesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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
func (r *GatewayReconciler) ensureOwnedDataPlanesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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
func (r *GatewayReconciler) ensureOwnedNetworkPoliciesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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
// GatewayReconciler - Private type utilities/wrappers
// -----------------------------------------------------------------------------

type gatewayConditionsAwareT struct {
	*gatewayv1beta1.Gateway
}

func gatewayConditionsAware(gw *gwtypes.Gateway) gatewayConditionsAwareT {
	return gatewayConditionsAwareT{
		Gateway: gw,
	}
}

func (g gatewayConditionsAwareT) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

func (g gatewayConditionsAwareT) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

type proxyListenEndpoint struct {
	Address string
	Port    int
}

type KongListenConfig struct {
	Endpoint    *proxyListenEndpoint
	SSLEndpoint *proxyListenEndpoint
}

// parseKongListenEnv parses the provided kong listen string and returns
// a KongProxyListen which can have the endpoint data filled in, if parsing is
// successful.
//
// One can find more information about the kong listen format at:
// - https://docs.konghq.com/gateway/3.0.x/reference/configuration/#admin_listen
// - https://docs.konghq.com/gateway/3.0.x/reference/configuration/#proxy_listen
func parseKongListenEnv(str string) (KongListenConfig, error) {
	kongListenConfig := KongListenConfig{}

	for _, s := range strings.Split(str, ",") {
		s = strings.TrimPrefix(s, " ")
		i := strings.IndexRune(s, ' ')
		var hostPort string
		if i >= 0 {
			hostPort = s[:i]
		} else {
			hostPort = s
		}

		host, port, err := net.SplitHostPort(hostPort)
		if err != nil {
			return kongListenConfig, fmt.Errorf("failed parsing host %s: %w", hostPort, err)
		}
		flags := s[i+1:]
		if strings.Contains(flags, "ssl") {
			p, err := strconv.Atoi(port)
			if err != nil {
				return kongListenConfig, fmt.Errorf("failed parsing port %s: %w", port, err)
			}
			kongListenConfig.SSLEndpoint = &proxyListenEndpoint{
				Address: host,
				Port:    p,
			}
		} else {
			p, err := strconv.Atoi(port)
			if err != nil {
				return kongListenConfig, fmt.Errorf("failed parsing port %s: %w", port, err)
			}
			kongListenConfig.Endpoint = &proxyListenEndpoint{
				Address: host,
				Port:    p,
			}
		}
	}

	return kongListenConfig, nil
}
