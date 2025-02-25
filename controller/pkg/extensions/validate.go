package extensions

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ValidateExtensions validates the extensions referenced by the provided DataPlane and returns
// a condition indicating if the extensions are supported or not.
func ValidateExtensions(dataplane *operatorv1beta1.DataPlane) *metav1.Condition {
	if len(dataplane.Spec.Extensions) == 0 {
		return nil
	}

	condition := metav1.Condition{
		Status:             metav1.ConditionTrue,
		Type:               string(consts.AcceptedExtensionsType),
		Reason:             string(consts.AcceptedExtensionsReason),
		Message:            "All extensions are accepted",
		ObservedGeneration: dataplane.Generation,
		LastTransitionTime: metav1.Now(),
	}
	var messageBuilder strings.Builder
	for i, ext := range dataplane.Spec.Extensions {
		if ext.Group != konnectv1alpha1.SchemeGroupVersion.Group || ext.Kind != konnectv1alpha1.KonnectExtensionKind {
			buildMessage(&messageBuilder, fmt.Sprintf("Extension %s/%s is not supported", ext.Group, ext.Kind))
			continue
		}
		for j, ext2 := range dataplane.Spec.Extensions {
			if i != j {
				if ext.Group == ext2.Group && ext.Kind == ext2.Kind {
					message := fmt.Sprintf("Extension %s/%s is duplicated", ext.Group, ext.Kind)
					if !strings.Contains(messageBuilder.String(), message) {
						buildMessage(&messageBuilder, message)
					}
				}
			}
		}
	}
	if messageBuilder.Len() > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = string(consts.NotSupportedExtensionsReason)
		condition.Message = messageBuilder.String()
	}

	return &condition
}

func buildMessage(messageBuilder *strings.Builder, message string) {
	if messageBuilder.Len() > 0 {
		messageBuilder.WriteString(" - ")
	}
	messageBuilder.WriteString(message)
}
