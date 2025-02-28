package dataplane

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dataplanepkg "github.com/kong/gateway-operator/controller/pkg/dataplane"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/internal/utils/config"
	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// DeploymentBuilder builds a Deployment for a DataPlane.
type DeploymentBuilder struct {
	clusterCertificateName string
	beforeCallbacks        CallbackManager
	afterCallbacks         CallbackManager
	logger                 logr.Logger
	client                 client.Client
	additionalLabels       client.MatchingLabels
	defaultImage           string
	opts                   []k8sresources.DeploymentOpt
}

// NewDeploymentBuilder creates a DeploymentBuilder.
func NewDeploymentBuilder(logger logr.Logger, client client.Client) *DeploymentBuilder {
	d := &DeploymentBuilder{}
	d.logger = logger
	d.client = client
	return d
}

// WithBeforeCallbacks sets callbacks to run before initial Deployment generation.
func (d *DeploymentBuilder) WithBeforeCallbacks(c CallbackManager) *DeploymentBuilder {
	d.beforeCallbacks = c
	return d
}

// WithAfterCallbacks sets callbacks to run after initial Deployment generation.
func (d *DeploymentBuilder) WithAfterCallbacks(c CallbackManager) *DeploymentBuilder {
	d.afterCallbacks = c
	return d
}

// WithClusterCertificate configures a cluster certificate name for a DeploymentBuilder.
func (d *DeploymentBuilder) WithClusterCertificate(name string) *DeploymentBuilder {
	d.clusterCertificateName = name
	return d
}

// WithAdditionalLabels configures additional labels for a DeploymentBuilder.
func (d *DeploymentBuilder) WithAdditionalLabels(labels client.MatchingLabels) *DeploymentBuilder {
	d.additionalLabels = labels
	return d
}

// WithDefaultImage configures the default image.
func (d *DeploymentBuilder) WithDefaultImage(image string) *DeploymentBuilder {
	d.defaultImage = image
	return d
}

// WithOpts adds option functions to a DeploymentBuilder.
func (d *DeploymentBuilder) WithOpts(opts ...k8sresources.DeploymentOpt) *DeploymentBuilder {
	d.opts = opts
	return d
}

// BuildAndDeploy builds and deploys a DataPlane Deployment, or reduces Deployments if there are more than one. It
// returns the Deployment if it created or updated one, or nil if it needed to reduce or did not need to update an
// existing Deployment.
func (d *DeploymentBuilder) BuildAndDeploy(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
	developmentMode bool,
) (*appsv1.Deployment, op.Result, error) {
	// run any preparatory callbacks
	beforeDeploymentCallbacks := NewCallbackRunner(d.client)
	cbErrors := beforeDeploymentCallbacks.For(dataplane).Runs(d.beforeCallbacks).Do(ctx, nil)
	if len(cbErrors) > 0 {
		for _, err := range cbErrors {
			d.logger.Error(err, "callback failed")
		}
		return nil, op.Noop, fmt.Errorf("before generation callbacks failed")
	}

	// if there is more than one Deployment, delete the extras
	reduced, existingDeployment, err := listOrReduceDataPlaneDeployments(ctx, d.client, dataplane, d.additionalLabels)
	if err != nil {
		return nil, op.Noop, fmt.Errorf("failed listing existing Deployments: %w", err)
	}
	if reduced {
		return nil, op.Noop, nil
	}

	// generate the initial Deployment struct
	desiredDeployment, err := generateDataPlaneDeployment(developmentMode, dataplane, d.defaultImage, d.additionalLabels, d.opts...)
	if err != nil {
		return nil, op.Noop, fmt.Errorf("could not generate Deployment: %w", err)
	}

	// Add the cluster certificate to the generated Deployment
	desiredDeployment = setClusterCertVars(desiredDeployment, d.clusterCertificateName)

	// run any callbacks that patch the initial Deployment struct
	afterDeploymentCallbacks := NewCallbackRunner(d.client)
	cbErrors = afterDeploymentCallbacks.For(dataplane).Runs(d.afterCallbacks).
		Modifies(reflect.TypeFor[k8sresources.Deployment]()).Do(ctx, desiredDeployment)
	if len(cbErrors) > 0 {
		for _, err := range cbErrors {
			d.logger.Error(err, "callback failed")
		}
		return nil, op.Noop, fmt.Errorf("after generation callbacks failed")
	}

	// TODO https://github.com/Kong/gateway-operator/issues/128
	// This is a a workaround to avoid patches clobbering the wrong EnvVar. We want to find an improved patch mechanism
	// that doesn't clobber EnvVars (and other array fields) it shouldn't.
	existingEnvVars := desiredDeployment.Spec.Template.Spec.Containers[0].Env
	desiredDeployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{}
	// apply user patches and set any default environment variables that aren't already set
	desiredDeployment, err = applyDeploymentUserPatchesForDataPlane(dataplane, desiredDeployment)
	if err != nil {
		return nil, op.Noop, err
	}
	// apply default envvars and restore the hacked-out ones
	desiredDeployment = applyEnvForDataPlane(existingEnvVars, desiredDeployment, config.KongDefaults)

	// push the complete Deployment to Kubernetes
	res, deployment, err := reconcileDataPlaneDeployment(ctx, d.client, d.logger,
		dataplane, existingDeployment, desiredDeployment.Unwrap())
	if err != nil {
		return nil, op.Noop, err
	}
	return deployment, res, nil
}

