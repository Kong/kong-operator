package convert

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ToPartialObjectMetadata converts typed Kubernetes objects into
// *metav1.PartialObjectMetadata so they can be used with the metadata fake client.
func ToPartialObjectMetadata(s *runtime.Scheme, objs ...runtime.Object) []runtime.Object {
	out := make([]runtime.Object, 0, len(objs))
	for _, obj := range objs {
		gvks, _, err := s.ObjectKinds(obj)
		if err != nil || len(gvks) == 0 {
			continue
		}
		accessor, err := meta.Accessor(obj)
		if err != nil {
			continue
		}
		out = append(out, &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: gvks[0].GroupVersion().String(),
				Kind:       gvks[0].Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      accessor.GetName(),
				Namespace: accessor.GetNamespace(),
			},
		})
	}
	return out
}
