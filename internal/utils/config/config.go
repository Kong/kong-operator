package config

import (
	"sort"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

const (
	kongPluginsEnvVarName   = "KONG_PLUGINS"
	kongPluginsDefaultValue = "bundled"

	kongLuaPackagePathVarName      = "KONG_LUA_PACKAGE_PATH"
	kongLuaPackagePathDefaultValue = "/opt/?.lua;;"
)

// -----------------------------------------------------------------------------
// Utils - Config
// -----------------------------------------------------------------------------

// FillContainerEnvMap updates the environment variables in the provided PodTemplateSpec's container by taking a slice of env vars as an input.
func FillContainerEnvs(existing []corev1.EnvVar, podTemplateSpec *corev1.PodTemplateSpec, containerName string, envSet []corev1.EnvVar) {
	if podTemplateSpec == nil {
		return
	}

	podSpec := &podTemplateSpec.Spec
	container := k8sutils.GetPodContainerByName(podSpec, containerName)
	if container == nil {
		return
	}

	for _, envVar := range existing {
		if !k8sutils.IsEnvVarPresent(envVar, container.Env) {
			container.Env = append(container.Env, envVar)
		}
	}
	for _, envVar := range envSet {
		if !k8sutils.IsEnvVarPresent(envVar, container.Env) {
			container.Env = append(container.Env, envVar)
		}
	}
	sort.Sort(k8sutils.SortableEnvVars(container.Env))
}

// EnvVarMapToSlice converts a map[string]string to a slice of environment variables.
// Note: this function should be used only when the env var slice is made of simple key-value pairs.
// in case of more complex env vars, don't rely on maps.
func EnvVarMapToSlice(envMap map[string]string) []corev1.EnvVar {
	return lo.MapToSlice(envMap, func(k, v string) corev1.EnvVar {
		return corev1.EnvVar{
			Name:  k,
			Value: v,
		}
	})
}

// ConfigureKongPluginRelatedEnvVars returns the environment variables
// needed for configuring the Kong Gateway with the provided Kong Plugin
// names. If kongPluginNames is nil or empty, nil is returned. Kong will use bundled
// plugins by default if we do not override `KONG_PLUGINS`.
func ConfigureKongPluginRelatedEnvVars(kongPluginNames []string) []corev1.EnvVar {
	if len(kongPluginNames) == 0 {
		return nil
	}
	kpiNames := make([]string, 0, len(kongPluginNames)+1) // +1 for the default value
	// Const "bundled" is required to have the default plugins enabled.
	kpiNames = append(kpiNames, kongPluginsDefaultValue)
	kpiNames = append(kpiNames, kongPluginNames...)
	return []corev1.EnvVar{
		{
			Name:  kongPluginsEnvVarName,
			Value: strings.Join(kpiNames, ","),
		},
		{
			Name:  kongLuaPackagePathVarName,
			Value: kongLuaPackagePathDefaultValue,
		},
	}
}
