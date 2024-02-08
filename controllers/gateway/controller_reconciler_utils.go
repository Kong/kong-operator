package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/gatewayclass"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *Reconciler) createDataPlane(ctx context.Context,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
) error {
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: fmt.Sprintf("%s-", gateway.Name),
		},
	}
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		dataplane.Spec.DataPlaneOptions = *gatewayConfigDataPlaneOptionsToDataPlaneOptions(*gatewayConfig.Spec.DataPlaneOptions)
	}
	setDataPlaneOptionsDefaults(&dataplane.Spec.DataPlaneOptions)
	if err := setDataPlaneIngressServicePorts(&dataplane.Spec.DataPlaneOptions, gateway.Spec.Listeners); err != nil {
		return err
	}
	k8sutils.SetOwnerForObject(dataplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(dataplane)
	return r.Client.Create(ctx, dataplane)
}

func (r *Reconciler) createControlPlane(
	ctx context.Context,
	gatewayClass *gatewayv1.GatewayClass,
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
			GatewayClass: (*gatewayv1.ObjectName)(&gatewayClass.Name),
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

func (r *Reconciler) getGatewayAddresses(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) ([]gwtypes.GatewayStatusAddress, error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		},
	)
	if err != nil {
		return []gwtypes.GatewayStatusAddress{}, err
	}

	count := len(services)
	// if too many dataplane services are found here, this is a temporary situation.
	// the number of services will be reduced to 1 by the dataplane controller.
	if count > 1 {
		return []gwtypes.GatewayStatusAddress{}, fmt.Errorf("DataPlane %s/%s has multiple Services", dataplane.Namespace, dataplane.Name)
	}

	if count == 0 {
		return []gwtypes.GatewayStatusAddress{}, fmt.Errorf("no Services found for DataPlane %s/%s", dataplane.Namespace, dataplane.Name)
	}
	return gatewayAddressesFromService(services[0])
}

func gatewayConfigDataPlaneOptionsToDataPlaneOptions(opts operatorv1alpha1.GatewayConfigDataPlaneOptions) *operatorv1beta1.DataPlaneOptions {
	dataPlaneOptions := &operatorv1beta1.DataPlaneOptions{
		Deployment: opts.Deployment,
	}
	if opts.Network.Services != nil && opts.Network.Services.Ingress != nil {
		dataPlaneOptions.Network = operatorv1beta1.DataPlaneNetworkOptions{
			Services: &operatorv1beta1.DataPlaneServices{
				Ingress: &operatorv1beta1.DataPlaneServiceOptions{
					ServiceOptions: operatorv1beta1.ServiceOptions{
						Type:        opts.Network.Services.Ingress.Type,
						Annotations: opts.Network.Services.Ingress.Annotations,
					},
				},
			},
		}
	}

	return dataPlaneOptions
}

func gatewayAddressesFromService(svc corev1.Service) ([]gwtypes.GatewayStatusAddress, error) {
	addresses := make([]gwtypes.GatewayStatusAddress, 0, len(svc.Status.LoadBalancer.Ingress))

	switch svc.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		for _, serviceAddr := range svc.Status.LoadBalancer.Ingress {
			if serviceAddr.IP != "" {
				addresses = append(addresses, gwtypes.GatewayStatusAddress{
					Value: serviceAddr.IP,
					Type:  lo.ToPtr(gatewayv1.IPAddressType),
				})
			}
			if serviceAddr.Hostname != "" {
				addresses = append(addresses, gwtypes.GatewayStatusAddress{
					Value: serviceAddr.Hostname,
					Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
				})
			}
		}
	default:
		// if the Service is not a LoadBalancer, it will never have any public addresses and its status address list
		// will always be empty, so we use its internal IP instead
		if svc.Spec.ClusterIP == "" {
			return addresses, fmt.Errorf("service %s doesn't have a ClusterIP yet, not ready", svc.Name)
		}
		addresses = append(addresses, gwtypes.GatewayStatusAddress{
			Value: svc.Spec.ClusterIP,
			Type:  lo.ToPtr(gatewayv1.IPAddressType),
		})
	}

	return addresses, nil
}

