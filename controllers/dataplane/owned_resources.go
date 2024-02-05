package dataplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/dataplane"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	"github.com/kong/gateway-operator/controllers/pkg/patch"
	"github.com/kong/gateway-operator/controllers/pkg/secrets"
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
) (op.CreatedUpdatedOrNoop, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature, certificatesv1.UsageServerAuth,
	}
	return secrets.EnsureCertificate(ctx,
		dataplane,
		fmt.Sprintf("*.%s.%s.svc", adminServiceNN.Name, adminServiceNN.Namespace),
		clusterCASecretNN,
		usages,
		cl,
		secrets.GetManagedLabelForServiceSecret(adminServiceNN),
	)
}

func ensureDeploymentForDataPlane(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	developmentMode bool,
	dataPlane *operatorv1beta1.DataPlane,
	certSecretName string,
	additionalDeploymentLabels client.MatchingLabels,
	opts ...k8sresources.DeploymentOpt,
) (res op.CreatedUpdatedOrNoop, deploy *appsv1.Deployment, err error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Deployments with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Deployments using the older labels.

	// Get the Deploments for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataPlane)
	for k, v := range additionalDeploymentLabels {
		matchingLabels[k] = v
	}

	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Deployments for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}

	// Get the Deploments for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataPlane)
	if err != nil {
		return op.Noop, nil, err
	}
	deploymentsLegacy, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Deployments for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}
	deployments = append(deployments, deploymentsLegacy...)

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, cl, deployments, dataplane.OwnedObjectPreDeleteHook); err != nil {
			return op.Noop, nil, err
		}
		return op.Updated, nil, errors.New("number of deployments reduced")
	}

	if len(additionalDeploymentLabels) > 0 {
		opts = append(opts, matchingLabelsToDeploymentOpt(additionalDeploymentLabels))
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !developmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsDataPlaneImageVersionSupported)
	}
	dataplaneImage, err := generateDataPlaneImage(dataPlane, versionValidationOptions...)
	if err != nil {
		return op.Noop, nil, err
	}

	generatedDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName, opts...)
	if err != nil {
		return op.Noop, nil, err
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
		if !cmp.Equal(existingDeployment.Spec.Template, generatedDeployment.Spec.Template, opts...) {
			existingDeployment.Spec.Template = generatedDeployment.Spec.Template
			updated = true
		}

		// ensure that rollout strategy is up to date
		if !cmp.Equal(existingDeployment.Spec.Strategy, generatedDeployment.Spec.Strategy) {
			existingDeployment.Spec.Strategy = generatedDeployment.Spec.Strategy
			updated = true
		}

		if scaling := dataPlane.Spec.Deployment.DeploymentOptions.Scaling; false ||
			// If the scaling strategy is not specified, we compare the replicas.
			(scaling == nil || scaling.HorizontalScaling == nil) ||
			// If the scaling strategy is specified with minReplicas, we compare
			// the minReplicas with the existing Deployment replicas and we set
			// the replicas to the minReplicas if the existing Deployment replicas
			// are less than the minReplicas to enforce faster scaling before HPA
			// kicks in.
			(scaling != nil && scaling.HorizontalScaling != nil &&
				scaling.HorizontalScaling.MinReplicas != nil &&
				existingDeployment.Spec.Replicas != nil &&
				*existingDeployment.Spec.Replicas < *scaling.HorizontalScaling.MinReplicas) {
			if !cmp.Equal(existingDeployment.Spec.Replicas, generatedDeployment.Spec.Replicas) {
				existingDeployment.Spec.Replicas = generatedDeployment.Spec.Replicas
				updated = true
			}
		}

		return patch.ApplyPatchIfNonEmpty(ctx, cl, logger, existingDeployment, oldExistingDeployment, dataPlane, updated)
	}

	if err = cl.Create(ctx, generatedDeployment); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating Deployment for DataPlane %s: %w", dataPlane.Name, err)
	}

	log.Debug(logger, "deployment for DataPlane created", dataPlane, "deployment", generatedDeployment.Name)
	return op.Created, generatedDeployment, nil
}

