package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/versions"
)

// ensureDataPlaneCertificate ensures that a certificate exists for the given dataplane.
// Said certificate is used to secure the Admin API.
func ensureDataPlaneCertificate(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
	clusterCASecretNN types.NamespacedName,
	adminServiceNN types.NamespacedName,
) (CreatedUpdatedOrNoop, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature, certificatesv1.UsageServerAuth,
	}
	return maybeCreateCertificateSecret(ctx,
		dataplane,
		fmt.Sprintf("*.%s.%s.svc", adminServiceNN.Name, adminServiceNN.Namespace),
		clusterCASecretNN,
		usages,
		cl,
		getManagedLabelForServiceSecret(adminServiceNN),
	)
}

func ensureDeploymentForDataPlane(
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	developmentMode bool,
	dataplane *operatorv1beta1.DataPlane,
	certSecretName string,
	additionalDeploymentLabels client.MatchingLabels,
	opts ...k8sresources.DeploymentOpt,
) (res CreatedUpdatedOrNoop, deploy *appsv1.Deployment, err error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Deployments with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Deployments using the older labels.

	// Get the Deploments for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	for k, v := range additionalDeploymentLabels {
		matchingLabels[k] = v
	}

	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Deployments for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	// Get the Deploments for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataplane)
	if err != nil {
		return Noop, nil, err
	}
	deploymentsLegacy, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Deployments for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	deployments = append(deployments, deploymentsLegacy...)

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, cl, deployments, DataPlaneOwnedObjectPreDeleteHook); err != nil {
			return Noop, nil, err
		}
		return Updated, nil, errors.New("number of deployments reduced")
	}

	if len(additionalDeploymentLabels) > 0 {
		opts = append(opts, matchingLabelsToDeploymentOpt(additionalDeploymentLabels))
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !developmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsDataPlaneImageVersionSupported)
	}
	dataplaneImage, err := generateDataPlaneImage(dataplane, versionValidationOptions...)
	if err != nil {
		return Noop, nil, err
	}

	generatedDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataplane, dataplaneImage, certSecretName, opts...)
	if err != nil {
		return Noop, nil, err
	}

	if count == 1 {
		var updated bool
		existingDeployment := &deployments[0]
		oldExistingDeployment := existingDeployment.DeepCopy()

		// ensure that object metadata is up to date
		updated, existingDeployment.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingDeployment.ObjectMeta, generatedDeployment.ObjectMeta)

		// some custom comparison rules are needed for some PodTemplateSpec sub-attributes, in particular
		// resources and affinity.
		opts := []cmp.Option{
			cmp.Comparer(func(a, b corev1.ResourceRequirements) bool { return k8sresources.ResourceRequirementsEqual(a, b) }),
		}

		// ensure that PodTemplateSpec is up to date
		// TODO: this is currently relying on us pre-empting API server defaults (by setting them ourselves).
		// This could in theory lead to situations of incompatibility with newer Kubernetes versions down the
		// road, the tradeoff was made due to a time crunch of a matter of hours. We should consider other options
		// to verify whether there are changes staged.
		//
		// See: https://github.com/Kong/gateway-operator/issues/904
		if !cmp.Equal(existingDeployment.Spec.Template, generatedDeployment.Spec.Template, opts...) {
			existingDeployment.Spec.Template = generatedDeployment.Spec.Template
			updated = true
		}

		// ensure that rollout strategy is up to date
		if !cmp.Equal(existingDeployment.Spec.Strategy, generatedDeployment.Spec.Strategy) {
			existingDeployment.Spec.Strategy = generatedDeployment.Spec.Strategy
			updated = true
		}

		// ensure that replication strategy is up to date
		if !cmp.Equal(existingDeployment.Spec.Replicas, generatedDeployment.Spec.Replicas) {
			existingDeployment.Spec.Replicas = generatedDeployment.Spec.Replicas
			updated = true
		}

		if updated {
			if err := cl.Patch(ctx, existingDeployment, client.MergeFrom(oldExistingDeployment)); err != nil {
				return Noop, existingDeployment, fmt.Errorf("failed patching DataPlane Deployment %s: %w", existingDeployment.Name, err)
			}
			debug(log, "deployment modified", dataplane, "deployment", existingDeployment.Name)
			return Updated, existingDeployment, nil
		}

		trace(log, "no need for deployment update", dataplane, "deployment", existingDeployment.Name)
		return Noop, existingDeployment, nil
	}

	if err = cl.Create(ctx, generatedDeployment); err != nil {
		return Noop, nil, fmt.Errorf("failed creating Deployment for DataPlane %s: %w", dataplane.Name, err)
	}
	debug(log, "deployment created", dataplane, "deployment", generatedDeployment.Name)

	return Created, generatedDeployment, nil
}