func (r *Reconciler) verifyGatewayClassSupport(ctx context.Context, gateway *gwtypes.Gateway) (*gatewayclass.Decorator, error) {
	if gateway.Spec.GatewayClassName == "" {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	gwc := gatewayclass.NewDecorator()
	if err := r.Client.Get(ctx, client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}, gwc.GatewayClass); err != nil {
		return nil, err
	}

	if string(gwc.Spec.ControllerName) != vars.ControllerName() {
		return nil, operatorerrors.ErrUnsupportedGateway
	}

	return gwc, nil
}

func (r *Reconciler) getOrCreateGatewayConfiguration(ctx context.Context, gatewayClass *gatewayv1.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
	gatewayConfig, err := r.getGatewayConfigForGatewayClass(ctx, gatewayClass)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrObjectMissingParametersRef) {
			return new(operatorv1alpha1.GatewayConfiguration), nil
		}
		return nil, err
	}

	return gatewayConfig, nil
}

func (r *Reconciler) getGatewayConfigForGatewayClass(ctx context.Context, gatewayClass *gatewayv1.GatewayClass) (*operatorv1alpha1.GatewayConfiguration, error) {
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

func (r *Reconciler) ensureDataPlaneHasNetworkPolicy(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	dataplane *operatorv1beta1.DataPlane,
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

	generatedPolicy, err := generateDataPlaneNetworkPolicy(gateway.Namespace, dataplane, controlplane)
	if err != nil {
		return false, fmt.Errorf("failed generating network policy for DataPlane %s: %w", dataplane.Name, err)
	}
	k8sutils.SetOwnerForObject(generatedPolicy, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(generatedPolicy)

	if count == 1 {
		var (
			metaUpdated    bool
			existingPolicy = &networkPolicies[0]
			old            = existingPolicy.DeepCopy()
		)
		metaUpdated, existingPolicy.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingPolicy.ObjectMeta, generatedPolicy.ObjectMeta)

		if k8sresources.EnsureNetworkPolicyIsUpdated(existingPolicy, generatedPolicy) || metaUpdated {
			if err := r.Client.Patch(ctx, existingPolicy, client.MergeFrom(old)); err != nil {
				return false, fmt.Errorf("failed updating DataPlane's NetworkPolicy %s: %w", existingPolicy.Name, err)
			}
			return true, nil
		}
		return false, nil
	}

	return true, r.Client.Create(ctx, generatedPolicy)
}

func generateDataPlaneNetworkPolicy(
	namespace string,
	dataplane *operatorv1beta1.DataPlane,
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
	dpOpts := dataplane.Spec.DataPlaneOptions
	container := k8sutils.GetPodContainerByName(&dpOpts.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if proxyListen := k8sutils.EnvValueByName(container.Env, "KONG_PROXY_LISTEN"); proxyListen != "" {
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
	if adminListen := k8sutils.EnvValueByName(container.Env, "KONG_ADMIN_LISTEN"); adminListen != "" {
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
func (r *Reconciler) ensureOwnedControlPlanesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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
func (r *Reconciler) ensureOwnedDataPlanesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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
func (r *Reconciler) ensureOwnedNetworkPoliciesDeleted(ctx context.Context, gateway *gwtypes.Gateway) (bool, error) {
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

type gatewayConditionsAndListenersAwareT struct {
	*gatewayv1.Gateway
}

func gatewayConditionsAndListenersAware(gw *gwtypes.Gateway) gatewayConditionsAndListenersAwareT {
	return gatewayConditionsAndListenersAwareT{
		Gateway: gw,
	}
}

// GetConditions returns the status conditions.
func (g gatewayConditionsAndListenersAwareT) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

// SetConditions sets the status conditions.
func (g gatewayConditionsAndListenersAwareT) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

// GetListenersConditions returns the listeners status.
func (g gatewayConditionsAndListenersAwareT) GetListenersConditions() []gatewayv1.ListenerStatus {
	return g.Status.Listeners
}

// SetListenersConditions sets the listeners status.
func (g gatewayConditionsAndListenersAwareT) SetListenersConditions(listeners []gatewayv1.ListenerStatus) {
	g.Status.Listeners = listeners
}

type listenerConditionAwareT struct {
	*gatewayv1.ListenerStatus
}

func listenerConditionsAware(listener *gatewayv1.ListenerStatus) listenerConditionAwareT {
	return listenerConditionAwareT{
		ListenerStatus: listener,
	}
}

// GetConditions returns the status conditions.
func (l listenerConditionAwareT) GetConditions() []metav1.Condition {
	return l.Conditions
}

// SetConditions sets the status conditions.
func (l listenerConditionAwareT) SetConditions(conditions []metav1.Condition) {
	l.Conditions = conditions
}

// supportedRoutesByProtocol returns a map of maps to relate each protocolType with the
// set of supported Routes.
//
// Note: the inner maps have only one element as for now, but in future they will be improved,
// as each protocolType can be compatible with many different route types.
func supportedRoutesByProtocol() map[gatewayv1.ProtocolType]map[gatewayv1.Kind]struct{} {
	return map[gatewayv1.ProtocolType]map[gatewayv1.Kind]struct{}{
		gatewayv1.HTTPProtocolType:  {"HTTPRoute": {}},
		gatewayv1.HTTPSProtocolType: {"HTTPRoute": {}},

		// L4 routes not supported yet
		// gatewayv1.TLSProtocolType:   {"TLSRoute": {}},
		// gatewayv1.TCPProtocolType:   {"TCPRoute": {}},
		// gatewayv1.UDPProtocolType:   {"UDPRoute": {}},
	}
}

// initReadyAndProgrammed initializes the gateway Programmed and Ready conditions
// by setting the underlying Gateway Programmed and Ready status to false.
// Furthermore, it sets the supportedKinds and initializes the readiness to false with reason
// Pending for each Gateway listener.
func (g *gatewayConditionsAndListenersAwareT) initReadyAndProgrammed() {
	k8sutils.InitReady(g)
	k8sutils.InitProgrammed(g)
	for i, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition := getSupportedKindsWithCondition(g.Generation, listener)
		lStatus := g.Status.Listeners[i]
		lStatus.SupportedKinds = supportedKinds
		lStatus.Conditions = append(lStatus.Conditions,
			metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonPending),
				ObservedGeneration: g.Generation,
				LastTransitionTime: metav1.Now(),
			},
			resolvedRefsCondition,
		)
		g.Status.Listeners[i] = lStatus
	}
}

// initListenersStatus initialize the listener status for a given gateway.
func (g *gatewayConditionsAndListenersAwareT) initListenersStatus() {
	// Initialize listeners status.
	listenersStatus := make([]gatewayv1.ListenerStatus, len(g.Spec.Listeners))
	for i, listener := range g.Spec.Listeners {
		listenersStatus[i] = gatewayv1.ListenerStatus{
			Name:       listener.Name,
			Conditions: []metav1.Condition{},
		}
	}
	g.Status.Listeners = listenersStatus
}

// setAccepted sets the listeners and gateway Accepted condition according to the Gateway API specification.
func (g *gatewayConditionsAndListenersAwareT) setAccepted() {
	for i, listener := range g.Spec.Listeners {
		acceptedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonAccepted),
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: g.Generation,
		}
		if listener.Protocol != gatewayv1.HTTPProtocolType && listener.Protocol != gatewayv1.HTTPSProtocolType {
			acceptedCondition.Status = metav1.ConditionFalse
			acceptedCondition.Reason = string(gatewayv1.ListenerReasonUnsupportedProtocol)
		}
		listenerConditionsAware := listenerConditionsAware(&g.Status.Listeners[i])
		listenerConditionsAware.SetConditions(append(listenerConditionsAware.Conditions, acceptedCondition))
	}
	k8sutils.SetAcceptedConditionOnGateway(g)
}

// setConflicted sets the gateway Conflicted condition according to the Gateway API specification.
func (g *gatewayConditionsAndListenersAwareT) setConflicted() {
	for i, l := range g.Spec.Listeners {
		conflictedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionConflicted),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.ListenerReasonNoConflicts),
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: g.Generation,
		}
		for j, l2 := range g.Spec.Listeners {
			if i == j {
				continue
			}
			// If two listeners specify the same port and different protocols, they have a protocol conflict,
			// and the conflicted condition must be updated accordingly.
			if l.Port == l2.Port && l.Protocol != l2.Protocol {
				conflictedCondition.Status = metav1.ConditionTrue
				conflictedCondition.Reason = string(gatewayv1.ListenerReasonProtocolConflict)
				break
			}
			// If two listeners specify the same hostname, they have a hostname conflict, and
			// the conflicted condition must be updated accordingly.
			if l.Hostname != nil && l2.Hostname != nil && *l.Hostname == *l2.Hostname {
				conflictedCondition.Status = metav1.ConditionTrue
				conflictedCondition.Reason = string(gatewayv1.ListenerReasonHostnameConflict)
				break
			}
		}
		listenerConditionsAware := listenerConditionsAware(&g.Status.Listeners[i])
		k8sutils.SetCondition(conflictedCondition, listenerConditionsAware)
	}
}