func ensureHPAForDataPlane(
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	deploymentName string,
) (res op.CreatedUpdatedOrNoop, hpa *autoscalingv2.HorizontalPodAutoscaler, err error) {
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	hpas, err := k8sutils.ListHPAsForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing HPAs for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	if scaling := dataplane.Spec.Deployment.DeploymentOptions.Scaling; scaling == nil || scaling.HorizontalScaling == nil {
		if err := k8sreduce.ReduceHPAs(ctx, cl, hpas, k8sreduce.FilterNone); err != nil {
			return op.Noop, nil, fmt.Errorf("failed reducing HPAs for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return op.Noop, nil, nil
	}

	if len(hpas) > 1 {
		if err := k8sreduce.ReduceHPAs(ctx, cl, hpas, k8sreduce.FilterHPAs); err != nil {
			return op.Noop, nil, fmt.Errorf("failed reducing HPAs for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return op.Noop, nil, nil
	}

	generatedHPA, err := k8sresources.GenerateHPAForDataPlane(dataplane, deploymentName)
	if err != nil {
		return op.Noop, nil, err
	}

	if len(hpas) == 1 {
		var updated bool
		existingHPA := &hpas[0]
		oldExistingHPA := existingHPA.DeepCopy()

		// ensure that object metadata is up to date
		updated, existingHPA.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingHPA.ObjectMeta, generatedHPA.ObjectMeta)

		// ensure that rollout strategy is up to date
		if !cmp.Equal(existingHPA.Spec, generatedHPA.Spec) {
			existingHPA.Spec = generatedHPA.Spec
			updated = true
		}

		return patch.ApplyPatchIfNonEmpty(ctx, cl, log, existingHPA, oldExistingHPA, dataplane, updated)
	}

	if err = cl.Create(ctx, generatedHPA); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating HPA for DataPlane %s: %w", dataplane.Name, err)
	}

	return op.Created, nil, nil
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

func ensureAdminServiceForDataPlane(
	ctx context.Context,
	cl client.Client,
	dataPlane *operatorv1beta1.DataPlane,
	additionalServiceLabels client.MatchingLabels,
	opts ...k8sresources.ServiceOpt,
) (res op.CreatedUpdatedOrNoop, svc *corev1.Service, err error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Services with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Services using the older labels.

	// Get the Services for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataPlane)
	matchingLabels[consts.DataPlaneServiceTypeLabel] = string(consts.DataPlaneAdminServiceLabelValue)
	for k, v := range additionalServiceLabels {
		matchingLabels[k] = v
	}

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}

	// Get the Services for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataPlane)
	if err != nil {
		return op.Noop, nil, err
	}
	reqLegacyServiceType, err := labels.NewRequirement(
		consts.DataPlaneServiceTypeLabelLegacy, selection.Equals, []string{string(consts.DataPlaneAdminServiceLabelValue)},
	)
	if err != nil {
		return op.Noop, nil, err
	}
	servicesLegacy, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(*reqLegacyServiceType).Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}
	services = append(services, servicesLegacy...)

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, cl, services, dataplane.OwnedObjectPreDeleteHook); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of DataPlane Admin API services reduced")
	}

	if len(additionalServiceLabels) > 0 {
		opts = append(opts, matchingLabelsToServiceOpt(additionalServiceLabels))
	}

	generatedService, err := k8sresources.GenerateNewAdminServiceForDataPlane(dataPlane, opts...)
	if err != nil {
		return op.Noop, nil, err
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
				return op.Noop, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return op.Updated, existingService, nil
		}
		return op.Noop, existingService, nil
	}

	if err = cl.Create(ctx, generatedService); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating Admin API Service for DataPlane %s: %w", dataPlane.Name, err)
	}

	return op.Created, generatedService, nil
}

// ensureIngressServiceForDataPlane ensures ingress service with metadata and spec
// generated from the dataplane.
func ensureIngressServiceForDataPlane(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	dataPlane *operatorv1beta1.DataPlane,
	additionalServiceLabels client.MatchingLabels,
	opts ...k8sresources.ServiceOpt,
) (op.CreatedUpdatedOrNoop, *corev1.Service, error) {
	// TODO: https://github.com/Kong/gateway-operator/pull/1101.
	// Use only new labels after several minor version of soak time.

	// Below we list both the Services with the new labels and the legacy labels
	// in order to support upgrades from older versions of the operator and perform
	// the reduction of the Services using the older labels.

	// Get the Services for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataPlane)
	matchingLabels[consts.DataPlaneServiceTypeLabel] = string(consts.DataPlaneIngressServiceLabelValue)
	for k, v := range additionalServiceLabels {
		matchingLabels[k] = v
	}

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}

	// Get the Services for the DataPlane using legacy labels.
	reqLegacyLabels, err := k8sresources.GetManagedLabelRequirementsForOwnerLegacy(dataPlane)
	if err != nil {
		return op.Noop, nil, err
	}
	reqLegacyServiceType, err := labels.NewRequirement(
		consts.DataPlaneServiceTypeLabelLegacy, selection.Equals, []string{string(consts.DataPlaneProxyServiceLabelValueLegacy)},
	)
	if err != nil {
		return op.Noop, nil, err
	}
	servicesLegacy, err := k8sutils.ListServicesForOwner(
		ctx,
		cl,
		dataPlane.Namespace,
		dataPlane.UID,
		&client.ListOptions{
			LabelSelector: labels.NewSelector().Add(*reqLegacyServiceType).Add(reqLegacyLabels...),
		},
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Services for DataPlane %s/%s: %w", dataPlane.Namespace, dataPlane.Name, err)
	}
	services = append(services, servicesLegacy...)

	count := len(services)
	if count > 1 {
		if err := k8sreduce.ReduceServices(ctx, cl, services, dataplane.OwnedObjectPreDeleteHook); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of DataPlane ingress services reduced")
	}

	if len(additionalServiceLabels) > 0 {
		opts = append(opts, matchingLabelsToServiceOpt(additionalServiceLabels))
	}

	generatedService, err := k8sresources.GenerateNewIngressServiceForDataPlane(dataPlane, opts...)
	if err != nil {
		return op.Noop, nil, err
	}
	addAnnotationsForDataPlaneIngressService(generatedService, *dataPlane)
	k8sutils.SetOwnerForObject(generatedService, dataPlane)

	if count == 1 {
		var updated bool
		existingService := &services[0]
		updated, existingService.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingService.ObjectMeta, generatedService.ObjectMeta,
			// enforce all the annotations provided through the dataplane API
			func(existingMeta metav1.ObjectMeta, generatedMeta metav1.ObjectMeta) (bool, metav1.ObjectMeta) {
				metaToUpdate, updatedAnnotations, err := ensureDataPlaneIngressServiceAnnotationsUpdated(
					dataPlane, existingMeta.Annotations, generatedMeta.Annotations,
				)
				if err != nil {
					logger.Error(err, "failed to update annotations of existing ingress service for dataplane",
						"dataplane", fmt.Sprintf("%s/%s", dataPlane.Namespace, dataPlane.Name),
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
				return op.Noop, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return op.Updated, existingService, nil
		}
		return op.Noop, existingService, nil
	}

	return op.Created, generatedService, cl.Create(ctx, generatedService)
}
