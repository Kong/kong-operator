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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller/pkg/extensions"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/controller/pkg/secrets/ref"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/pkg/consts"
	gatewayutils "github.com/kong/gateway-operator/pkg/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	kcfgdataplane "github.com/kong/kubernetes-configuration/api/gateway-operator/dataplane"
	kcfggateway "github.com/kong/kubernetes-configuration/api/gateway-operator/gateway"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Reconciler Helpers
// -----------------------------------------------------------------------------

func (r *Reconciler) createDataPlane(ctx context.Context,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1beta1.GatewayConfiguration,
) (*operatorv1beta1.DataPlane, error) {
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", gateway.Name)),
		},
	}
	if gatewayConfig.Spec.DataPlaneOptions != nil {
		dataplane.Spec.DataPlaneOptions = *gatewayConfigDataPlaneOptionsToDataPlaneOptions(gatewayConfig.Namespace, *gatewayConfig.Spec.DataPlaneOptions)
	}
	setDataPlaneOptionsDefaults(&dataplane.Spec.DataPlaneOptions, r.DefaultDataPlaneImage)
	if err := setDataPlaneIngressServicePorts(&dataplane.Spec.DataPlaneOptions, gateway.Spec.Listeners, gatewayConfig.Spec.ListenersOptions); err != nil {
		return nil, err
	}

	dataplane.Spec.Extensions = extensions.MergeExtensions(gatewayConfig.Spec.Extensions, dataplane.Spec.Extensions)

	k8sutils.SetOwnerForObject(dataplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(dataplane)
	err := r.Create(ctx, dataplane)
	if err != nil {
		return nil, err
	}
	return dataplane, nil
}

func (r *Reconciler) createControlPlane(
	ctx context.Context,
	gatewayClass *gatewayv1.GatewayClass,
	gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1beta1.GatewayConfiguration,
	dataplaneName string,
) error {
	controlplane := &operatorv1beta1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    gateway.Namespace,
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", gateway.Name)),
		},
		Spec: operatorv1beta1.ControlPlaneSpec{
			GatewayClass: (*gatewayv1.ObjectName)(&gatewayClass.Name),
		},
	}
	if gatewayConfig.Spec.ControlPlaneOptions != nil {
		controlplane.Spec.ControlPlaneOptions = *gatewayConfig.Spec.ControlPlaneOptions
	}
	if controlplane.Spec.DataPlane == nil {
		controlplane.Spec.DataPlane = &dataplaneName
	}

	controlplane.Spec.Extensions = extensions.MergeExtensions(gatewayConfig.Spec.Extensions, controlplane.Spec.Extensions)

	setControlPlaneOptionsDefaults(&controlplane.Spec.ControlPlaneOptions)
	k8sutils.SetOwnerForObject(controlplane, gateway)
	gatewayutils.LabelObjectAsGatewayManaged(controlplane)
	return r.Create(ctx, controlplane)
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