// generateDataPlaneDeployment generates the base Deployment for a DataPlane. It determines the image to use and
// generates an opt transform function to add additional labels before invoking the generator utility.
func generateDataPlaneDeployment(
	developmentMode bool,
	dataplane *operatorv1beta1.DataPlane,
	defaultImage string,
	additionalDeploymentLabels client.MatchingLabels,
	opts ...k8sresources.DeploymentOpt,
) (deployment *k8sresources.Deployment, err error) {
	if len(additionalDeploymentLabels) > 0 {
		opts = append(opts, matchingLabelsToDeploymentOpt(additionalDeploymentLabels))
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if !developmentMode {
		versionValidationOptions = append(versionValidationOptions, versions.IsDataPlaneImageVersionSupported)
	}
	dataplaneImage, err := generateDataPlaneImage(dataplane, defaultImage, versionValidationOptions...)
	if err != nil {
		return nil, err
	}

	generatedDeployment, err := k8sresources.GenerateNewDeploymentForDataPlane(dataplane, dataplaneImage, opts...)
	if err != nil {
		return nil, err
	}
	return generatedDeployment, nil
}

// applyDeploymentUserPatchesForDataPlane applies user PodTemplateSpec patches and fills in defaults
// for any previously unset environment variables.
func applyDeploymentUserPatchesForDataPlane(
	dataplane *operatorv1beta1.DataPlane,
	deployment *k8sresources.Deployment,
) (*k8sresources.Deployment, error) {
	var err error
	deployment, err = k8sresources.ApplyDeploymentUserPatches(deployment, dataplane.Spec.Deployment.PodTemplateSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to apply deployment user patches: %w", err)
	}
	return deployment, nil
}

// applyEnvForDataPlane applies user PodTemplateSpec patches and fills in defaults
// for any previously unset environment variables.
func applyEnvForDataPlane(
	existing []corev1.EnvVar,
	deployment *k8sresources.Deployment,
	envSet map[string]string,
) *k8sresources.Deployment {
	config.FillContainerEnvMap(existing, &deployment.Spec.Template, consts.DataPlaneProxyContainerName, envSet)
	return deployment
}

// setClusterCertVars configures a cluster certificate in the proxy environment.
func setClusterCertVars(
	deployment *k8sresources.Deployment,
	secretName string,
) *k8sresources.Deployment {
	return deployment.WithVolume(k8sresources.ClusterCertificateVolume(secretName)).
		WithVolumeMount(k8sresources.ClusterCertificateVolumeMount(), consts.DataPlaneProxyContainerName).
		WithEnvVar(
			corev1.EnvVar{
				Name:  "KONG_CLUSTER_CERT",
				Value: filepath.Join(consts.ClusterCertificateVolumeMountPath, "tls.crt"),
			}, consts.DataPlaneProxyContainerName,
		).
		WithEnvVar(
			corev1.EnvVar{
				Name:  "KONG_CLUSTER_CERT_KEY",
				Value: filepath.Join(consts.ClusterCertificateVolumeMountPath, "tls.key"),
			}, consts.DataPlaneProxyContainerName,
		)
}

// listOrReduceDataPlaneDeployments lists existing DataPlane Deployments. If only one is present, it returns it. If
// multiple are present, it reduces them to one and notifies the caller it reduced, so that the caller can try its
// operation again once there's only a single Deployment to work with.
func listOrReduceDataPlaneDeployments(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
	additionalDeploymentLabels client.MatchingLabels,
) (reduced bool, deployment *appsv1.Deployment, err error) {
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
		return false, nil, fmt.Errorf("failed listing Deployments for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	count := len(deployments)
	if count > 1 {
		if err := k8sreduce.ReduceDeployments(ctx, cl, deployments, dataplanepkg.OwnedObjectPreDeleteHook); err != nil {
			return false, nil, err
		}
		return true, nil, errors.New("number of deployments reduced")
	}
	if count == 0 {
		return false, nil, nil
	}

	return false, &deployments[0], nil
}

// reconcileDataPlaneDeployment takes any existing DataPlane Deployment and a desired DataPlane Deployment and
// reconciles the existing state to the desired state by either updating an existing Deployment, creating a new one,
// or doing nothing.
func reconcileDataPlaneDeployment(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	existing *appsv1.Deployment,
	desired *appsv1.Deployment,
) (res op.Result, deploy *appsv1.Deployment, err error) {
	if existing != nil {
		var updated bool
		original := existing.DeepCopy()

		k8sresources.SetDefaultsPodTemplateSpec(&desired.Spec.Template)

		// ensure that object metadata is up to date
		updated, existing.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existing.ObjectMeta, desired.ObjectMeta)

		// some custom comparison rules are needed for some PodTemplateSpec sub-attributes, in particular
		// resources and affinity.
		opts := []cmp.Option{
			cmp.Comparer(k8sresources.ResourceRequirementsEqual),
		}

		// ensure that PodTemplateSpec is up to date
		if !cmp.Equal(existing.Spec.Template, desired.Spec.Template, opts...) {
			existing.Spec.Template = desired.Spec.Template
			updated = true
		}

		// ensure that rollout strategy is up to date
		if !cmp.Equal(existing.Spec.Strategy, desired.Spec.Strategy) {
			existing.Spec.Strategy = desired.Spec.Strategy
			updated = true
		}

		if scaling := dataplane.Spec.Deployment.DeploymentOptions.Scaling; false ||
			// If the scaling strategy is not specified, we compare the replicas.
			(scaling == nil || scaling.HorizontalScaling == nil) ||
			// If the scaling strategy is specified with minReplicas, we compare
			// the minReplicas with the existing Deployment replicas and we set
			// the replicas to the minReplicas if the existing Deployment replicas
			// are less than the minReplicas to enforce faster scaling before HPA
			// kicks in.
			(scaling.HorizontalScaling != nil &&
				scaling.HorizontalScaling.MinReplicas != nil &&
				existing.Spec.Replicas != nil &&
				*existing.Spec.Replicas < *scaling.HorizontalScaling.MinReplicas) {
			if !cmp.Equal(existing.Spec.Replicas, desired.Spec.Replicas) {
				existing.Spec.Replicas = desired.Spec.Replicas
				updated = true
			}
		}
		if updated {
			diff := cmp.Diff(original.Spec.Template, desired.Spec.Template, opts...)
			log.Trace(logger, "DataPlane Deployment diff detected", "diff", diff)
		}

		return patch.ApplyPatchIfNotEmpty(ctx, cl, logger, existing, original, updated)
	}

	if err = cl.Create(ctx, desired); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating Deployment for DataPlane %s: %w", dataplane.Name, err)
	}

	log.Debug(logger, "deployment for DataPlane created", "deployment", desired.Name)
	return op.Created, desired, nil
}
