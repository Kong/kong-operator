package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/internal/utils/gatewayclass"
	"github.com/kong/gateway-operator/pkg/consts"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *Reconciler) createDataPlane(ctx context.Context,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1alpha1.GatewayConfiguration,
) (*operatorv1beta1.DataPlane, error) {
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", gateway.Name)),
		},
	}
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		dataplane.Spec.DataPlaneOptions = *gatewayConfigDataPlaneOptionsToDataPlaneOptions(*gatewayConfig.Spec.DataPlaneOptions)
	}
	setDataPlaneOptionsDefaults(&dataplane.Spec.DataPlaneOptions)
	if err := setDataPlaneIngressServicePorts(&dataplane.Spec.DataPlaneOptions, gateway.Spec.Listeners); err != nil {
		return nil, err
	}
	k8sutils.SetOwnerForObject(dataplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(dataplane)
	err := r.Client.Create(ctx, dataplane)
	if err != nil {
		return nil, err
	}
	return dataplane, nil
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
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", gateway.Name)),
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
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-limit-admin-api-", dataplane.Name)),
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
	for i := range g.Spec.Listeners {
		lStatus := listenerConditionsAware(&g.Status.Listeners[i])
		k8sutils.SetCondition(metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionFalse,
			Reason:             string(gatewayv1.ListenerReasonPending),
			ObservedGeneration: g.Generation,
			LastTransitionTime: metav1.Now(),
		}, lStatus)
	}
}

func (g *gatewayConditionsAndListenersAwareT) setResolvedRefsAndSupportedKinds(ctx context.Context, c client.Client) error {
	for i, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition, err := getSupportedKindsWithResolvedRefsCondition(ctx, c, g.Namespace, g.Generation, listener)
		if err != nil {
			return err
		}
		lStatus := listenerConditionsAware(&g.Status.Listeners[i])
		lStatus.SupportedKinds = supportedKinds
		k8sutils.SetCondition(resolvedRefsCondition, lStatus)
	}
	return nil
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

	for i := range g.Spec.Listeners {
		programmedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonProgrammed),
			ObservedGeneration: g.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		}
		listenerStatus := listenerConditionsAware(&g.Status.Listeners[i])
		k8sutils.SetCondition(programmedCondition, listenerStatus)
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

// getSupportedKindsWithResolvedRefsCondition returns all the route kinds supported by the listener, along with the resolvedRefs
// condition, that is based on the presence of errors in such a field.
func getSupportedKindsWithResolvedRefsCondition(ctx context.Context, c client.Client, gatewayNamespace string, generation int64, listener gatewayv1.Listener) (supportedKinds []gatewayv1.RouteGroupKind, resolvedRefsCondition metav1.Condition, err error) {
	supportedKinds = make([]gatewayv1.RouteGroupKind, 0)
	resolvedRefsCondition = metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionResolvedRefs),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
		Message:            "Listeners' references are accepted.",
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}

	message := ""
	if listener.TLS != nil {
		// We currently do not support TLSRoutes, hence only TLS termination supported.
		if *listener.TLS.Mode != gatewayv1.TLSModeTerminate {
			resolvedRefsCondition.Status = metav1.ConditionFalse
			resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
			message = conditionMessage(message, "Only Terminate mode is supported")
		}
		// We currently do not support more that one listener certificate.
		if len(listener.TLS.CertificateRefs) != 1 {
			resolvedRefsCondition.Reason = string(ListenerReasonTooManyTLSSecrets)
			message = conditionMessage(message, "Only one certificate per listener is supported")
		} else {
			isValidGroupKind := true
			certificateRef := listener.TLS.CertificateRefs[0]
			if certificateRef.Group != nil && *certificateRef.Group != "" && *certificateRef.Group != gatewayv1.Group(corev1.SchemeGroupVersion.Group) {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
				message = conditionMessage(message, fmt.Sprintf("Group %s not supported in CertificateRef", *certificateRef.Group))
				isValidGroupKind = false
			}
			if certificateRef.Kind != nil && *certificateRef.Kind != "" && *certificateRef.Kind != gatewayv1.Kind("Secret") {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
				message = conditionMessage(message, fmt.Sprintf("Kind %s not supported in CertificateRef", *certificateRef.Kind))
				isValidGroupKind = false
			}
			secretNamespace := gatewayNamespace
			if certificateRef.Namespace != nil && *certificateRef.Namespace != "" {
				secretNamespace = string(*certificateRef.Namespace)
			}

			var secretExists bool
			if isValidGroupKind {
				// Get the secret and check it exists.
				certificateSecret := &corev1.Secret{}
				err = c.Get(ctx, types.NamespacedName{
					Namespace: secretNamespace,
					Name:      string(certificateRef.Name),
				}, certificateSecret)
				if err != nil {
					if !k8serrors.IsNotFound(err) {
						return
					}
					resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
					message = conditionMessage(message, fmt.Sprintf("Referenced secret %s/%s does not exist", secretNamespace, certificateRef.Name))
				} else {
					secretExists = true
				}
			}

			if secretExists {
				// In case there is a cross-namespace reference, check if there is any referenceGrant allowing it.
				if secretNamespace != gatewayNamespace {
					referenceGrantList := &gatewayv1beta1.ReferenceGrantList{}
					err = c.List(ctx, referenceGrantList, client.InNamespace(secretNamespace))
					if err != nil {
						return
					}
					if !isSecretCrossReferenceGranted(gatewayv1.Namespace(gatewayNamespace), certificateRef.Name, referenceGrantList.Items) {
						resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonRefNotPermitted)
						message = conditionMessage(message, fmt.Sprintf("Secret %s/%s reference not allowed by any referenceGrant", secretNamespace, certificateRef.Name))
					}
				}
			}
		}
	}

	if listener.AllowedRoutes == nil || len(listener.AllowedRoutes.Kinds) == 0 {
		supportedRoutes := supportedRoutesByProtocol()[listener.Protocol]
		for route := range supportedRoutes {
			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: (*gatewayv1.Group)(&gatewayv1.GroupVersion.Group),
				Kind:  route,
			})
		}
	} else {
		for _, k := range listener.AllowedRoutes.Kinds {
			validRoutes := supportedRoutesByProtocol()[listener.Protocol]
			if _, ok := validRoutes[k.Kind]; !ok || k.Group == nil || *k.Group != gatewayv1.Group(gatewayv1.GroupVersion.Group) {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidRouteKinds)
				message = conditionMessage(message, fmt.Sprintf("Route %s not supported", string(k.Kind)))
				continue
			}

			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: k.Group,
				Kind:  k.Kind,
			})
		}
	}

	if resolvedRefsCondition.Reason != string(gatewayv1.ListenerReasonResolvedRefs) {
		resolvedRefsCondition.Status = metav1.ConditionFalse
		resolvedRefsCondition.Message = message
	}

	return supportedKinds, resolvedRefsCondition, nil
}

