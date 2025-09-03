package dataplane

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"

	operatorv1alpha1 "github.com/kong/kong-operator/apis/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/controller/pkg/address"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

// ensureDataPlaneReadinessStatus ensures the readiness Status fields of DataPlane are set.
func ensureDataPlaneReadinessStatus(
	dataplane *operatorv1beta1.DataPlane,
	dataplaneDeploymentStatus appsv1.DeploymentStatus,
) {
	dataplane.Status.Replicas = dataplaneDeploymentStatus.Replicas
	dataplane.Status.ReadyReplicas = dataplaneDeploymentStatus.ReadyReplicas
}

func (r *Reconciler) ensureDataPlaneServiceStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	dataplaneServiceName string,
) (bool, error) {
	shouldUpdate := false
	if dataplane.Status.Service != dataplaneServiceName {
		dataplane.Status.Service = dataplaneServiceName
		shouldUpdate = true
	}

	if shouldUpdate {
		_, err := patchDataPlaneStatus(ctx, r.Client, log, dataplane)
		return true, err
	}
	return false, nil
}

// ensureDataPlaneAddressesStatus ensures that provided DataPlane's status addresses
// are as expected and patches its status if there's a difference between the
// current state and what's expected.
// It returns a boolean indicating if the patch has been triggered and an error.
func (r *Reconciler) ensureDataPlaneAddressesStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	dataplaneService *corev1.Service,
) (bool, error) {
	addresses, err := address.AddressesFromService(dataplaneService)
	if err != nil {
		return false, fmt.Errorf("failed getting addresses for service %s: %w", dataplaneService, err)
	}

	// Compare the lengths prior to cmp.Equal() because cmp.Equal() will return
	// false when comparing nil slice and 0 length slice.
	if len(addresses) != len(dataplane.Status.Addresses) ||
		!cmp.Equal(addresses, dataplane.Status.Addresses) {
		dataplane.Status.Addresses = addresses
		_, err := patchDataPlaneStatus(ctx, r.Client, log, dataplane)
		return true, err
	}

	return false, nil
}

// ensureMappedConfigMapToKongPluginInstallationForDataPlane ensures that the KongPluginInstallation
// resources referenced by the DataPlane are resolved and DataPlane is configured to use them.
// During resolving for each DataPlane based on each instance of KongPluginInstallation
// ConfigMap is created and mounted. The DataPlane manages its lifecycle. It returns a slice
// of custom plugins that are intended to be used to generate a Deployment.
func ensureMappedConfigMapToKongPluginInstallationForDataPlane(
	ctx context.Context, logger logr.Logger, c client.Client, dataplane *operatorv1beta1.DataPlane, configMapLabelSelector string,
) (cps []customPlugin, requeue bool, err error) {
	configMapsOwned, err := findCustomPluginConfigMapsOwnedByDataPlane(ctx, c, dataplane)
	if err != nil {
		return nil, false, err
	}
	configMapsToRetain := make(map[types.NamespacedName]struct{}, len(configMapsOwned))

	for _, kpiNN := range dataplane.Spec.PluginsToInstall {
		kpiNN := types.NamespacedName(kpiNN)
		if kpiNN.Namespace == "" {
			kpiNN.Namespace = dataplane.Namespace
		}

		var cp customPlugin
		cp, requeue, err = populateDedicatedConfigMapForKongPluginInstallation(
			ctx, logger, c, configMapsOwned, kpiNN, dataplane, configMapLabelSelector,
		)
		if err != nil || requeue {
			return nil, requeue, err
		}
		configMapsToRetain[cp.ConfigMapNN] = struct{}{}
		cps = append(cps, cp)
	}
	for _, cm := range configMapsOwned {
		if _, retain := configMapsToRetain[client.ObjectKeyFromObject(&cm)]; !retain {
			if err := c.Delete(ctx, &cm); client.IgnoreNotFound(err) != nil {
				return nil, false, err
			}
		}
	}

	return cps, false, nil
}