func gatewayConfigDataPlaneOptionsToDataPlaneOptions(
	gatewayConfigNamespace string,
	opts operatorv1beta1.GatewayConfigDataPlaneOptions,
) *operatorv1beta1.DataPlaneOptions {
	dataPlaneOptions := &operatorv1beta1.DataPlaneOptions{
		Deployment: opts.Deployment,
		Extensions: opts.Extensions,
	}

	if len(opts.PluginsToInstall) > 0 {
		dataPlaneOptions.PluginsToInstall = lo.Map(opts.PluginsToInstall,
			func(pluginReference operatorv1beta1.NamespacedName, _ int) operatorv1beta1.NamespacedName {
				// When Namespace is not provided, the GatewayConfiguration's namespace is assumed.
				if pluginReference.Namespace == "" {
					pluginReference.Namespace = gatewayConfigNamespace
				}
				return pluginReference
			},
		)
	}

	if opts.Resources != nil {
		dataPlaneOptions.Resources.PodDisruptionBudget = opts.Resources.PodDisruptionBudget
	}

	if opts.Network.Services != nil && opts.Network.Services.Ingress != nil {
		dataPlaneOptions.Network = operatorv1beta1.DataPlaneNetworkOptions{
			Services: &operatorv1beta1.DataPlaneServices{
				Ingress: &operatorv1beta1.DataPlaneServiceOptions{
					ServiceOptions: operatorv1beta1.ServiceOptions{
						Type:                  opts.Network.Services.Ingress.Type,
						Annotations:           opts.Network.Services.Ingress.Annotations,
						ExternalTrafficPolicy: opts.Network.Services.Ingress.ExternalTrafficPolicy,
						Name:                  opts.Network.Services.Ingress.Name,
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

func (r *Reconciler) getOrCreateGatewayConfiguration(ctx context.Context, gatewayClass *gatewayv1.GatewayClass) (*operatorv1beta1.GatewayConfiguration, error) {
	gatewayConfig, err := r.getGatewayConfigForGatewayClass(ctx, gatewayClass)
	if err != nil {
		if errors.Is(err, operatorerrors.ErrObjectMissingParametersRef) {
			return new(operatorv1beta1.GatewayConfiguration), nil
		}
		return nil, err
	}

	return gatewayConfig, nil
}

func (r *Reconciler) getGatewayConfigForGatewayClass(ctx context.Context, gatewayClass *gatewayv1.GatewayClass) (*operatorv1beta1.GatewayConfiguration, error) {
	if gatewayClass.Spec.ParametersRef == nil {
		return nil, fmt.Errorf("%w, gatewayClass = %s", operatorerrors.ErrObjectMissingParametersRef, gatewayClass.Name)
	}

	if string(gatewayClass.Spec.ParametersRef.Group) != operatorv1beta1.SchemeGroupVersion.Group ||
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
							operatorv1beta1.SchemeGroupVersion.Group, "GatewayConfiguration"),
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

	gatewayConfig := new(operatorv1beta1.GatewayConfiguration)
	return gatewayConfig, r.Get(ctx, client.ObjectKey{
		Namespace: string(*gatewayClass.Spec.ParametersRef.Namespace),
		Name:      gatewayClass.Spec.ParametersRef.Name,
	}, gatewayConfig)
}

func (r *Reconciler) ensureDataPlaneHasNetworkPolicy(
	ctx context.Context,
	gateway *gwtypes.Gateway,
	dataplane *operatorv1beta1.DataPlane,
	controlplane *operatorv1beta1.ControlPlane,
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
			if err := r.Patch(ctx, existingPolicy, client.MergeFrom(old)); err != nil {
				return false, fmt.Errorf("failed updating DataPlane's NetworkPolicy %s: %w", existingPolicy.Name, err)
			}
			return true, nil
		}
		return false, nil
	}

	return true, r.Create(ctx, generatedPolicy)
}

func generateDataPlaneNetworkPolicy(
	namespace string,
	dataplane *operatorv1beta1.DataPlane,
	controlplane *operatorv1beta1.ControlPlane,
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
		err = r.Delete(ctx, &controlplanes[i])
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
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
		err = r.Delete(ctx, &dataplanes[i])
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
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
		err = r.Delete(ctx, &networkPolicies[i])
		if client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
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

// initProgrammedAndListenersStatus initializes the gateway Programmed condition
// by setting the underlying Gateway Programmed status to false.
// It also sets the listeners Programmed condition by setting the underlying
// Listener Programmed status to false.
func (g *gatewayConditionsAndListenersAwareT) initProgrammedAndListenersStatus() {
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			kcfgconsts.ConditionType(gatewayv1.GatewayConditionProgrammed),
			metav1.ConditionFalse,
			kcfgconsts.ConditionReason(gatewayv1.GatewayReasonPending),
			kcfgdataplane.DependenciesNotReadyMessage,
			g.Generation),
		g)
	for i := range g.Spec.Listeners {
		lStatus := listenerConditionsAware(&g.Status.Listeners[i])
		cond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.ListenerConditionProgrammed), lStatus)
		if !ok || cond.ObservedGeneration != g.Generation {
			k8sutils.SetCondition(metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonPending),
				ObservedGeneration: g.Generation,
				LastTransitionTime: metav1.Now(),
			}, lStatus)
		}
	}
}

func (g *gatewayConditionsAndListenersAwareT) setResolvedRefsAndSupportedKinds(ctx context.Context, c client.Client) error {
	for i, listener := range g.Spec.Listeners {
		supportedKinds, resolvedRefsCondition, err := getSupportedKindsWithResolvedRefsCondition(ctx, c, *g.Gateway, g.Generation, listener)
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

// setAcceptedAndAttachedRoutes sets the listeners and gateway Accepted condition according to the Gateway API specification.
// It also sets the AttachedRoutes field in the listener status.
func (g *gatewayConditionsAndListenersAwareT) setAcceptedAndAttachedRoutes(ctx context.Context, c client.Client) error {
	for i, listener := range g.Spec.Listeners {
		acceptedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonAccepted),
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: g.Generation,
		}

		if _, protocolSupported := supportedRoutesByProtocol()[listener.Protocol]; !protocolSupported {
			acceptedCondition.Status = metav1.ConditionFalse
			acceptedCondition.Reason = string(gatewayv1.ListenerReasonUnsupportedProtocol)
		}
		listenerConditionsAware := listenerConditionsAware(&g.Status.Listeners[i])
		listenerConditionsAware.SetConditions(append(listenerConditionsAware.Conditions, acceptedCondition))

		// AttachedRoutes
		count, err := countAttachedRoutesForGatewayListener(ctx, g.Gateway, g.Spec.Listeners[i], c)
		if err != nil {
			return fmt.Errorf("failed to count attached routes for Gateway %s: %w", client.ObjectKeyFromObject(g), err)
		}

		g.Status.Listeners[i].AttachedRoutes = count
	}

	k8sutils.SetAcceptedConditionOnGateway(g)
	return nil
}

