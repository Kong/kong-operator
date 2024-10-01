package dataplane

import (
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

const (
	kongPluginsEnvVarName   = "KONG_PLUGINS"
	kongPluginsDefaultValue = "bundled"

	kongLuaPackagePathVarName      = "KONG_LUA_PACKAGE_PATH"
	kongLuaPackagePathDefaultValue = "/opt/?.lua;;"
)

// -----------------------------------------------------------------------------
// DataPlane Utils - Config
// -----------------------------------------------------------------------------

// FillDataPlaneProxyContainerEnvs sets any unset default configuration
// options on the DataPlane. It allows overriding the defaults via the provided
// PodTemplateSpec.
// EnvVars are sorted lexographically as a side effect.
// It also returns the updated EnvVar slice.
func FillDataPlaneProxyContainerEnvs(existing []corev1.EnvVar, podTemplateSpec *corev1.PodTemplateSpec, envSet map[string]string) {
	if podTemplateSpec == nil {
		return
	}

	podSpec := &podTemplateSpec.Spec
	container := k8sutils.GetPodContainerByName(podSpec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return
	}

	for _, envVar := range existing {
		if !k8sutils.IsEnvVarPresent(envVar, container.Env) {
			container.Env = append(container.Env, envVar)
		}
	}
	for k, v := range envSet {
		envVar := corev1.EnvVar{
			Name:  k,
			Value: v,
		}
		if !k8sutils.IsEnvVarPresent(envVar, container.Env) {
			container.Env = append(container.Env, envVar)
		}
	}
	sort.Sort(k8sutils.SortableEnvVars(container.Env))
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