func findCustomPluginConfigMapsOwnedByDataPlane(
	ctx context.Context, c client.Client, dataplane *operatorv1beta1.DataPlane,
) ([]corev1.ConfigMap, error) {
	cms, err := k8sutils.ListConfigMapsForOwner(ctx, c, dataplane.GetUID())
	if err != nil {
		return nil, err
	}
	return lo.Filter(cms, func(cm corev1.ConfigMap, _ int) bool {
		_, isForKPI := cm.Annotations[consts.AnnotationMappedToKongPluginInstallation]
		return isForKPI
	}), nil
}

func populateDedicatedConfigMapForKongPluginInstallation(
	ctx context.Context,
	logger logr.Logger,
	c client.Client,
	cms []corev1.ConfigMap,
	kpiNN types.NamespacedName,
	dataplane *operatorv1beta1.DataPlane,
	configMapLabelSelector string,
) (cp customPlugin, requeue bool, err error) {
	kpi, ready, err := verifyKPIReadinessForDataPlane(ctx, logger, c, dataplane, kpiNN)
	if err != nil {
		return customPlugin{}, false, err
	}
	if !ready {
		return customPlugin{}, true, nil
	}

	var underlyingCM corev1.ConfigMap
	backingCMNN := types.NamespacedName{
		Namespace: kpi.Namespace,
		Name:      kpi.Status.UnderlyingConfigMapName,
	}
	log.Trace(logger, fmt.Sprintf("Fetch underlying ConfigMap %s for KongPluginInstallation", backingCMNN))
	if err := c.Get(ctx, backingCMNN, &underlyingCM); err != nil {
		return customPlugin{}, false, fmt.Errorf("could not fetch underlying ConfigMap to clone %s: %w", backingCMNN, err)
	}

	log.Trace(logger, "Find ConfigMap mapped to KongPluginInstallation")
	mappedConfigMapForKPI := lo.Filter(cms, func(cm corev1.ConfigMap, _ int) bool {
		kpiNN := cm.Annotations[consts.AnnotationMappedToKongPluginInstallation]
		return kpiNN == client.ObjectKeyFromObject(&kpi).String()
	})
	var cm corev1.ConfigMap
	switch len(mappedConfigMapForKPI) {
	case 0:
		log.Trace(logger, "Create new ConfigMap for KongPluginInstallation")
		cm.GenerateName = dataplane.Name + "-"
		cm.Namespace = dataplane.Namespace
		k8sresources.SetLabel(&cm, configMapLabelSelector, "true")
		k8sutils.SetOwnerForObject(&cm, dataplane)
		k8sresources.LabelObjectAsDataPlaneManaged(&cm)
		k8sresources.AnnotateConfigMapWithKongPluginInstallation(&cm, kpi)
		cm.Data = underlyingCM.Data
		if err := c.Create(ctx, &cm); err != nil {
			return customPlugin{}, false, fmt.Errorf("could not create new ConfigMap for KongPluginInstallation: %w", err)
		}
	case 1:
		log.Trace(logger, fmt.Sprintf("Check if update existing ConfigMap %s for KongPluginInstallation", client.ObjectKeyFromObject(&cm)))
		cm = mappedConfigMapForKPI[0]
		if maps.Equal(cm.Data, underlyingCM.Data) {
			log.Trace(logger, fmt.Sprintf("Nothing to update in existing ConfigMap %s for KongPluginInstallation", client.ObjectKeyFromObject(&cm)))
		} else {
			log.Trace(logger, fmt.Sprintf("Update existing ConfigMap %s for KongPluginInstallation", client.ObjectKeyFromObject(&cm)))
			cm.Data = underlyingCM.Data
			if err := c.Update(ctx, &cm); err != nil {
				if k8serrors.IsConflict(err) {
					return customPlugin{}, true, nil
				}
				return customPlugin{}, false, fmt.Errorf("could not update mapped: %w", err)
			}
		}

	default:
		// It should never happen.
		names := strings.Join(lo.Map(mappedConfigMapForKPI, func(cm corev1.ConfigMap, _ int) string {
			return client.ObjectKeyFromObject(&cm).String()
		}), ", ")
		return customPlugin{}, false, fmt.Errorf("unexpected error happened - more than one ConfigMap found: %s", names)
	}
	return customPlugin{
		Name:        kpi.Name,
		ConfigMapNN: client.ObjectKeyFromObject(&cm),
		Generation:  kpi.Generation,
	}, false, nil
}