// countAttachedRoutesForGatewayListener counts the number of attached routes for a given listener.
// It takes into account the AllowedRoutes field in the listener spec and route's ParentRefs.
// It returns the number of attached routes and an error.
func countAttachedRoutesForGatewayListener(ctx context.Context, g *gwtypes.Gateway, listener gwtypes.Listener, cl client.Client) (int32, error) {
	allowedRoutes := listener.AllowedRoutes
	// Gateway API defines a default value for AllowedRoutes, so if this is nil there's something wrong.
	if allowedRoutes == nil {
		return 0, fmt.Errorf("AllowedRoutes is nil for listener %s in gateway %s",
			listener.Name, client.ObjectKeyFromObject(g),
		)
	}

	var (
		count int32
		opts  []client.ListOption
	)

	namespaces := allowedRoutes.Namespaces
	// Gateway API defines a default value for AllowedRoutes.Namespaces, so
	// if this is nil there's something wrong.
	if namespaces == nil || namespaces.From == nil {
		return 0, fmt.Errorf("AllowedRoutes.Namespaces is nil for listener %s in gateway %s",
			listener.Name, client.ObjectKeyFromObject(g),
		)
	}

	switch *namespaces.From {
	case gatewayv1.NamespacesFromNone:
		// No namespaces are allowed, so no routes can be attached.
		return 0, nil
	case gatewayv1.NamespacesFromAll:
	case gatewayv1.NamespacesFromSame:
		opts = append(opts, client.InNamespace(g.Namespace))
	case gatewayv1.NamespacesFromSelector:
		var nsList corev1.NamespaceList

		s, err := metav1.LabelSelectorAsSelector(listener.AllowedRoutes.Namespaces.Selector)
		if err != nil {
			return 0, fmt.Errorf("failed to create requirement for namespace selector (for Gateway %s): %w",
				client.ObjectKeyFromObject(g), err,
			)
		}
		reqs, selectable := s.Requirements()
		if !selectable {
			return 0, fmt.Errorf("namespace selector is not selectable (for Gateway %s)", client.ObjectKeyFromObject(g))
		}
		labelSelector := labels.NewSelector()
		for _, req := range reqs {
			labelSelector = labelSelector.Add(req)
		}
		if err := cl.List(ctx, &nsList, &client.ListOptions{
			LabelSelector: labelSelector,
		}); err != nil {
			if k8serrors.IsNotFound(err) {
				return 0, nil
			}
			return 0, fmt.Errorf("failed to list namespaces for gateway %s: %w", g.Name, err)
		}

		switch len(nsList.Items) {
		case 0:
			// If no namespaces matching the selector are found, set the AttachedRoutes to 0 as
			// there are no routes to attach.
			return 0, nil

		default:
			for _, ns := range nsList.Items {
				opts = append(opts, client.InNamespace(ns.Name))
			}
		}
	}

	kindsForProtocol, protocolSupported := supportedRoutesByProtocol()[listener.Protocol]
	switch len(allowedRoutes.Kinds) {
	case 0:
		if protocolSupported {
			for k := range kindsForProtocol {
				// NOTE: Count other types of routes when they are supported.

				switch k {
				case "HTTPRoute":
					httpRoutes, err := gatewayutils.ListHTTPRoutesForGateway(ctx, cl, g, opts...)
					if err != nil {
						return 0, fmt.Errorf(
							"failed to list HTTPRoutes for Gateway %s when counting AttachedRoutes: %w",
							client.ObjectKeyFromObject(g), err,
						)
					}
					count += countAttachedHTTPRoutes(listener.Name, httpRoutes)
				default:
					return 0, fmt.Errorf("unsupported route kind: %T", k)
				}
			}
		}
	default:
		if lo.ContainsBy(allowedRoutes.Kinds, func(gvk gatewayv1.RouteGroupKind) bool {
			if _, ok := kindsForProtocol[gvk.Kind]; !ok {
				return false
			}
			return gvk.Group != nil && *gvk.Group == gatewayv1.Group(gatewayv1.GroupVersion.Group)
		}) {
			httpRoutes, err := gatewayutils.ListHTTPRoutesForGateway(ctx, cl, g, opts...)
			if err != nil {
				return 0, fmt.Errorf(
					"failed to list HTTPRoutes for Gateway %s when counting AttachedRoutes: %w",
					client.ObjectKeyFromObject(g), err,
				)
			}

			count += countAttachedHTTPRoutes(listener.Name, httpRoutes)
		}
	}

	return count, nil
}

