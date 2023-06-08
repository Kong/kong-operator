package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/versions"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureIsMarkedScheduled(
	dataplane *operatorv1alpha1.DataPlane,
) bool {
	_, present := k8sutils.GetCondition(DataPlaneConditionTypeProvisioned, dataplane)
	if !present {
		condition := k8sutils.NewCondition(
			DataPlaneConditionTypeProvisioned,
			metav1.ConditionFalse,
			DataPlaneConditionReasonPodsNotReady,
			"DataPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, dataplane)
		return true
	}
	return false
}

func (r *DataPlaneReconciler) ensureIsMarkedProvisioned(
	dataplane *operatorv1alpha1.DataPlane,
) {
	condition := k8sutils.NewCondition(
		DataPlaneConditionTypeProvisioned,
		metav1.ConditionTrue,
		DataPlaneConditionReasonPodsReady,
		"pods for all Deployments are ready",
	)
	k8sutils.SetCondition(condition, dataplane)
	k8sutils.SetReady(dataplane, dataplane.Generation)
}

func addressOf[T any](v T) *T {
	return &v
}

func (r *DataPlaneReconciler) ensureDataPlaneServiceStatus(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
	dataplaneServiceName string,
) (bool, error) {
	if dataplane.Status.Service != dataplaneServiceName {
		dataplane.Status.Service = dataplaneServiceName
		return true, r.Status().Update(ctx, dataplane)
	}
	return false, nil
}

// ensureDataPlaneAddressesStatus ensures that provided DataPlane's status addresses
// are as expected and pathes its status if there's a difference between the
// current state and what's expected.
// It returns a boolean indicating if the patch has been trigerred and an error.
func (r *DataPlaneReconciler) ensureDataPlaneAddressesStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1alpha1.DataPlane,
	dataplaneService *corev1.Service,
) (bool, error) {
	addresses, err := addressesFromService(dataplaneService)
	if err != nil {
		return false, fmt.Errorf("failed getting addresses for service %s: %w", dataplaneService, err)
	}

	// Compare the lengths prior to cmp.Equal() because cmp.Equal() will return
	// false when comparing nil slice and 0 length slice.
	if len(addresses) != len(dataplane.Status.Addresses) ||
		!cmp.Equal(addresses, dataplane.Status.Addresses) {
		dataplane.Status.Addresses = addresses
		return true, r.patchStatus(ctx, log, dataplane)
	}

	return false, nil
}

// isSameDataPlaneCondition returns true if two `metav1.Condition`s
// indicates the same condition of a `DataPlane` resource.
func isSameDataPlaneCondition(condition1, condition2 metav1.Condition) bool {
	return condition1.Type == condition2.Type &&
		condition1.Status == condition2.Status &&
		condition1.Reason == condition2.Reason &&
		condition1.Message == condition2.Message
}

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedNotProvisioned(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
	reason k8sutils.ConditionReason, message string,
) error {
	notProvisionedCondition := metav1.Condition{
		Type:               string(DataPlaneConditionTypeProvisioned),
		Status:             metav1.ConditionFalse,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: dataplane.Generation,
		LastTransitionTime: metav1.Now(),
	}

	conditionFound := false
	shouldUpdate := false
	for i, condition := range dataplane.Status.Conditions {
		// update the condition if condition has type `provisioned`, and the condition is not the same.
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			conditionFound = true
			// update the slice if the condition is not the same as we expected.
			if !isSameDataPlaneCondition(notProvisionedCondition, condition) {
				dataplane.Status.Conditions[i] = notProvisionedCondition
				shouldUpdate = true
			}
		}
	}

	if !conditionFound {
		// append a new condition if provisioned condition is not found.
		dataplane.Status.Conditions = append(dataplane.Status.Conditions, notProvisionedCondition)
		shouldUpdate = true
	}

	if shouldUpdate {
		return r.Status().Update(ctx, dataplane)
	}
	return nil
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Owned Resource Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureCertificate(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
	adminServiceName string,
) (bool, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature, certificatesv1.UsageServerAuth,
	}
	return maybeCreateCertificateSecret(ctx,
		dataplane,
		fmt.Sprintf("*.%s.%s.svc", adminServiceName, dataplane.Namespace),
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.Client)
}

type CreatedUpdatedOrNoop byte

const (
	Created CreatedUpdatedOrNoop = iota
	Updated
	Noop
)