func isSecretCrossReferenceGranted(gatewayNamespace gatewayv1.Namespace, secretName gatewayv1.ObjectName, referenceGrants []gatewayv1beta1.ReferenceGrant) bool {
	for _, rg := range referenceGrants {
		var fromFound bool
		for _, from := range rg.Spec.From {
			if from.Group != gatewayv1.GroupName {
				continue
			}
			if from.Kind != "Gateway" {
				continue
			}
			if from.Namespace != gatewayNamespace {
				continue
			}
			fromFound = true
			break
		}
		if fromFound {
			for _, to := range rg.Spec.To {
				if to.Group != "" && to.Group != "core" {
					continue
				}
				if to.Kind != "Secret" {
					continue
				}
				if to.Name != nil && secretName != *to.Name {
					continue
				}
				return true
			}
		}
	}
	return false
}

// conditionMessage updates a condition message string with an additional message, for use when a problem
// condition has multiple concurrent causes. It ensures all messages end with a period. New messages are
// appended to the end of the current message with a leading space separating them.
func conditionMessage(oldStr, newStr string) string {
	if oldStr == "" {
		return fmt.Sprintf("%s.", newStr)
	}
	if strings.HasSuffix(newStr, ".") {
		return fmt.Sprintf("%s %s", oldStr, newStr)
	}
	return fmt.Sprintf("%s %s.", oldStr, newStr)
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

func gatewayStatusNeedsUpdate(oldGateway, newGateway gatewayConditionsAndListenersAwareT) bool {
	oldCondAccepted, okOld := k8sutils.GetCondition(k8sutils.ConditionType(gatewayv1.GatewayConditionAccepted), oldGateway)
	newCondAccepted, _ := k8sutils.GetCondition(k8sutils.ConditionType(gatewayv1.GatewayConditionAccepted), newGateway)

	if !okOld || !areConditionsEqual(oldCondAccepted, newCondAccepted) {
		return true
	}

	if len(newGateway.Status.Listeners) != len(oldGateway.Status.Listeners) {
		return true
	}

	for i, newlistener := range newGateway.GetListenersConditions() {
		oldListener := oldGateway.Status.Listeners[i]
		if len(newlistener.Conditions) != len(oldListener.Conditions) {
			return true
		}
		if !cmp.Equal(newlistener.SupportedKinds, oldListener.SupportedKinds) {
			return true
		}

		for j, newListenerCond := range newlistener.Conditions {
			// Do not consider the programmed condition, as it depends on the DataPlane and ControlPlane status.
			if newListenerCond.Type != string(gatewayv1.ListenerConditionProgrammed) {
				if !areConditionsEqual(oldListener.Conditions[j], newListenerCond) {
					return true
				}
			} else {
				if oldListener.Conditions[j].Type != string(gatewayv1.ListenerConditionProgrammed) {
					return true
				}
			}
		}
	}
	return false
}

func areConditionsEqual(cond1, cond2 metav1.Condition) bool {
	return cond1.Type == cond2.Type &&
		cond1.Status == cond2.Status &&
		cond1.Reason == cond2.Reason &&
		cond1.Message == cond2.Message
}