// setReadyAndProgrammed sets the gateway Programmed and Ready conditions by
// setting the underlying Gateway Programmed and Ready status to true.
// Furthermore, it sets the supportedKinds and initializes the readiness to true with reason
// Ready or false with reason Invalid for each Gateway listener.
func (g *gatewayConditionsAndListenersAwareT) setReadyAndProgrammed() {
	k8sutils.SetReady(g)
	k8sutils.SetProgrammed(g)

	for i, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition := getSupportedKindsWithCondition(g.Generation, listener)
		programmedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonProgrammed),
			ObservedGeneration: g.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		}
		if resolvedRefsCondition.Status == metav1.ConditionFalse {
			programmedCondition.Status = metav1.ConditionFalse
			programmedCondition.Reason = string(gatewayv1.ListenerReasonInvalid)
		}
		listenerStatus := listenerConditionsAware(&g.Status.Listeners[i])
		listenerStatus.SupportedKinds = supportedKinds
		k8sutils.SetCondition(programmedCondition, listenerStatus)
		k8sutils.SetCondition(resolvedRefsCondition, listenerStatus)
	}
}

func setDataPlaneIngressServicePorts(opts *operatorv1beta1.DataPlaneOptions, listeners []gatewayv1.Listener) error {
	if len(listeners) == 0 {
		return nil
	}

	if opts.Network.Services == nil {
		opts.Network.Services = &operatorv1beta1.DataPlaneServices{}
	}
	if opts.Network.Services.Ingress == nil {
		opts.Network.Services.Ingress = &operatorv1beta1.DataPlaneServiceOptions{
			Ports: []operatorv1beta1.DataPlaneServicePort{},
		}
	}

	var errs error
	for i, l := range listeners {
		var name string
		// If the listener name is set, use it. Otherwise, we need to be sure the
		// port name is unique at the service level, hence the trimmed uuid.
		if l.Name != "" {
			name = string(l.Name)
		} else {
			name = fmt.Sprintf("%s-%s", l.Protocol, uuid.NewString()[:6])
		}
		port := operatorv1beta1.DataPlaneServicePort{
			Name: name,
			Port: int32(l.Port),
		}
		switch l.Protocol {
		case gatewayv1.HTTPSProtocolType:
			port.TargetPort = intstr.FromInt(consts.DataPlaneProxySSLPort)
		case gatewayv1.HTTPProtocolType:
			port.TargetPort = intstr.FromInt(consts.DataPlaneProxyPort)
		default:
			errs = errors.Join(errs, fmt.Errorf("listener %d uses unsupported protocol %s", i, l.Protocol))
			continue
		}
		opts.Network.Services.Ingress.Ports = append(opts.Network.Services.Ingress.Ports, port)
	}
	return errs
}

