package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// ensureOldOwnedResourcesDeleted ensures that all old (created by KGO <= 1.4) resources owned by the ControlPlane that
// are no longer needed are deleted.
func (r *Reconciler) ensureOldOwnedResourcesDeleted(ctx context.Context, logger logr.Logger, cp *operatorv1beta1.ControlPlane) error {
	resourceCleanups := []struct {
		resourceName    string
		finalizerName   mo.Option[string]
		ensureDeletedFn func(context.Context, *operatorv1beta1.ControlPlane) (bool, error)
	}{
		{
			resourceName:    "ValidatingWebhookConfiguration",
			finalizerName:   mo.Some(string(ControlPlaneFinalizerCleanupValidatingWebhookConfiguration)),
			ensureDeletedFn: r.ensureOwnedValidatingWebhookConfigurationDeleted,
		},
		{
			resourceName:    "ClusterRole",
			finalizerName:   mo.Some(string(ControlPlaneFinalizerCleanupClusterRole)),
			ensureDeletedFn: r.ensureOwnedClusterRolesDeleted,
		},
		{
			resourceName:    "ClusterRoleBinding",
			finalizerName:   mo.Some(string(ControlPlaneFinalizerCleanupClusterRoleBinding)),
			ensureDeletedFn: r.ensureOwnedClusterRoleBindingsDeleted,
		},
		{
			resourceName:    "Secret",
			ensureDeletedFn: r.ensureOwnedSecretsDeleted,
		},
		{
			resourceName:    "ServiceAccount",
			ensureDeletedFn: r.ensureOwnedServiceAccountsDeleted,
		},
		{
			resourceName:    "Deployment",
			ensureDeletedFn: r.ensureOwnedDeploymentsDeleted,
		},
		{
			resourceName:    "Service",
			ensureDeletedFn: r.ensureOwnedServicesDeleted,
		},
	}

	newControlPlane := cp.DeepCopy()
	for _, cleanup := range resourceCleanups {
		deletions, err := cleanup.ensureDeletedFn(ctx, cp)
		if err != nil {
			return fmt.Errorf("failed to delete owned %s: %w", cleanup.resourceName, err)
		}
		if deletions {
			log.Debug(logger, "deleted old owned resources", "resource", cleanup.resourceName)
			return nil // Resource deletion will requeue.
		}
		if finalizerName, ok := cleanup.finalizerName.Get(); ok {
			if controllerutil.RemoveFinalizer(newControlPlane, finalizerName) {
				if err := r.Client.Patch(ctx, newControlPlane, client.MergeFrom(cp)); err != nil {
					return fmt.Errorf("failed to remove %s finalizer: %w", cleanup.resourceName, err)
				}
				log.Debug(logger, "removed finalizer", "finalizer", finalizerName)
				return nil // ControlPlane update will requeue.
			}
		}
	}

	return nil
}

func (r *Reconciler) ensureOwnedValidatingWebhookConfigurationDeleted(
	ctx context.Context,
	cp *operatorv1beta1.ControlPlane,
) (deletions bool, err error) {
	validatingWebhookConfigurations, err := k8sutils.ListValidatingWebhookConfigurations(
		ctx,
		r.Client,
		client.MatchingLabels(k8sutils.GetManagedByLabelSet(cp)),
	)
	if err != nil {
		return false, fmt.Errorf("failed listing webhook configurations for owner: %w", err)
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range validatingWebhookConfigurations {
		if err = r.Client.Delete(ctx, &validatingWebhookConfigurations[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedClusterRoleBindingsDeleted(
	ctx context.Context,
	cp *operatorv1beta1.ControlPlane,
) (deletions bool, err error) {
	clusterRoleBindings, err := k8sutils.ListClusterRoleBindings(
		ctx,
		r.Client,
		client.MatchingLabels(k8sutils.GetManagedByLabelSet(cp)),
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range clusterRoleBindings {
		if err = r.Client.Delete(ctx, &clusterRoleBindings[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedClusterRolesDeleted(
	ctx context.Context,
	cp *operatorv1beta1.ControlPlane,
) (deletions bool, err error) {
	clusterRoles, err := k8sutils.ListClusterRoles(
		ctx,
		r.Client,
		client.MatchingLabels(k8sutils.GetManagedByLabelSet(cp)),
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range clusterRoles {
		if err = r.Client.Delete(ctx, &clusterRoles[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedSecretsDeleted(ctx context.Context, cp *operatorv1beta1.ControlPlane) (deletions bool, err error) {
	secrets, err := k8sutils.ListSecretsForOwner(
		ctx,
		r.Client,
		cp.UID,
		client.HasLabels{consts.SecretUsedByServiceLabel},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range secrets {
		if err = r.Client.Delete(ctx, &secrets[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedServiceAccountsDeleted(ctx context.Context, cp *operatorv1beta1.ControlPlane) (bool, error) {
	serviceAccounts, err := k8sutils.ListServiceAccountsForOwner(
		ctx,
		r.Client,
		cp.GetNamespace(),
		cp.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range serviceAccounts {
		if err = r.Client.Delete(ctx, &serviceAccounts[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedDeploymentsDeleted(ctx context.Context, cp *operatorv1beta1.ControlPlane) (bool, error) {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		cp.GetNamespace(),
		cp.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range deployments {
		if err = r.Client.Delete(ctx, &deployments[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}

func (r *Reconciler) ensureOwnedServicesDeleted(ctx context.Context, cp *operatorv1beta1.ControlPlane) (bool, error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		cp.GetNamespace(),
		cp.UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.ControlPlaneManagedLabelValue,
		},
	)
	if err != nil {
		return false, err
	}

	var (
		deleted bool
		errs    []error
	)
	for i := range services {
		if err = r.Client.Delete(ctx, &services[i]); client.IgnoreNotFound(err) != nil {
			errs = append(errs, err)
			continue
		}
		deleted = true
	}

	return deleted, errors.Join(errs...)
}
