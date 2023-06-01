package controllers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

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
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		dataplane.Spec.DataPlaneOptions = *gatewayConfig.Spec.DataPlaneOptions
	}
	setDataPlaneOptionsDefaults(&dataplane.Spec.DataPlaneOptions)
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
	if gatewayConfig.Spec.ControlPlaneOptions != nil {
		controlplane.Spec.ControlPlaneOptions = *gatewayConfig.Spec.ControlPlaneOptions
	}
	if controlplane.Spec.DataPlane == nil {
		controlplane.Spec.DataPlane = &dataplaneName
	}

	setControlPlaneOptionsDefaults(&controlplane.Spec.ControlPlaneOptions)
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
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneProxyServiceLabelValue),
		},
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
	dpOpts := gatewayConfig.Spec.DataPlaneOptions
	if proxyListen := envValueByName(dpOpts.Deployment.Pods.Env, "KONG_PROXY_LISTEN"); proxyListen != "" {
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
	if adminListen := envValueByName(dpOpts.Deployment.Pods.Env, "KONG_ADMIN_LISTEN"); adminListen != "" {
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

	var (
		deleted bool
		errs    []error
	)
	for i := range controlplanes {
		// skip already deleted controlplanes, because controlplanes may have finalizers
		// to wait for owned cluster wide resources deleted.
		if !controlplanes[i].DeletionTimestamp.IsZero() {
			continue
		}
		err = r.Client.Delete(ctx, &controlplanes[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

// ensureOwnedDataPlanesDeleted deleted all dataplanes owned by gateway.
// returns true if at least one dataplane resource is deleted.
func (r *GatewayReconciler) ensureOwnedDataPlanesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
	dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range dataplanes {
		err = r.Client.Delete(ctx, &dataplanes[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

// ensureOwnedNetworkPoliciesDeleted deleted all network policies owned by gateway.
// returns true if at least one networkPolicy resource is deleted.
func (r *GatewayReconciler) ensureOwnedNetworkPoliciesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
	networkPolicies, err := gatewayutils.ListNetworkPoliciesForGateway(ctx, r.Client, gateway)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range networkPolicies {
		err = r.Client.Delete(ctx, &networkPolicies[i])
		if err != nil && !k8serrors.IsNotFound(err) {
			errs = append(errs, err)
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Private type status-related utilities/wrappers
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

// supportedRoutesByProtocol returns a map of maps to relate each protocolType with the
// set of supported Routes.
//
// Note: the inner maps have only one element as for now, but in future they will be improved,
// as each protocolType can be compatible with many different route types.
func supportedRoutesByProtocol() map[gatewayv1beta1.ProtocolType]map[gatewayv1beta1.Kind]struct{} {
	return map[gatewayv1beta1.ProtocolType]map[gatewayv1beta1.Kind]struct{}{
		gatewayv1beta1.HTTPProtocolType:  {"HTTPRoute": {}},
		gatewayv1beta1.HTTPSProtocolType: {"HTTPRoute": {}},
		gatewayv1beta1.TLSProtocolType:   {"TLSRoute": {}},
		gatewayv1beta1.TCPProtocolType:   {"TCPRoute": {}},
		gatewayv1beta1.UDPProtocolType:   {"UDPRoute": {}},
	}
}

// InitReady initializes the gateway readiness by setting the Gateway ready status to false.
// Furthermore, it sets the supportedKinds and initializes the readiness to false with reason
// Pending for each Gateway listener.
func (g *gatewayConditionsAwareT) InitReady() {
	k8sutils.InitReady(g)
	g.Status.Listeners = make([]gatewayv1beta1.ListenerStatus, 0, len(g.Spec.Listeners))
	for _, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition := getSupportedKindsWithCondition(g.Generation, listener)
		lStatus := gatewayv1beta1.ListenerStatus{
			Name:           listener.Name,
			SupportedKinds: supportedKinds,
			Conditions: []metav1.Condition{
				{
					Type:               string(gatewayv1beta1.ListenerConditionReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1beta1.ListenerReasonPending),
					ObservedGeneration: g.Generation,
					LastTransitionTime: metav1.Now(),
				},
				resolvedRefsCondition,
			},
		}
		g.Status.Listeners = append(g.Status.Listeners, lStatus)
	}
}

// SetReady sets the gateway readiness by setting the Gateway ready status to true.
// Furthermore, it sets the supportedKinds and initializes the readiness to true with reason
// Ready or false with reason Invalid for each Gateway listener.
func (g *gatewayConditionsAwareT) SetReady() {
	k8sutils.SetReady(g, g.Generation)
	listenersStatus := []gatewayv1beta1.ListenerStatus{}
	for _, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition := getSupportedKindsWithCondition(g.Generation, listener)
		readyCondition := metav1.Condition{
			Type:               string(gatewayv1beta1.ListenerConditionReady),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1beta1.ListenerReasonReady),
			ObservedGeneration: g.Generation,
			LastTransitionTime: metav1.Now(),
		}
		if resolvedRefsCondition.Status == metav1.ConditionFalse {
			readyCondition.Status = metav1.ConditionFalse
			readyCondition.Reason = string(gatewayv1beta1.ListenerReasonInvalid)
		}
		lStatus := gatewayv1beta1.ListenerStatus{
			Name:           listener.Name,
			SupportedKinds: supportedKinds,
			Conditions: []metav1.Condition{
				readyCondition,
				resolvedRefsCondition,
			},
		}
		listenersStatus = append(listenersStatus, lStatus)
	}
	g.Status.Listeners = listenersStatus
}

// getSupportedKindsWithCondition returns all the route kinds supported by the listener, along with the resolvedRefs
// condition, that is based on the presence of errors in such a field.
func getSupportedKindsWithCondition(generation int64, listener gatewayv1beta1.Listener) (supportedKinds []gatewayv1beta1.RouteGroupKind, resolvedRefsCondition metav1.Condition) {
	supportedKinds = make([]gatewayv1beta1.RouteGroupKind, 0)
	resolvedRefsCondition = metav1.Condition{
		Type:               string(gatewayv1beta1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1beta1.ListenerReasonResolvedRefs),
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
	if len(listener.AllowedRoutes.Kinds) == 0 {
		supportedRoutes, ok := supportedRoutesByProtocol()[listener.Protocol]
		if !ok {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(gatewayv1beta1.ListenerReasonInvalidRouteKinds)
		}
		for route := range supportedRoutes {
			supportedKinds = append(supportedKinds, gatewayv1beta1.RouteGroupKind{
				Group: (*gatewayv1beta1.Group)(&gatewayv1beta1.GroupVersion.Group),
				Kind:  route,
			})
		}
	}

	for _, k := range listener.AllowedRoutes.Kinds {
		validRoutes := supportedRoutesByProtocol()[listener.Protocol]
		if _, ok := validRoutes[k.Kind]; !ok || k.Group == nil || *k.Group != gatewayv1beta1.Group(gatewayv1beta1.GroupVersion.Group) {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(gatewayv1beta1.ListenerReasonInvalidRouteKinds)
			continue
		}

		supportedKinds = append(supportedKinds, gatewayv1beta1.RouteGroupKind{
			Group: k.Group,
			Kind:  k.Kind,
		})
	}
	return supportedKinds, resolvedRefsCondition
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
