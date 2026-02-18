package dataplane

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/dataplane/certificates"
	dataplanepkg "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/controller/pkg/utils"
	"github.com/kong/kong-operator/v2/internal/utils/config"
	"github.com/kong/kong-operator/v2/internal/versions"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// restartAnnotationKey is the annotation key used to mark a Deployment as restarted.
// This is used to detect if a Deployment was restarted using `kubectl rollout restart`.
// The value is a timestamp in RFC3339 format.
// It's hardcoded here to match the annotation used by the kubectl command:
// https://github.com/kubernetes/kubernetes/blob/82db38a23c7820b1924d89f458fd368023f3980c/staging/src/k8s.io/kubectl/pkg/polymorphichelpers/objectrestarter.go#L51
//
//godoclint:disable max-len
const restartAnnotationKey = "kubectl.kubernetes.io/restartedAt"

// DeploymentBuilder builds a Deployment for a DataPlane.
type DeploymentBuilder struct {
	clusterCertificateName string
	logger                 logr.Logger
	client                 client.Client
	additionalLabels       client.MatchingLabels
	defaultImage           string
	opts                   []k8sresources.DeploymentOpt

	secretLabelSelector string
}

// NewDeploymentBuilder creates a DeploymentBuilder.
func NewDeploymentBuilder(logger logr.Logger, client client.Client) *DeploymentBuilder {
	d := &DeploymentBuilder{}
	d.logger = logger
	d.client = client
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

// WithSecretLabelSelector sets the label of created `Secret`s to "true" to get the secrets
// to be reconciled by other controllers.
func (d *DeploymentBuilder) WithSecretLabelSelector(key string) *DeploymentBuilder {
	d.secretLabelSelector = key
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
	enforceConfig bool,
	validateDataPlaneImage bool,
) (*appsv1.Deployment, op.Result, error) {
	opts := []certificates.CertOpt{}
	if d.secretLabelSelector != "" {
		opts = append(opts, certificates.WithSecretLabel(d.secretLabelSelector, "true"))
	}
	if err := certificates.CreateKonnectCert(ctx, d.logger, dataplane, d.client, opts...); err != nil {
		return nil, op.Noop, fmt.Errorf("failed creating konnect cert: %w", err)
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
	desiredDeployment, err := generateDataPlaneDeployment(validateDataPlaneImage, dataplane, d.defaultImage, d.additionalLabels, d.opts...)
	if err != nil {
		return nil, op.Noop, fmt.Errorf("could not generate Deployment: %w", err)
	}

	// Add the cluster certificate to the generated Deployment
	desiredDeployment = setClusterCertVars(desiredDeployment, d.clusterCertificateName)

	if err := certificates.MountAndUseKonnectCert(ctx, d.logger, dataplane, d.client, desiredDeployment); err != nil {
		return nil, op.Noop, fmt.Errorf("failed to mount konnect cert: %w", err)
	}

	// TODO https://github.com/kong/kong-operator/issues/128
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

	if err := k8sresources.AnnotateObjWithHash(desiredDeployment.Unwrap(), dataplane.Spec); err != nil {
		return nil, op.Noop, err
	}

	// push the complete Deployment to Kubernetes
	res, deployment, err := reconcileDataPlaneDeployment(ctx, d.client, d.logger, enforceConfig,
		dataplane, existingDeployment, desiredDeployment.Unwrap())
	if err != nil {
		return nil, op.Noop, err
	}
	return deployment, res, nil
}

// generateDataPlaneDeployment generates the base Deployment for a DataPlane. It determines the image to use and
// generates an opt transform function to add additional labels before invoking the generator utility.
func generateDataPlaneDeployment(
	validateDataPlaneImage bool,
	dataplane *operatorv1beta1.DataPlane,
	defaultImage string,
	additionalDeploymentLabels client.MatchingLabels,
	opts ...k8sresources.DeploymentOpt,
) (deployment *k8sresources.Deployment, err error) {
	if len(additionalDeploymentLabels) > 0 {
		opts = append(opts, matchingLabelsToDeploymentOpt(additionalDeploymentLabels))
	}

	versionValidationOptions := make([]versions.VersionValidationOption, 0)
	if validateDataPlaneImage {
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
	config.FillContainerEnvs(existing, &deployment.Spec.Template, consts.DataPlaneProxyContainerName, config.EnvVarMapToSlice(envSet))
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
	maps.Copy(matchingLabels, additionalDeploymentLabels)

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

func podTemplateSpecHasRestartAnnotation(template *corev1.PodTemplateSpec) (string, bool) {
	if template == nil || template.Annotations == nil {
		return "", false
	}
	v, ok := template.Annotations[restartAnnotationKey]
	return v, ok && v != ""
}

// isRecentDeploymentRestart detects if a deployment is undergoing a recent restart operation.
// It checks for the presence of the kubectl restart annotation and verifies if it's recent (within 5 minutes).
// Returns the restart timestamp if present, and a boolean indicating if a recent restart is detected.
func isRecentDeploymentRestart(template *corev1.PodTemplateSpec, logger logr.Logger) (string, bool) {
	// Check if we have a fresh restart annotation (from the last 5 minutes)
	// This prevents us from treating all deployments with old restart annotations
	// as perpetually in a restart state, fixing issue #1390
	restartTimeStr, hasRestartAnnotation := podTemplateSpecHasRestartAnnotation(template)
	if !hasRestartAnnotation {
		return "", false
	}

	// Only treat it as a restart if the timestamp is recent (within 5 minutes)
	restartTime, err := time.Parse(time.RFC3339, restartTimeStr)
	if err != nil {
		log.Debug(logger, "detected restart with unparseable timestamp", "timestamp", restartTimeStr)
		return restartTimeStr, true // If we can't parse time, assume it's a restart for safety
	}

	// Check if restart annotation is less than 5 minutes old
	// We use 5m here as threshold as that correlates with constants used by Kubernetes itself.
	// Restart annotations older than this duration shouldn't cause a reconciliation effect.
	// See: https://github.com/kubernetes/kubernetes/blob/82db38a23c7820b1924d89f458fd368023f3980c/pkg/controller/namespace/config/v1alpha1/defaults.go#L41
	if time.Since(restartTime) < 5*time.Minute {
		log.Debug(logger, "detected recent restart operation", "isRestartOperation", true, "restartTime", restartTime)
		return restartTimeStr, true
	}

	log.Debug(logger, "found old restart annotation, not treating as restart", "restartTime", restartTime)
	return restartTimeStr, false
}

// reconcileDataPlaneDeployment takes any existing DataPlane Deployment and a desired DataPlane Deployment and
// reconciles the existing state to the desired state by either updating an existing Deployment, creating a new one,
// or doing nothing.
func reconcileDataPlaneDeployment(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	enforceConfig bool,
	dataplane *operatorv1beta1.DataPlane,
	existing *appsv1.Deployment,
	desired *appsv1.Deployment,
) (res op.Result, deploy *appsv1.Deployment, err error) {
	if existing != nil {

		// If the enforceConfig flag is not set, we compare the spec hash of the
		// existing Deployment with the spec hash of the desired Deployment. If
		// the hashes match, we skip the update.
		if !enforceConfig {
			match, err := k8sresources.SpecHashMatchesAnnotation(dataplane.Spec, existing)
			if err != nil {
				return op.Noop, nil, err
			}
			if match {
				log.Debug(logger, "DataPlane Deployment spec hash matches existing Deployment, skipping update")
				return op.Noop, existing, nil
			}
			// If the spec hash does not match, we need to enforce the configuration
			// so fall through to the update logic.
		}

		var updated bool
		original := existing.DeepCopy()

		k8sresources.SetDefaultsPodTemplateSpec(&desired.Spec.Template)

		// Keep track of this for logging purposes
		originalReplicaCount := existing.Spec.Replicas

		// ensure that object metadata is up to date
		updated, existing.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existing.ObjectMeta, desired.ObjectMeta)

		// some custom comparison rules are needed for some PodTemplateSpec sub-attributes
		opts := []cmp.Option{
			cmp.Comparer(k8sresources.ResourceRequirementsEqual),
			utils.IgnoreAnnotationKeysComparer(restartAnnotationKey),
		}

		if !cmp.Equal(existing.Spec.Template, desired.Spec.Template, opts...) {
			restartTimeStr, isRestartOperation := isRecentDeploymentRestart(&existing.Spec.Template, logger)
			if isRestartOperation {
				log.Debug(logger, "found restart annotation", "timestamp", restartTimeStr)
				// Preserve the restart annotation
				if desired.Spec.Template.Annotations == nil {
					desired.Spec.Template.Annotations = make(map[string]string)
				}
				// Use the restart timestamp we already retrieved
				desired.Spec.Template.Annotations[restartAnnotationKey] = restartTimeStr
			}

			existing.Spec.Template = desired.Spec.Template
			updated = true
		}

		// ensure that rollout strategy is up to date
		if !cmp.Equal(existing.Spec.Strategy, desired.Spec.Strategy) {
			existing.Spec.Strategy = desired.Spec.Strategy
			updated = true
		}

		// Always process replica updates, even during/after restart operations
		// This ensures that if a user scales after a restart, the changes take effect
		if scaling := dataplane.Spec.Deployment.Scaling; false ||
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

			// Always check for replica changes, regardless of restart status
			// Fixed issue #1390: Ensure replicas update correctly after restart
			if !cmp.Equal(existing.Spec.Replicas, desired.Spec.Replicas) {
				log.Debug(logger, "updating replica count", "original", originalReplicaCount, "from", existing.Spec.Replicas, "to", desired.Spec.Replicas)
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