// verifyKPIReadinessForDataPlane updates DataPlane status conditions based on status of KPI object.
// Possible states: it does not exist or it hasn't been fully reconciled yet, or it's failing. Those
// problems can be fixed by the user or they're transient. Use returned kpi only when ready is true.
func verifyKPIReadinessForDataPlane(
	ctx context.Context, logger logr.Logger, c client.Client, dataplane *operatorv1beta1.DataPlane, kpiNN types.NamespacedName,
) (kpi operatorv1alpha1.KongPluginInstallation, ready bool, err error) {
	// Report to user when KPI does not exist or it hasn't been fully reconciled yet.
	// It can be fixed by the user or it's transient.
	if err := c.Get(ctx, kpiNN, &kpi); err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("referenced KongPluginInstallation %s not found", kpiNN)
			markErr := ensureDataPlaneIsMarkedNotReady(ctx, logger, c, dataplane, kcfgdataplane.DataPlaneConditionReferencedResourcesNotAvailable, msg)
			return kpi, false, markErr
		} else {
			return kpi, true, err
		}
	}
	if len(kpi.Status.Conditions) == 0 || lo.ContainsBy(kpi.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == string(operatorv1alpha1.KongPluginInstallationConditionStatusAccepted) &&
			c.Status == metav1.ConditionFalse &&
			c.Reason == string(operatorv1alpha1.KongPluginInstallationReasonPending)
	}) {
		msgPending := fmt.Sprintf("please wait, referenced KongPluginInstallation %s has not been fully reconciled yet", kpiNN)
		markErr := ensureDataPlaneIsMarkedNotReady(
			ctx, logger, c, dataplane, kcfgdataplane.DataPlaneConditionReferencedResourcesNotAvailable, msgPending,
		)
		return kpi, false, markErr
	}
	if lo.ContainsBy(kpi.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == string(operatorv1alpha1.KongPluginInstallationConditionStatusAccepted) &&
			c.Status == metav1.ConditionFalse &&
			c.Reason == string(operatorv1alpha1.KongPluginInstallationReasonFailed)
	}) {
		msgFailed := fmt.Sprintf("something wrong with referenced KongPluginInstallation %s, please check it", kpiNN)
		markErr := ensureDataPlaneIsMarkedNotReady(
			ctx, logger, c, dataplane, kcfgdataplane.DataPlaneConditionReferencedResourcesNotAvailable, msgFailed,
		)
		return kpi, false, markErr
	}
	return kpi, true, nil
}

// isSameDataPlaneCondition returns true if two `metav1.Condition`s
// indicates the same condition of a `DataPlane` resource.
func isSameDataPlaneCondition(condition1, condition2 metav1.Condition) bool {
	return condition1.Type == condition2.Type &&
		condition1.Status == condition2.Status &&
		condition1.Reason == condition2.Reason &&
		condition1.Message == condition2.Message
}

func ensureDataPlaneIsMarkedNotReady(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	dataplane *operatorv1beta1.DataPlane,
	reason kcfgconsts.ConditionReason, message string,
) error {
	notReadyCondition := metav1.Condition{
		Type:               string(kcfgdataplane.ReadyType),
		Status:             metav1.ConditionFalse,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: dataplane.Generation,
		LastTransitionTime: metav1.Now(),
	}

	conditionFound := false
	shouldUpdate := false
	for i, condition := range dataplane.Status.Conditions {
		// update the condition if condition has type `Ready`, and the condition is not the same.
		if condition.Type == string(kcfgdataplane.ReadyType) {
			conditionFound = true
			// update the slice if the condition is not the same as we expected.
			if !isSameDataPlaneCondition(notReadyCondition, condition) {
				dataplane.Status.Conditions[i] = notReadyCondition
				shouldUpdate = true
			}
		}
	}

	if !conditionFound {
		// append a new condition if Ready condition is not found.
		dataplane.Status.Conditions = append(dataplane.Status.Conditions, notReadyCondition)
		shouldUpdate = true
	}

	if shouldUpdate {
		_, err := patchDataPlaneStatus(ctx, c, log, dataplane)
		return err
	}
	return nil
}