func (r *DataPlaneReconciler) ensureDeploymentForDataPlane(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1alpha1.DataPlane,
	certSecretName string,
) (res CreatedUpdatedOrNoop, deploy *appsv1.Deployment, err error) {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return Noop, nil, err
	}

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, r.Client, deployments); err != nil {
			return Noop, nil, err
		}
		return Updated, nil, errors.New("number of deployments reduced")
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !r.DevelopmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsDataPlaneImageVersionSupported)
	}
	dataplaneImage, err := generateDataPlaneImage(dataplane, versionValidationOptions...)
	if err != nil {
		return Noop, nil, err
	}
	generatedDeployment := k8sresources.GenerateNewDeploymentForDataPlane(dataplane, dataplaneImage, certSecretName)
	k8sutils.SetOwnerForObject(generatedDeployment, dataplane)
	addLabelForDataplane(generatedDeployment)

	if count == 1 {
		var updated bool
		existingDeployment := &deployments[0]
		updated, existingDeployment.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingDeployment.ObjectMeta, generatedDeployment.ObjectMeta)

		// update cluster certiifcate if needed.
		if r.deploymentSpecVolumesNeedsUpdate(&existingDeployment.Spec, &generatedDeployment.Spec) {
			existingDeployment.Spec.Template.Spec.Volumes = generatedDeployment.Spec.Template.Spec.Volumes
			updated = true
		}

		// We do not want to permit direct edits of the Deployment environment. Any user-supplied values should be set
		// in the DataPlane. If the actual Deployment.Pods.Environment does not match the generated environment, either
		// something requires an update or there was a manual edit we want to purge.
		container := k8sutils.GetPodContainerByName(&existingDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		if container == nil {
			// someone has deleted the main container from the Deployment for ??? reasons. we can't fathom why they
			// would do this, but don't allow it and replace the container set entirely
			existingDeployment.Spec.Template.Spec.Containers = generatedDeployment.Spec.Template.Spec.Containers
			updated = true
			container = k8sutils.GetPodContainerByName(&existingDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		}
		if !reflect.DeepEqual(container.Env, dataplane.Spec.Deployment.Pods.Env) {
			trace(log, "DataPlane Deployment.Pods.Env needs updating", dataplane)
			container.Env = dataplane.Spec.Deployment.Pods.Env
			updated = true
		}

		if !reflect.DeepEqual(container.EnvFrom, dataplane.Spec.Deployment.Pods.EnvFrom) {
			trace(log, "DataPlane Deployment.Pods.EnvFrom needs updating", dataplane)
			container.EnvFrom = dataplane.Spec.Deployment.Pods.EnvFrom
			updated = true
		}

		// check for volume mounts.
		generatedContainer := k8sutils.GetPodContainerByName(&generatedDeployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		if containerVolumeMountNeedsUpdate(generatedContainer, container) {
			container.VolumeMounts = generatedContainer.VolumeMounts
			updated = true
		}

		if dataplane.Spec.Deployment.Pods.Affinity != nil {
			// DataPlane pod affinity is set.
			// Check if existing deployment already has its affinity set per DataPlane spec.
			dataPlaneAffinity := dataplane.Spec.Deployment.Pods.Affinity
			if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.Affinity, dataPlaneAffinity) {
				trace(log, "DataPlane deployment Affinity needs to be set as per DataPlane spec",
					dataplane, "dataplane.affinity", dataPlaneAffinity,
				)
				existingDeployment.Spec.Template.Spec.Affinity = dataPlaneAffinity
				updated = true
			}
		} else {
			if existingDeployment.Spec.Template.Spec.Affinity != nil {
				trace(log, "DataPlane deployment Affinity needs to be unset",
					dataplane, "dataplane.affinity", nil,
				)
				existingDeployment.Spec.Template.Spec.Affinity = nil
				updated = true
			}
		}

		if dataplane.Spec.Deployment.Pods.Resources != nil {
			// DataPlane deployment resources are set.
			// Check if existing container already has its resources set per DataPlane spec.
			dataPlaneResources := dataplane.Spec.Deployment.Pods.Resources
			if !k8sresources.ResourceRequirementsEqual(container.Resources, dataPlaneResources) {
				trace(log, "DataPlane deployment Resources needs to be set as per DataPlane spec",
					dataplane, "dataplane.resources", dataPlaneResources,
				)
				container.Resources = *dataPlaneResources
				updated = true
			}
		} else {
			// DataPlane deployment resources are unset.
			// Check if existing container already has defaults set.
			defaults := k8sresources.DefaultDataPlaneResources()
			if !k8sresources.ResourceRequirementsEqual(container.Resources, defaults) {
				trace(log, "DataPlane deployment Resources need to be set to defaults", dataplane)
				container.Resources = *defaults
				updated = true
			}
		}

		if !reflect.DeepEqual(existingDeployment.Spec.Strategy, generatedDeployment.Spec.Strategy) {
			existingDeployment.Spec.Strategy = generatedDeployment.Spec.Strategy
			updated = true
		}

		if !reflect.DeepEqual(existingDeployment.Spec.Replicas, generatedDeployment.Spec.Replicas) {
			existingDeployment.Spec.Replicas = generatedDeployment.Spec.Replicas
			updated = true
		}

		if !reflect.DeepEqual(existingDeployment.Spec.Template.Labels, generatedDeployment.Spec.Template.Labels) {
			existingDeployment.Spec.Template.Labels = generatedDeployment.Spec.Template.Labels
			updated = true
		}

		// update the container image or image version if needed
		imageUpdated, err := ensureContainerImageUpdated(container, dataplane.Spec.Deployment.Pods.ContainerImage, dataplane.Spec.Deployment.Pods.Version)
		if err != nil {
			return Noop, nil, err
		}
		if imageUpdated {
			updated = true
		}

		if updated {
			return Updated, existingDeployment, r.Client.Update(ctx, existingDeployment)
		}
		return Noop, existingDeployment, nil
	}

	return Created, generatedDeployment, r.Client.Create(ctx, generatedDeployment)
}

