package metadata

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// ExtractPluginsWithNamespaces extracts plugin namespaced names from the given object's
// konghq.com/plugins annotation.
// This function trims the whitespace from the plugin names.
//
// For example, for KongConsumer in namespace default, having the "konghq.com/plugins"
// annotation set to "p1,p2" this will return []string{"default/p1", "default/p2"}
func ExtractPluginsWithNamespaces(obj ObjectWithAnnotationsAndNamespace) []string {
	return extractPlugins(obj, nsOptWithNamespace)
}

// ExtractPlugins extracts plugin names from the given object's
// konghq.com/plugins annotation.
// This function trims the whitespace from the plugin names.
//
// For example, for KongConsumer in namespace default, having the "konghq.com/plugins"
// annotation set to "p1,p2" this will return []string{"p1", "p2"}
func ExtractPlugins(obj ObjectWithAnnotationsAndNamespace) []string {
	return extractPlugins(obj, nsOptWithoutNamespace)
}

// ExtractPluginsNamespacedNames extracts plugin namespaced names from the given object's
// konghq.com/plugins annotation. Plugins can optionally specify the namespace using the
// "<namespace>:<plugin-name>" format.
// This function trims the whitespace from the plugin names.
//
// For example, for an object having the "konghq.com/plugins" annotation set to "default:p1,p2"
// this will return:
//
//	 []types.NamespacedName{
//			types.NamespacedName{Namespace: "default", Name: "p1"},
//			types.NamespacedName{Namespace: "", Name: "p2"},
//		}
func ExtractPluginsNamespacedNames(obj ObjectWithAnnotationsAndNamespace) []types.NamespacedName {
	ann, ok := obj.GetAnnotations()[AnnotationKeyPlugins]
	if !ok || len(ann) == 0 {
		return nil
	}

	split := strings.Split(ann, ",")
	plugins := make([]types.NamespacedName, 0, len(split))
	for _, s := range split {
		if strings.TrimSpace(s) == "" {
			continue
		}

		plugin := types.NamespacedName{}

		idxColon := strings.Index(s, ":")
		if idxColon == len(s)-1 || idxColon == 0 {
			// invalid plugin name or namespace
			continue
		}

		if idxColon != -1 {
			plugin.Namespace = strings.TrimSpace(s[0:idxColon])
			plugin.Name = strings.TrimSpace(s[idxColon+1:])
		} else {
			plugin.Name = strings.TrimSpace(s)
		}
		plugins = append(plugins, plugin)
	}
	return plugins
}

type extractPluginsNamespaceOpt byte

const (
	nsOptWithNamespace extractPluginsNamespaceOpt = iota
	nsOptWithoutNamespace
)

func extractPlugins(obj ObjectWithAnnotationsAndNamespace, nsOpt extractPluginsNamespaceOpt) []string {
	if obj == nil {
		return nil
	}

	ann, ok := obj.GetAnnotations()[AnnotationKeyPlugins]
	if !ok || len(ann) == 0 {
		return nil
	}

	namespace := obj.GetNamespace()

	split := strings.Split(ann, ",")
	ret := make([]string, 0, len(split))
	for _, p := range split {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}

		v := trimmed
		if nsOpt == nsOptWithNamespace {
			v = namespace + "/" + trimmed
		}
		ret = append(ret, v)
	}
	return ret
}