// getSupportedKindsWithCondition returns all the route kinds supported by the listener, along with the resolvedRefs
// condition, that is based on the presence of errors in such a field.
func getSupportedKindsWithCondition(generation int64, listener gatewayv1.Listener) (supportedKinds []gatewayv1.RouteGroupKind, resolvedRefsCondition metav1.Condition) {
	supportedKinds = make([]gatewayv1.RouteGroupKind, 0)
	resolvedRefsCondition = metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
	if len(listener.AllowedRoutes.Kinds) == 0 {
		supportedRoutes := supportedRoutesByProtocol()[listener.Protocol]
		for route := range supportedRoutes {
			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: (*gatewayv1.Group)(&gatewayv1.GroupVersion.Group),
				Kind:  route,
			})
		}
	}

	for _, k := range listener.AllowedRoutes.Kinds {
		validRoutes := supportedRoutesByProtocol()[listener.Protocol]
		if _, ok := validRoutes[k.Kind]; !ok || k.Group == nil || *k.Group != gatewayv1.Group(gatewayv1.GroupVersion.Group) {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidRouteKinds)
			continue
		}

		supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
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

type kongListenConfig struct {
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
func parseKongListenEnv(str string) (kongListenConfig, error) {
	kongListenConfig := kongListenConfig{}

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
