package dataplane

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// Validator validates DataPlane objects.
type Validator struct {
	c client.Client
}

// NewValidator creates a DataPlane validator.
func NewValidator(c client.Client) *Validator {
	return &Validator{c: c}
}

// ValidateUpdate validates a DataPlane object change upon an update event.
func (v *Validator) ValidateUpdate(dataplane, oldDataplane *operatorv1beta1.DataPlane) error {
	return v.ValidateIfRolloutInProgress(dataplane, oldDataplane)
}

// Validate validates a DataPlane object and return the first validation error found.
func (v *Validator) Validate(dataplane *operatorv1beta1.DataPlane) error {
	err := v.ValidateDataPlaneDeploymentOptions(dataplane.Namespace, &dataplane.Spec.Deployment.DeploymentOptions)
	if err != nil {
		return err
	}

	if err := v.ValidateDataPlaneDeploymentRollout(dataplane.Spec.Deployment.Rollout); err != nil {
		return err
	}

	return nil
}

// ValidateDataPlaneDeploymentRollout validates the Rollout field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentRollout(rollout *operatorv1beta1.Rollout) error {
	if rollout != nil && rollout.Strategy.BlueGreen != nil && rollout.Strategy.BlueGreen.Promotion.Strategy == operatorv1beta1.AutomaticPromotion {
		// Can't use AutomaticPromotion just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1006.
		return errors.New("DataPlane AutomaticPromotion cannot be used yet")
	}

	if rollout != nil && rollout.Strategy.BlueGreen != nil &&
		rollout.Strategy.BlueGreen.Resources.Plan.Deployment == operatorv1beta1.RolloutResourcePlanDeploymentDeleteOnPromotionRecreateOnRollout {
		// Can't use DeleteOnPromotionRecreateOnRollout just yet.
		// Related: https://github.com/Kong/gateway-operator/issues/1010.
		return errors.New("DataPlane Deployment resource plan DeleteOnPromotionRecreateOnRollout cannot be used yet")
	}

	return nil
}

func (v *Validator) ValidateIfRolloutInProgress(dataplane, oldDataplane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.RolloutStatus == nil {
		return nil
	}

	// If no rollout condition exists, the rollout is not started yet
	c, exists := k8sutils.GetCondition(consts.DataPlaneConditionTypeRolledOut, dataplane.Status.RolloutStatus)
	if !exists {
		return nil
	}

	// If the promotion is in progress and the spec is changed in the update
	// then reject the change.
	if c.Reason == string(consts.DataPlaneConditionReasonRolloutPromotionInProgress) &&
		!cmp.Equal(dataplane.Spec, oldDataplane.Spec) {
		return ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion
	}

	return nil
}

// ValidateDataPlaneDeploymentOptions validates the DeploymentOptions field of DataPlane object.
func (v *Validator) ValidateDataPlaneDeploymentOptions(namespace string, opts *operatorv1beta1.DeploymentOptions) error {
	if opts == nil || opts.PodTemplateSpec == nil {
		// Can't use empty DeploymentOptions because we still require users
		// to provide an image
		// Related: https://github.com/Kong/gateway-operator/issues/754.
		return errors.New("DataPlane requires an image")
	}

	// Until https://github.com/Kong/gateway-operator/issues/20 is resolved we
	// require DataPlanes that they are provided with image and version set.
	// Related: https://github.com/Kong/gateway-operator/issues/754.
	container := k8sutils.GetPodContainerByName(&opts.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return fmt.Errorf("couldn't find proxy container in DataPlane spec")
	}

	if container.Image == "" {
		return errors.New("DataPlane requires an image")
	}

	// validate db mode.
	dbMode, dbModeFound, err := v.getDBModeFromEnv(namespace, container.Env)
	if err != nil {
		return err
	}

	// if dbMode not found in envVar, search for it in EnvVarFrom.
	if !dbModeFound {
		dbMode, _, err = v.getDBModeFromEnvFrom(namespace, container.EnvFrom)
		if err != nil {
			return err
		}
	}

	// only support dbless mode.
	if dbMode != "" && dbMode != "off" {
		return fmt.Errorf("database backend %s of DataPlane not supported currently", dbMode)
	}

	return nil
}

// getDBModeFromEnv gets the dbmode from Env.
// If the second return value is false, the dbMode is not found in Env.
func (v *Validator) getDBModeFromEnv(namespace string, envs []corev1.EnvVar) (string, bool, error) {
	dbMode := ""
	dbModeFound := false
	for _, envVar := range envs {
		// use the last appearance of the same key as the result since k8s takes this precedence.
		if envVar.Name == consts.EnvVarKongDatabase {
			// value is non-empty.
			if envVar.Value != "" {
				dbMode = envVar.Value
				dbModeFound = true
			} else if envVar.ValueFrom != nil {
				// value is empty,get from ValueFrom from configmap/secret.
				if envVar.ValueFrom.ConfigMapKeyRef != nil {
					var err error
					dbMode, dbModeFound, err = v.getValueFromConfigMapKeyRef(namespace, envVar.ValueFrom.ConfigMapKeyRef)
					if err != nil {
						return "", false, err
					}
				}
				if envVar.ValueFrom.SecretKeyRef != nil {
					var err error
					dbMode, dbModeFound, err = v.getValueFromSecretRef(namespace, envVar.ValueFrom.SecretKeyRef)
					if err != nil {
						return "", false, err
					}
				}
			}
		}
	}
	return dbMode, dbModeFound, nil
}

