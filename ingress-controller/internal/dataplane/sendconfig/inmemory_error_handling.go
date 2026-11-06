package sendconfig

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
)

type (
	// FlatEntityError represents a single Kong entity with one or more invalid fields.
	FlatEntityError = dataplanepush.FlatEntityError

	// FlatError represents an error for a single field within a Kong entity or a whole entity.
	FlatError = dataplanepush.FlatError

	// FlatErrorType tells whether a FlatError is associated with a single field or a whole entity.
	FlatErrorType = dataplanepush.FlatErrorType
)

const (
	// FlatErrorTypeField is an error associated with a single field.
	FlatErrorTypeField = dataplanepush.FlatErrorTypeField

	// FlatErrorTypeEntity is an error associated with a whole entity.
	FlatErrorTypeEntity = dataplanepush.FlatErrorTypeEntity
)

// rawResourceError is a Kong configuration error associated with a Kubernetes resource with Kubernetes metadata stored
// in raw Kong entity tags.
type rawResourceError struct {
	Name     string
	ID       string
	Tags     []string
	Problems map[string]string
}

// parseFlatEntityErrors takes a Kong /config error response body and parses its "fields.flattened_errors" value
// into errors associated with Kubernetes resources.
func parseFlatEntityErrors(body []byte, logger logr.Logger) ([]ResourceError, error) {
	flattened, err := dataplanepush.ParseFlatEntityErrors(body, logger)
	if err != nil {
		return nil, err
	}
	if len(flattened) == 0 {
		return nil, nil
	}

	resourceErrors := make([]ResourceError, 0, len(flattened))
	for _, ee := range flattened {
		raw := flattenedErrorIntoRaw(ee, logger)
		parsed, err := parseRawResourceError(raw)
		if err != nil {
			logger.Error(err, "API error returned from the gateway for an entity is missing Kubernetes metadata in tags, Kubernetes Event won't be created",
				"name", ee.Name,
				"id", ee.ID,
				"tags", ee.Tags,
				"type", ee.Type,
				"raw_error", ee,
			)
			continue
		}
		resourceErrors = append(resourceErrors, parsed)
	}
	return resourceErrors, nil
}

func flattenedErrorIntoRaw(ee FlatEntityError, logger logr.Logger) rawResourceError {
	raw := rawResourceError{
		Name:     ee.Name,
		ID:       ee.ID,
		Tags:     ee.Tags,
		Problems: map[string]string{},
	}
	for _, p := range ee.Errors {
		if len(p.Message) > 0 && len(p.Messages) > 0 {
			logger.Error(nil, "Entity has both single and array errors for field",
				"name", ee.Name, "field", p.Field)
			continue
		}
		if len(p.Message) > 0 {
			switch p.Type {
			case FlatErrorTypeField:
				// If the error is associated with a single field, store it in the map under the field name.
				raw.Problems[p.Field] = p.Message
			case FlatErrorTypeEntity:
				// If the error is associated with a whole entity, store it in the map under the entity type and name.
				raw.Problems[fmt.Sprintf("%s:%s", ee.Type, ee.Name)] = p.Message
			}
		}
		for i, message := range p.Messages {
			if len(message) > 0 {
				raw.Problems[fmt.Sprintf("%s[%d]", p.Field, i)] = message
			}
		}
	}
	return raw
}

// parseRawResourceError takes a raw resource error and parses its tags into Kubernetes metadata. If critical tags are
// missing, it returns an error indicating the missing tag.
func parseRawResourceError(raw rawResourceError) (ResourceError, error) {
	re := ResourceError{
		Problems: raw.Problems,
	}

	var gvk schema.GroupVersionKind
	for _, tag := range raw.Tags {
		switch {
		case strings.HasPrefix(tag, util.K8sNameTagPrefix):
			re.Name = strings.TrimPrefix(tag, util.K8sNameTagPrefix)
		case strings.HasPrefix(tag, util.K8sNamespaceTagPrefix):
			re.Namespace = strings.TrimPrefix(tag, util.K8sNamespaceTagPrefix)
		case strings.HasPrefix(tag, util.K8sKindTagPrefix):
			gvk.Kind = strings.TrimPrefix(tag, util.K8sKindTagPrefix)
		case strings.HasPrefix(tag, util.K8sVersionTagPrefix):
			gvk.Version = strings.TrimPrefix(tag, util.K8sVersionTagPrefix)
		case strings.HasPrefix(tag, util.K8sGroupTagPrefix):
			gvk.Group = strings.TrimPrefix(tag, util.K8sGroupTagPrefix)
		case strings.HasPrefix(tag, util.K8sUIDTagPrefix):
			re.UID = strings.TrimPrefix(tag, util.K8sUIDTagPrefix)
		}
	}

	re.APIVersion, re.Kind = gvk.ToAPIVersionAndKind()
	if re.Name == "" {
		return re, fmt.Errorf("resource error has no name tag")
	}
	if re.Namespace == "" && !gvkIsClusterScoped(gvk) {
		return re, fmt.Errorf("resource error has no namespace tag, name: %s", raw.Name)
	}
	if re.Kind == "" {
		return re, fmt.Errorf("resource error has not enough kind, group, version tags, name: %s", raw.Name)
	}
	if re.UID == "" {
		return re, fmt.Errorf("resource error has no uid tag, name: %s", raw.Name)
	}
	return re, nil
}

func gvkIsClusterScoped(gvk schema.GroupVersionKind) bool {
	if gvk.Group == configurationv1.GroupVersion.Group && gvk.Version == configurationv1.GroupVersion.Version {
		return gvk.Kind == "KongClusterPlugin" || gvk.Kind == "KongLicense"
	}
	if gvk.Group == configurationv1alpha1.GroupVersion.Group && gvk.Version == configurationv1alpha1.GroupVersion.Version {
		return gvk.Kind == "KongVault"
	}
	return false
}
