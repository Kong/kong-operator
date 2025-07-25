package diagnostics

import (
	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/kong/kong-operator/ingress-controller/internal/dataplane/fallback"
)

// mapFallbackCacheMetadataIntoFallbackResponse maps the generated cache metadata into a FallbackResponse.
func mapFallbackCacheMetadataIntoFallbackResponse(m mo.Option[fallback.GeneratedCacheMetadata]) FallbackResponse {
	meta, ok := m.Get()
	if !ok {
		return FallbackResponse{
			Status: FallbackStatusNotTriggered,
		}
	}

	brokenObjects := lo.Map(meta.BrokenObjects, func(objHash fallback.ObjectHash, _ int) FallbackAffectedObjectMeta {
		return FallbackAffectedObjectMeta{
			Group:     objHash.Group,
			Kind:      objHash.Kind,
			Namespace: objHash.Namespace,
			Name:      objHash.Name,
			ID:        string(objHash.UID),
		}
	})
	mapAffectedObjectsMeta := func(affectedObjects []fallback.AffectedCacheObjectMetadata) []FallbackAffectedObjectMeta {
		return lo.Map(affectedObjects, func(affectedObject fallback.AffectedCacheObjectMetadata, _ int) FallbackAffectedObjectMeta {
			gvk := affectedObject.Object.GetObjectKind().GroupVersionKind()
			return FallbackAffectedObjectMeta{
				Group:     gvk.Group,
				Kind:      gvk.Kind,
				Version:   gvk.Version,
				Namespace: affectedObject.Object.GetNamespace(),
				Name:      affectedObject.Object.GetName(),
				ID:        string(affectedObject.Object.GetUID()),
				CausingObjects: lo.Map(affectedObject.CausingObjects, func(objHash fallback.ObjectHash, _ int) string {
					return objHash.String()
				}),
			}
		})
	}
	return FallbackResponse{
		Status:            FallbackStatusTriggered,
		BrokenObjects:     brokenObjects,
		ExcludedObjects:   mapAffectedObjectsMeta(meta.ExcludedObjects),
		BackfilledObjects: mapAffectedObjectsMeta(meta.BackfilledObjects),
	}
}