func matchingLabelsToServiceOpt(ml client.MatchingLabels) k8sresources.ServiceOpt {
	return func(s *corev1.Service) {
		if s.Labels == nil {
			s.Labels = make(map[string]string)
		}
		for k, v := range ml {
			s.Labels[k] = v
		}
	}
}

func matchingLabelsToDeploymentOpt(ml client.MatchingLabels) k8sresources.DeploymentOpt {
	return func(a *appsv1.Deployment) {
		if a.Labels == nil {
			a.Labels = make(map[string]string)
		}
		for k, v := range ml {
			a.Labels[k] = v
		}
	}
}

func matchingLabelsToSecretOpt(ml client.MatchingLabels) k8sresources.SecretOpt {
	return func(a *corev1.Secret) {
		if a.Labels == nil {
			a.Labels = make(map[string]string)
		}
		for k, v := range ml {
			a.Labels[k] = v
		}
	}
}

func ensureAdminServiceForDataPlane(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
	additionalServiceLabels client.MatchingLabels,
	opts ...k8sresources.ServiceOpt,
) (res CreatedUpdatedOrNoop, svc *corev1.Service, err error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Services with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Services using the older labels.

	// Get the Services for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	matchingLabels[consts.DataPlaneServiceTypeLabel] = string(consts.DataPlaneAdminServiceLabelValue)
	for k, v := range additionalServiceLabels {
		matchingLabels[k] = v
	}

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	// Get the Services for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataplane)
	if err != nil {
		return Noop, nil, err
	}
	reqLegacyServiceType, err := labels.NewRequirement(
		consts.DataPlaneServiceTypeLabelLegacy, selection.Equals, []string{string(consts.DataPlaneAdminServiceLabelValue)},
	)
	if err != nil {
		return Noop, nil, err
	}
	servicesLegacy, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(*reqLegacyServiceType).Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	services = append(services, servicesLegacy...)

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, cl, services, DataPlaneOwnedObjectPreDeleteHook); err != nil {
			return Noop, nil, err
		}
		return Noop, nil, errors.New("number of DataPlane Admin API services reduced")
	}

	if len(additionalServiceLabels) > 0 {
		opts = append(opts, matchingLabelsToServiceOpt(additionalServiceLabels))
	}

	generatedService, err := k8sresources.GenerateNewAdminServiceForDataPlane(dataplane, opts...)
	if err != nil {
		return Noop, nil, err
	}

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta)

		if existingService.Spec.Type != generatedService.Spec.Type {
			existingService.Spec.Type = generatedService.Spec.Type
			updated = true
		}
		if !cmp.Equal(existingService.Spec.Selector, generatedService.Spec.Selector) {
			existingService.Spec.Selector = generatedService.Spec.Selector
			updated = true
		}
		if !cmp.Equal(existingService.Labels, generatedService.Labels) {
			existingService.Labels = generatedService.Labels
			updated = true
		}

		if updated {
			if err := cl.Update(ctx, existingService); err != nil {
				return Noop, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return Updated, existingService, nil
		}
		return Noop, existingService, nil
	}

	if err = cl.Create(ctx, generatedService); err != nil {
		return Noop, nil, fmt.Errorf("failed creating Admin API Service for DataPlane %s: %w", dataplane.Name, err)
	}

	return Created, generatedService, nil
}