func (v *Validator) getDBModeFromEnvFrom(namespace string, envFroms []corev1.EnvFromSource) (string, bool, error) {
	dbMode := ""
	dbModeFound := false
	for _, envFrom := range envFroms {
		// if the envFrom.Prefix is the prefix of KONG_DATABASE,
		// it is possible that this envFrom contains values of KONG_DATABASE.
		if strings.HasPrefix(consts.EnvVarKongDatabase, envFrom.Prefix) {
			if envFrom.ConfigMapRef != nil {
				var err error
				dbMode, dbModeFound, err = v.getDBModeFromConfigMapRef(namespace, envFrom.Prefix, envFrom.ConfigMapRef)
				// technically it goes slightly against eventual-consistency to throw an error here,
				// but the alternative is that we would need to validate ALL ConfigMaps on create
				// and do relational mapping to DataPlane resources to validate that they aren't
				// going to introduce a new violation, or we would have to do an additional level
				// of validation that could only run during reconciliation.
				if err != nil {
					return "", false, err
				}
			}
			if envFrom.SecretRef != nil {
				var err error
				dbMode, dbModeFound, err = v.getDBModeFromSecretRef(namespace, envFrom.Prefix, envFrom.SecretRef)
				if err != nil {
					return "", false, err
				}
			}
		}
	}
	return dbMode, dbModeFound, nil
}

func (v *Validator) getValueFromConfigMapKeyRef(namespace string, cmKeyRef *corev1.ConfigMapKeySelector) (string, bool, error) {
	cm := &corev1.ConfigMap{}
	namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: cmKeyRef.Name}
	err := v.c.Get(context.Background(), namespacedName, cm)
	if err != nil {
		return "", false, fmt.Errorf("failed to get configMap %s in configMapKeyRef: %w", cmKeyRef.Name, err)
	}
	if cm.Data != nil && cm.Data[cmKeyRef.Key] != "" {
		return cm.Data[cmKeyRef.Key], true, nil
	}
	return "", false, nil
}

func (v *Validator) getValueFromSecretRef(namespace string, secretKeyRef *corev1.SecretKeySelector) (string, bool, error) {
	secret := &corev1.Secret{}
	namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: secretKeyRef.Name}
	err := v.c.Get(context.Background(), namespacedName, secret)
	if err != nil {
		return "", false, fmt.Errorf("failed to get secret %s in secretRef: %w", secretKeyRef.Name, err)
	}
	if secret.Data != nil && len(secret.Data[secretKeyRef.Key]) > 0 {
		decoded, err := base64.StdEncoding.DecodeString(string(secret.Data[secretKeyRef.Key]))
		if err == nil {
			return string(decoded), true, nil
		}
	}
	return "", false, nil
}

func (v *Validator) getDBModeFromConfigMapRef(namespace string, prefix string, cmRef *corev1.ConfigMapEnvSource) (string, bool, error) {
	cm := &corev1.ConfigMap{}
	namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: cmRef.Name}
	err := v.c.Get(context.Background(), namespacedName, cm)
	if err != nil {
		return "", false, fmt.Errorf("failed to get configMap %s in configMapRef: %w", cmRef.Name, err)
	}

	if cm.Data == nil {
		return "", false, nil
	}

	// find the key in the Data that would become `KONG_DATABASE` after concatenation with the prefix.
	suffix := strings.TrimPrefix(consts.EnvVarKongDatabase, prefix)
	dbMode, ok := cm.Data[suffix]
	return dbMode, ok, nil
}

func (v *Validator) getDBModeFromSecretRef(namespace string, prefix string, secretRef *corev1.SecretEnvSource) (string, bool, error) {
	secret := &corev1.Secret{}
	namespacedName := k8stypes.NamespacedName{Namespace: namespace, Name: secretRef.Name}
	err := v.c.Get(context.Background(), namespacedName, secret)
	if err != nil {
		return "", false, fmt.Errorf("failed to get secret %s in secretRef: %w", secretRef, err)
	}
	if secret.Data == nil {
		return "", false, nil
	}

	suffix := strings.TrimPrefix(consts.EnvVarKongDatabase, prefix)
	value, ok := secret.Data[suffix]
	if !ok {
		return "", false, nil
	}
	decoded, decodeErr := base64.RawStdEncoding.DecodeString(string(value))
	if decodeErr == nil {
		return string(decoded), true, nil
	}
	return "", false, nil
}
