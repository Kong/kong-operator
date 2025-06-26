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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/dataplane"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// ensureDataPlaneCertificate ensures that a certificate exists for the given dataplane.
// Said certificate is used to secure the Admin API.
func ensureDataPlaneCertificate(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
	clusterCASecretNN types.NamespacedName,
	adminServiceNN types.NamespacedName,
	keyConfig secrets.KeyConfig,
) (op.Result, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature, certificatesv1.UsageServerAuth,
	}
	return secrets.EnsureCertificate(ctx,
		dataplane,
		fmt.Sprintf("*.%s.%s.svc", adminServiceNN.Name, adminServiceNN.Namespace),
		clusterCASecretNN,
		usages,
		keyConfig,
		cl,
		secrets.GetManagedLabelForServiceSecret(adminServiceNN),
	)
}

func ensureHPAForDataPlane(
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	deploymentName string,
) (res op.Result, hpa *autoscalingv2.HorizontalPodAutoscaler, err error) {
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

	if scaling := dataplane.Spec.Deployment.Scaling; scaling == nil || scaling.HorizontalScaling == nil {
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

		return patch.ApplyPatchIfNotEmpty(ctx, cl, log, existingHPA, oldExistingHPA, updated)
	}

	if err = cl.Create(ctx, generatedHPA); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating HPA for DataPlane %s: %w", dataplane.Name, err)
	}

	return op.Created, nil, nil
}

func ensurePodDisruptionBudgetForDataPlane(
	ctx context.Context,
	cl client.Client,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
) (res op.Result, pdb *policyv1.PodDisruptionBudget, err error) {
	dpNn := client.ObjectKeyFromObject(dataplane)
	matchingLabels := k8sresources.GetManagedLabelForOwner(dataplane)
	pdbs, err := k8sutils.ListPodDisruptionBudgetsForOwner(ctx, cl, dataplane.Namespace, dataplane.UID, matchingLabels)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing PodDisruptionBudgets for DataPlane %s: %w", dpNn, err)
	}

	if dataplane.Spec.Resources.PodDisruptionBudget == nil {
		if err := k8sreduce.ReducePodDisruptionBudgets(ctx, cl, pdbs, k8sreduce.FilterNone); err != nil {
			return op.Noop, nil, fmt.Errorf("failed reducing PodDisruptionBudgets for DataPlane %s: %w", dpNn, err)
		}
		return op.Noop, nil, nil
	}

	if len(pdbs) > 1 {
		if err := k8sreduce.ReducePodDisruptionBudgets(ctx, cl, pdbs, k8sreduce.FilterPodDisruptionBudgets); err != nil {
			return op.Noop, nil, fmt.Errorf("failed reducing PodDisruptionBudgets for DataPlane %s: %w", dpNn, err)
		}
		return op.Noop, nil, nil
	}

	generatedPDB, err := k8sresources.GeneratePodDisruptionBudgetForDataPlane(dataplane)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed generating PodDisruptionBudget for DataPlane %s: %w", dpNn, err)
	}

	if len(pdbs) == 1 {
		var updated bool
		existingPDB := &pdbs[0]
		oldExistingPDB := existingPDB.DeepCopy()

		// Ensure that PDB's metadata is up-to-date.
		updated, existingPDB.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingPDB.ObjectMeta, generatedPDB.ObjectMeta)

		// Ensure that PDB's spec is up-to-date.
		if !cmp.Equal(existingPDB.Spec, generatedPDB.Spec) {
			existingPDB.Spec = generatedPDB.Spec
			updated = true
		}

		return patch.ApplyPatchIfNotEmpty(ctx, cl, log, existingPDB, oldExistingPDB, updated)
	}

	if err := cl.Create(ctx, generatedPDB); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating PodDisruptionBudget for DataPlane %s: %w", dpNn, err)
	}

	return op.Created, generatedPDB, nil
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
) (res op.Result, svc *corev1.Service, err error) {
	// Get the Services for the DataPlane by label.
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
) (op.Result, *corev1.Service, error) {
	// Get the Services for the DataPlane by label.
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

	count := len(services)
	if serviceName := k8sresources.GetDataPlaneIngressServiceName(dataPlane); serviceName != "" {
		if count > 1 || (count == 1 && services[0].Name != serviceName) {
			if err := k8sreduce.ReduceServicesByName(ctx, cl, services, serviceName, dataplane.OwnedObjectPreDeleteHook); err != nil {
				return op.Noop, nil, err
			}
			return op.Noop, nil, errors.New("DataPlane ingress services with different names reduced")
		}
	} else if count > 1 {
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
		old := existingService.DeepCopy()
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

		const (
			defaultExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
		)

		// Update when
		// - the existing service does not have the default value for ExternalTrafficPolicy
		// - or the generated service has the a different than default value for ExternalTrafficPolicy or is non empty.
		if existingService.Spec.ExternalTrafficPolicy != defaultExternalTrafficPolicy || (generatedService.Spec.ExternalTrafficPolicy != "" && generatedService.Spec.ExternalTrafficPolicy != defaultExternalTrafficPolicy) {
			existingService.Spec.ExternalTrafficPolicy = generatedService.Spec.ExternalTrafficPolicy
			updated = true
		}

		if !cmp.Equal(existingService.Spec.Selector, generatedService.Spec.Selector) {
			existingService.Spec.Selector = generatedService.Spec.Selector
			updated = true
		}
		if !comparePorts(existingService.Spec.Ports, generatedService.Spec.Ports, dataPlane) {
			existingService.Spec.Ports = generatedService.Spec.Ports
			updated = true
		}

		if updated {
			res, existingService, err := patch.ApplyPatchIfNotEmpty(ctx, cl, logger, existingService, old, updated)
			if err != nil {
				return op.Noop, existingService, fmt.Errorf("failed updating DataPlane Service %s: %w", existingService.Name, err)
			}
			return res, existingService, nil
		}
		return op.Noop, existingService, nil
	}

	return op.Created, generatedService, cl.Create(ctx, generatedService)
}

// comparePorts compares the ports of two services.
// It returns true if the ports are equal, ignoring the NodePort field
// for the ingress service if it was not set in the dataplane spec.
func comparePorts(
	a, b []corev1.ServicePort,
	dataPlane *operatorv1beta1.DataPlane,
) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
		if a[i].Protocol != b[i].Protocol {
			return false
		}
		if a[i].Port != b[i].Port {
			return false
		}
		if a[i].TargetPort != b[i].TargetPort {
			return false
		}
		if a[i].AppProtocol != b[i].AppProtocol {
			return false
		}

		if a[i].NodePort != b[i].NodePort {
			if dataPlane != nil &&
				dataPlane.Spec.Network.Services != nil &&
				dataPlane.Spec.Network.Services.Ingress != nil &&
				len(dataPlane.Spec.Network.Services.Ingress.Ports) > i &&
				dataPlane.Spec.Network.Services.Ingress.Ports[i].NodePort != 0 {
				return false
			}
		}

	}
	return true
}