// ensureDataPlaneIngressServiceAnnotationsUpdated updates annotations of existing ingress service
// owned by the `DataPlane`. It first removes outdated annotations and then update annotations
// in current spec of `DataPlane`.
func ensureDataPlaneIngressServiceAnnotationsUpdated(
	dataplane *operatorv1beta1.DataPlane, existingAnnotations map[string]string, generatedAnnotations map[string]string,
) (bool, map[string]string, error) {
	// Remove annotations applied from previous version of DataPlane but removed in the current version.
	// Should be done before updating new annotations, because the updating process will overwrite the annotation
	// to save last applied annotations.
	outdatedAnnotations, err := extractOutdatedDataPlaneIngressServiceAnnotations(dataplane, existingAnnotations)
	if err != nil {
		return true, existingAnnotations, fmt.Errorf("failed to extract outdated annotations: %w", err)
	}
	var shouldUpdate bool
	for k := range outdatedAnnotations {
		if _, ok := existingAnnotations[k]; ok {
			delete(existingAnnotations, k)
			shouldUpdate = true
		}
	}
	if generatedAnnotations != nil && existingAnnotations == nil {
		existingAnnotations = map[string]string{}
	}
	// set annotations by current specified ingress service annotations.
	for k, v := range generatedAnnotations {
		if existingAnnotations[k] != v {
			existingAnnotations[k] = v
			shouldUpdate = true
		}
	}
	return shouldUpdate, existingAnnotations, nil
}

// dataPlaneIngressServiceIsReady returns:
//   - true for DataPlanes that do not have the Ingress Service type set as LoadBalancer
//   - true for DataPlanes that have the Ingress Service type set as LoadBalancer and
//     which have at least one IP or Hostname in their Ingress Service Status
//   - false otherwise.
func dataPlaneIngressServiceIsReady(dataplaneIngressService *corev1.Service) bool {
	// If the DataPlane ingress Service is not of a LoadBalancer type then
	// report the DataPlane as Ready.
	// We don't check DataPlane spec to see if the Service is of type LoadBalancer
	// because we might be relying on the default Service type which might change.
	if dataplaneIngressService.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return true
	}

	ingressStatuses := dataplaneIngressService.Status.LoadBalancer.Ingress
	// If there are ingress statuses attached to the ingress Service, check
	// if there are IPs of Hostnames specified.
	// If that's the case, the DataPlane is Ready.
	for _, ingressStatus := range ingressStatuses {
		if ingressStatus.Hostname != "" || ingressStatus.IP != "" {
			return true
		}
	}
	// Otherwise the DataPlane is not Ready.
	return false
}

// patchDataPlaneStatus patches the resource status only when there are changes
// that requires it.
func patchDataPlaneStatus(ctx context.Context, cl client.Client, logger logr.Logger, updated *operatorv1beta1.DataPlane) (bool, error) {
	current := &operatorv1beta1.DataPlane{}

	err := cl.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if client.IgnoreNotFound(err) != nil {
		return false, err
	}

	if k8sutils.ConditionsNeedsUpdate(current, updated) ||
		addressesChanged(current, updated) ||
		readinessChanged(current, updated) ||
		current.Status.Service != updated.Status.Service ||
		current.Status.Selector != updated.Status.Selector {

		log.Debug(logger, "patching DataPlane status", "status", updated.Status)
		return true, cl.Status().Patch(ctx, updated, client.MergeFrom(current))
	}

	return false, nil
}

// addressesChanged returns a boolean indicating whether the addresses in provided
// DataPlane stauses differ.
func addressesChanged(current, updated *operatorv1beta1.DataPlane) bool {
	return !cmp.Equal(current.Status.Addresses, updated.Status.Addresses)
}

func readinessChanged(current, updated *operatorv1beta1.DataPlane) bool {
	return current.Status.ReadyReplicas != updated.Status.ReadyReplicas ||
		current.Status.Replicas != updated.Status.Replicas
}
