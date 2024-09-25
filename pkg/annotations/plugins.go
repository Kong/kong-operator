package annotations

import (
	"strings"

	"github.com/kong/gateway-operator/pkg/consts"
)

// ExtractPlugins extracts plugin names from the given object's
// konghq.com/plugins annotation.
func ExtractPlugins(obj interface {
	GetAnnotations() map[string]string
	GetNamespace() string
},
) []string {
	ann, ok := obj.GetAnnotations()[consts.PluginsAnnotationKey]
	if !ok {
		return nil
	}

	namespace := obj.GetNamespace()
	split := strings.Split(ann, ",")
	ret := make([]string, 0, len(split))
	for _, p := range split {
		ret = append(ret, namespace+"/"+strings.TrimSpace(p))
	}
	return ret
}