// countAttachedHTTPRoutes counts the number of attached HTTPRoutes for a given listener,
// taking into account the ParentRefs' sectionName.
func countAttachedHTTPRoutes(listenerName gatewayv1.SectionName, httpRoutes []gatewayv1.HTTPRoute) int32 {
	var count int32

	for _, httpRoute := range httpRoutes {
		if lo.ContainsBy(httpRoute.Spec.ParentRefs, func(parentRef gatewayv1.ParentReference) bool {
			return parentRef.SectionName == nil || *parentRef.SectionName == listenerName
		}) {
			count++
		}
	}

	return count
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

// setProgrammed sets the gateway Programmed condition by setting the underlying
// Gateway Programmed status to true.
// It also sets the listeners Programmed condition by setting the underlying
// Listener Programmed status to true.
func (g *gatewayConditionsAndListenersAwareT) setProgrammed() {
	k8sutils.SetProgrammed(g)

	for i := range g.Status.Listeners {
		listener := &g.Status.Listeners[i]
		programmedCondition := metav1.Condition{
			Type:               string(gatewayv1.ListenerConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.ListenerReasonProgrammed),
			ObservedGeneration: g.GetGeneration(),
			LastTransitionTime: metav1.Now(),
		}
		listenerStatus := listenerConditionsAware(listener)
		rCond, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.ListenerConditionResolvedRefs), listenerStatus)
		if ok && rCond.Status == metav1.ConditionFalse {
			programmedCondition.Status = metav1.ConditionFalse
			programmedCondition.Reason = string(gatewayv1.ListenerReasonPending)
			programmedCondition.Message = "Listener references are not resolved yet."
		}
		k8sutils.SetCondition(programmedCondition, listenerStatus)
	}
}

func setDataPlaneIngressServicePorts(
	opts *operatorv1beta1.DataPlaneOptions,
	listeners []gatewayv1.Listener,
	listenersOpts []operatorv1beta1.GatewayConfigurationListenerOptions,
) error {

	// Check if all the names in GatewayConfiguration's spec.listenersOptions matches a listener in Gateway.
	for i, listenerOpts := range listenersOpts {
		if !lo.ContainsBy(listeners, func(l gatewayv1.Listener) bool {
			return l.Name == listenerOpts.Name
		}) {
			return fmt.Errorf("GatewayConfiguration.spec.listenersOptions[%d]: name '%s' not in gateway's listeners", i, listenerOpts.Name)
		}
	}

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

		// Update the service port by GatewayConfiguration's spec.listenersOptions if there is a matching item by listener name.
		if listenerOpt, found := lo.Find(listenersOpts, func(listenerOpts operatorv1beta1.GatewayConfigurationListenerOptions) bool {
			return listenerOpts.Name == l.Name
		}); found {
			port.NodePort = listenerOpt.NodePort
		}

		opts.Network.Services.Ingress.Ports = append(opts.Network.Services.Ingress.Ports, port)
	}
	return errs
}

