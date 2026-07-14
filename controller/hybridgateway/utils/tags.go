package utils

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// TagsFromBackendRefs returns the konghq.com/tags of the first backend Service
// (in backendRefs order) that carries the annotation. Returns nil when no
// referenced Service has the annotation, or none can be fetched. This mirrors
// the first-wins resolution used for other backend-Service annotations
// (e.g. konghq.com/protocol).
func TagsFromBackendRefs(
	ctx context.Context,
	cl client.Client,
	namespace string,
	backendRefs []gwtypes.BackendRef,
	logger logr.Logger,
) []string {
	for _, ref := range backendRefs {
		if !IsBackendRefSupported(ref.Group, ref.Kind) {
			continue
		}
		ns := namespace
		if ref.Namespace != nil && *ref.Namespace != "" {
			ns = string(*ref.Namespace)
		}
		svc := &corev1.Service{}
		if err := cl.Get(ctx, client.ObjectKey{Namespace: ns, Name: string(ref.Name)}, svc); err != nil {
			log.Debug(logger, "Failed to fetch backend Service for tags annotation check",
				"service", fmt.Sprintf("%s/%s", ns, ref.Name), "error", err)
			continue
		}
		if tags := metadata.ExtractTags(svc.GetAnnotations()); len(tags) > 0 {
			return tags
		}
	}
	return nil
}