func (r *DataPlaneReconciler) deploymentSpecVolumesNeedsUpdate(
	existingDeploymentSpec *appsv1.DeploymentSpec,
	generatedDeploymentSpec *appsv1.DeploymentSpec,
) bool {
	generatedClusterCertVolume := k8sutils.GetPodVolumeByName(&generatedDeploymentSpec.Template.Spec, consts.ClusterCertificateVolume)
	existingClusterCertVolume := k8sutils.GetPodVolumeByName(&existingDeploymentSpec.Template.Spec, consts.ClusterCertificateVolume)
	// check for cluster certificate volume.
	if generatedClusterCertVolume == nil || existingClusterCertVolume == nil {
		return true
	}

	if generatedClusterCertVolume.Secret == nil || existingClusterCertVolume.Secret == nil {
		return true
	}

	if generatedClusterCertVolume.Secret.SecretName != existingClusterCertVolume.Secret.SecretName {
		return true
	}

	// check for other volumes.
	for _, generatedVolume := range generatedDeploymentSpec.Template.Spec.Volumes {
		// skip already checked cluster-certificate volume.
		if generatedVolume.Name == consts.ClusterCertificateVolume {
			continue
		}
		existingVolume := k8sutils.GetPodVolumeByName(&existingDeploymentSpec.Template.Spec, generatedVolume.Name)
		if existingVolume == nil {
			return true
		}
		if !k8sutils.HasSameVolumeSource(&generatedVolume.VolumeSource, &existingVolume.VolumeSource) {
			return true
		}
	}

	return false
}

func (r *DataPlaneReconciler) ensureProxyServiceForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (createdOrUpdated bool, svc *corev1.Service, err error) {
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
		return false, nil, err
	}

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, r.Client, services); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of dataplane proxy services reduced")
	}

	generatedService := k8sresources.GenerateNewProxyServiceForDataplane(dataplane)
	addLabelForDataplane(generatedService)
	addAnnotationsForDataplaneProxyService(generatedService, *dataplane)
	k8sutils.SetOwnerForObject(generatedService, dataplane)

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta,
			// enforce all the annotations provided through the dataplane API
			func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta) {
				var metaToUpdate bool
				if existingMeta.Annotations == nil && generatedMeta.Annotations != nil {
					existingMeta.Annotations = map[string]string{}
				}
				for k, v := range generatedMeta.Annotations {
					if existingMeta.Annotations[k] != v {
						existingMeta.Annotations[k] = v
						metaToUpdate = true
					}
				}
				return metaToUpdate, existingMeta
			})

		if existingService.Spec.Type != generatedService.Spec.Type {
			existingService.Spec.Type = generatedService.Spec.Type
			updated = true
		}

		if updated {
			return true, existingService, r.Client.Update(ctx, existingService)
		}
		return false, existingService, nil
	}

	return true, generatedService, r.Client.Create(ctx, generatedService)
}

func (r *DataPlaneReconciler) ensureAdminServiceForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (createdOrUpdated bool, svc *corev1.Service, err error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneAdminServiceLabelValue),
		},
	)
	if err != nil {
		return false, nil, err
	}

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, r.Client, services); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of services reduced")
	}

	generatedService := k8sresources.GenerateNewAdminServiceForDataPlane(dataplane)
	addLabelForDataplane(generatedService)
	k8sutils.SetOwnerForObject(generatedService, dataplane)

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta)
		if updated {
			return true, existingService, r.Client.Update(ctx, existingService)
		}
		return false, existingService, nil
	}

	return true, generatedService, r.Client.Create(ctx, generatedService)
}

func containerVolumeMountNeedsUpdate(
	generatedContainer *corev1.Container,
	existingContainer *corev1.Container,
) bool {
	for _, generatedVolumeMount := range generatedContainer.VolumeMounts {
		existingVolumeMount := k8sutils.GetContainerVolumeMountByMountPath(existingContainer, generatedVolumeMount.MountPath)
		if existingVolumeMount == nil {
			return true
		}
		if !(generatedVolumeMount.Name == existingVolumeMount.Name) ||
			!(generatedVolumeMount.ReadOnly == existingVolumeMount.ReadOnly) ||
			!(generatedVolumeMount.SubPath == existingVolumeMount.SubPath) {
			return true
		}
	}

	return false
}