// getSupportedKindsWithResolvedRefsCondition returns all the route kinds supported by the listener, along with the resolvedRefs
// condition, that is based on the presence of errors in such a field.
func getSupportedKindsWithResolvedRefsCondition(ctx context.Context, c client.Client, gateway gatewayv1.Gateway, generation int64, listener gatewayv1.Listener) (supportedKinds []gatewayv1.RouteGroupKind, resolvedRefsCondition metav1.Condition, err error) {
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
			resolvedRefsCondition.Reason = string(kcfggateway.ListenerReasonTooManyTLSSecrets)
			message = conditionMessage(message, "Only one certificate per listener is supported")
		} else {
			isValidGroupKind := true
			certificateRef := listener.TLS.CertificateRefs[0]
			gatewayNamespace := gatewayv1.Namespace(gateway.Namespace)
			ref.EnsureNamespaceInSecretRef(&certificateRef, gatewayNamespace)

			if err := ref.DoesFieldReferenceCoreV1Secret(certificateRef, "CertificateRef"); err != nil {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
				message = conditionMessage(message, err.Error())
				isValidGroupKind = false
			}

			msg, isReferenceGranted, err := ref.CheckReferenceGrantForSecret(ctx, c, &gateway, certificateRef)
			if err != nil {
				return nil, metav1.Condition{}, fmt.Errorf("failed to resolve reference: %w", err)
			}
			if !isReferenceGranted {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonRefNotPermitted)
				message = conditionMessage(message, msg)
			}

			var secretExists bool
			certificateSecret := &corev1.Secret{}
			if isValidGroupKind && isReferenceGranted {
				// Get the secret and check it exists.
				err = c.Get(ctx, types.NamespacedName{
					Namespace: string(*certificateRef.Namespace),
					Name:      string(certificateRef.Name),
				}, certificateSecret)
				if err != nil {
					if !k8serrors.IsNotFound(err) {
						return nil, metav1.Condition{}, fmt.Errorf("failed to get Secret: %w", err)
					}
					resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
					message = conditionMessage(message, fmt.Sprintf("Referenced secret %s/%s does not exist", *certificateRef.Namespace, certificateRef.Name))
				} else {
					secretExists = true
				}
			}

			if isReferenceGranted && secretExists {
				// Check if the secret is a valid TLS secret.
				if !secrets.IsTLSSecretValid(certificateSecret) {
					resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidCertificateRef)
					message = conditionMessage(message, "Referenced secret does not contain a valid TLS certificate")
				}
			}
		}
	}

	if listener.AllowedRoutes == nil || len(listener.AllowedRoutes.Kinds) == 0 {
		supportedRoutes := supportedRoutesByProtocol()[listener.Protocol]
		for routeKind := range supportedRoutes {
			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: (*gatewayv1.Group)(&gatewayv1.GroupVersion.Group),
				Kind:  routeKind,
			})
		}
	} else {
		for _, routeGK := range listener.AllowedRoutes.Kinds {
			validRoutes := supportedRoutesByProtocol()[listener.Protocol]
			if _, ok := validRoutes[routeGK.Kind]; !ok || routeGK.Group == nil || *routeGK.Group != gatewayv1.Group(gatewayv1.GroupVersion.Group) {
				resolvedRefsCondition.Reason = string(gatewayv1.ListenerReasonInvalidRouteKinds)
				message = conditionMessage(message, fmt.Sprintf("Route %s not supported", string(routeGK.Kind)))
				continue
			}

			supportedKinds = append(supportedKinds, gatewayv1.RouteGroupKind{
				Group: routeGK.Group,
				Kind:  routeGK.Kind,
			})
		}
	}

	if resolvedRefsCondition.Reason != string(gatewayv1.ListenerReasonResolvedRefs) {
		resolvedRefsCondition.Status = metav1.ConditionFalse
		resolvedRefsCondition.Message = message
	}

	return supportedKinds, resolvedRefsCondition, nil
}

// conditionMessage updates a condition message string with an additional message, for use when a problem
// condition has multiple concurrent causes. It ensures all messages end with a period. New messages are
// appended to the end of the current message with a leading space separating them.
func conditionMessage(oldStr, newStr string) string {
	if len(newStr) > 0 && !strings.HasSuffix(newStr, ".") {
		newStr += "."
	}
	if oldStr == "" {
		return newStr
	}
	return fmt.Sprintf("%s %s", oldStr, newStr)
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
	oldCondAccepted, okOld := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayConditionAccepted), oldGateway)
	newCondAccepted, _ := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayConditionAccepted), newGateway)

	if !okOld || !areConditionsEqual(oldCondAccepted, newCondAccepted) {
		return true
	}

	if len(newGateway.Status.Listeners) != len(oldGateway.Status.Listeners) {
		return true
	}

	for i, newListener := range newGateway.GetListenersConditions() {
		oldListener := oldGateway.Status.Listeners[i]
		if newListener.AttachedRoutes != oldListener.AttachedRoutes {
			return true
		}
		if len(newListener.Conditions) != len(oldListener.Conditions) {
			return true
		}
		if !cmp.Equal(newListener.SupportedKinds, oldListener.SupportedKinds) {
			return true
		}

		for j, newListenerCond := range newListener.Conditions {
			switch newListenerCond.Type {
			case string(gatewayv1.ListenerConditionProgrammed):
				// Do not consider the programmed condition, as it depends on the DataPlane and ControlPlane status.
				if oldListener.Conditions[j].Type != string(gatewayv1.ListenerConditionProgrammed) {
					return true
				}
			default:
				if !areConditionsEqual(oldListener.Conditions[j], newListenerCond) {
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
		cond1.Message == cond2.Message &&
		cond1.ObservedGeneration == cond2.ObservedGeneration
}