// ensureIngressServiceForDataPlane ensures ingress service with metadata and spec
// generated from the dataplane.
func ensureIngressServiceForDataPlane(
	ctx context.Context,
	log logr.Logger,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
	additionalServiceLabels client.MatchingLabels,
	opts ...k8sresources.ServiceOpt,
) (CreatedUpdatedOrNoop, *corev1.Service, error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Services with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Services using the older labels.

	// Get the Services for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	matchingLabels[consts.DataPlaneServiceTypeLabel] = string(consts.DataPlaneIngressServiceLabelValue)
	for k, v := range additionalServiceLabels {
		matchingLabels[k] = v
	}

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	// Get the Services for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataplane)
	if err != nil {
		return Noop, nil, err
	}
	reqLegacyServiceType, err := labels.NewRequirement(
		consts.DataPlaneServiceTypeLabelLegacy, selection.Equals, []string{string(consts.DataPlaneProxyServiceLabelValueLegacy)},
	)
	if err != nil {
		return Noop, nil, err
	}
	servicesLegacy, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(*reqLegacyServiceType).Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	services = append(services, servicesLegacy...)

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, cl, services, DataPlaneOwnedObjectPreDeleteHook); err != nil {
			return Noop, nil, err
		}
		return Noop, nil, errors.New("number of DataPlane ingress services reduced")
	}

	if len(additionalServiceLabels) > 0 {
		opts = append(opts, matchingLabelsToServiceOpt(additionalServiceLabels))
	}

	generatedService, err := k8sresources.GenerateNewIngressServiceForDataplane(dataplane, opts...)
	if err != nil {
		return Noop, nil, err
	}
	addAnnotationsForDataplaneIngressService(generatedService, *dataplane)
	k8sutils.SetOwnerForObject(generatedService, dataplane)

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta,
			// enforce all the annotations provided through the dataplane API
			func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta) {
				metaToUpdate, updatedAnnotations, err := ensureDataPlaneIngressServiceAnnotationsUpdated(
					dataplane, existingMeta.Annotations, generatedMeta.Annotations,
				)
				if err != nil {
					log.Error(err, "failed to update annotations of existing ingress service for dataplane",
						"dataplane", fmt.Sprintf("%s/%s", dataplane.Namespace, dataplane.Name),
						"ingress_service", fmt.Sprintf("%s/%s", existingService.Namespace, existingService.Name))
					return true, existingMeta
				}
				existingMeta.Annotations = updatedAnnotations
				return metaToUpdate, existingMeta
			})

		if existingService.Spec.Type != generatedService.Spec.Type {
			existingService.Spec.Type = generatedService.Spec.Type
			updated = true
		}
		if !cmp.Equal(existingService.Spec.Selector, generatedService.Spec.Selector) {
			existingService.Spec.Selector = generatedService.Spec.Selector
			updated = true
		}

		if updated {
			if err := cl.Update(ctx, existingService); err != nil {
				return Noop, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return Updated, existingService, nil
		}
		return Noop, existingService, nil
	}

	return Created, generatedService, cl.Create(ctx, generatedService)
}

// DataPlaneOwnedObjectPreDeleteHook is a pre-delete hook for DataPlane-owned objects that ensures that before the
// operator attempts to delete the object, it removes the finalizer that prevents the object from being deleted
// accidentally by users.
func DataPlaneOwnedObjectPreDeleteHook(ctx context.Context, cl client.Client, obj client.Object) error {
	finalizers := obj.GetFinalizers()

	// If there's no finalizer, we don't need to do anything.
	if !lo.Contains(finalizers, consts.DataPlaneOwnedWaitForOwnerFinalizer) {
		return nil
	}

	// Otherwise, we delete the finalizer and update the object.
	obj.SetFinalizers(lo.Reject(finalizers, func(s string, _ int) bool {
		return s == consts.DataPlaneOwnedWaitForOwnerFinalizer
	}))
	if err := cl.Update(ctx, obj); err != nil {
		return fmt.Errorf("failed to remove %q finalizer before deletion: %w", consts.DataPlaneOwnedWaitForOwnerFinalizer, err)
	}
	return nil
}
