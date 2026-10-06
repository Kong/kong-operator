// This script is responsible for generating CRDs.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/samber/lo"
	"golang.org/x/tools/go/packages"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-tools/pkg/crd"
	"sigs.k8s.io/controller-tools/pkg/genall"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
)

// ChannelType is the type of the channel for CRDs. A CRD can be included in multiple channels at once.
type ChannelType string

const (
	// IngressControllerChannelType is the channel for CRDs that are intended to be used by the Ingress Controller.
	IngressControllerChannelType ChannelType = "ingress-controller"

	// IngressControllerIncubatorChannelType is the channel for CRDs that are incubating to be used by the Ingress Controller.
	IngressControllerIncubatorChannelType ChannelType = "ingress-controller-incubator"

	// GatewayOperatorChannelType is the channel for CRDs that are intended to be used by the Gateway Operator.
	GatewayOperatorChannelType ChannelType = "gateway-operator"

	// KongOperatorChannelType is the channel for CRDs that are used in Kong Operator.
	KongOperatorChannelType ChannelType = "kong-operator"

	// ChannelsAnnotation is the annotation key that's used to mark the channels a CRD belongs to.
	ChannelsAnnotation = "kubernetes-configuration.konghq.com/channels"

	// VersionAnnotation is the annotation key that's used to mark the version of the CRD.
	VersionAnnotation = "kubernetes-configuration.konghq.com/version"
)

// AllChannels is a list of all available channels.
var AllChannels = []ChannelType{IngressControllerIncubatorChannelType, KongOperatorChannelType}

// Code is inspired by https://github.com/kubernetes-sigs/gateway-api/blob/1fe2b9f8ee99a6475a65eedd1ce060f363a8634d/pkg/generator/main.go.
func main() {
	version := os.Getenv("VERSION")
	if version == "" {
		log.Fatalf("VERSION environment variable is required")
	}
	// prepend 'v' to version if the version does not have the 'v' prefix to keep the version annotation the same.
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}

	roots, err := loader.LoadRoots(
		// Needed to parse generated register functions.
		"k8s.io/apimachinery/pkg/runtime/schema",

		// configuration.konghq.com
		"github.com/kong/kong-operator/v2/api/configuration/v1",
		"github.com/kong/kong-operator/v2/api/configuration/v1alpha1",
		"github.com/kong/kong-operator/v2/api/configuration/v1beta1",

		// incubator.ingress-controller.konghq.com
		"github.com/kong/kong-operator/v2/api/incubator/v1alpha1",

		// eventgateway.konghq.com
		"github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1",

		// aigateway.konghq.com
		"github.com/kong/kong-operator/v2/api/aigateway/v1alpha1",

		// mcp.konghq.com
		"github.com/kong/kong-operator/v2/api/mcp/v1alpha1",

		// konnect.konghq.com
		"github.com/kong/kong-operator/v2/api/konnect/v1alpha1",
		"github.com/kong/kong-operator/v2/api/konnect/v1alpha2",

		// aiconfiguration.konghq.com
		"github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1",

		// gateway-operator.konghq.com
		"github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1",
		"github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1",
		"github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1",

		// common types
		"github.com/kong/kong-operator/v2/api/common/v1alpha1",
	)
	if err != nil {
		log.Fatalf("failed to load package roots: %s", err)
	}

	markersRegistry := &markers.Registry{}
	channelsMarkerDef, err := ChannelsMarkerDef()
	if err != nil {
		log.Fatalf("failed to define channels marker: %s", err)
	}
	if err := markersRegistry.Register(channelsMarkerDef); err != nil {
		log.Fatalf("failed to register channels marker: %s", err)
	}

	// Options for writing YAML files that will make sure we do not write the CRD status field
	// and the creation timestamp.
	yamlOpts := []*genall.WriteYAMLOptions{
		genall.WithTransform(transformRemoveCRDStatus),
		genall.WithTransform(genall.TransformRemoveCreationTimestamp),
		genall.WithTransform(addVersion(version)),
	}

	generator := &crd.Generator{}
	parser := &crd.Parser{
		Collector: &markers.Collector{Registry: markersRegistry},
		Checker: &loader.TypeChecker{
			NodeFilters: []loader.NodeFilter{generator.CheckFilter()},
		},
		AllowDangerousTypes:        true, // Allows float32 and float64.
		GenerateEmbeddedObjectMeta: true,
	}

	err = generator.RegisterMarkers(parser.Collector.Registry)
	if err != nil {
		log.Fatalf("failed to register markers: %s", err)
	}

	crd.AddKnownTypes(parser)
	for _, r := range roots {
		parser.NeedPackage(r)
	}

	metav1Pkg := crd.FindMetav1(roots)
	if metav1Pkg == nil {
		log.Fatalf("no objects in the roots, since nothing imported metav1")
	}

	kubeKinds := crd.FindKubeKinds(parser, metav1Pkg)
	if len(kubeKinds) == 0 {
		log.Fatalf("no objects in the roots")
	}

	for _, groupKind := range kubeKinds {
		parser.NeedCRDFor(groupKind, nil)
		crdRaw := parser.CustomResourceDefinitions[groupKind]

		// Prevent the top level metadata for the CRD to be generated regardless of the intention in the arguments
		crd.FixTopLevelMetadata(crdRaw)

		for i := range crdRaw.Spec.Versions {
			if err := simplifyAllOf(crdRaw.Spec.Versions[i].Schema.OpenAPIV3Schema); err != nil {
				log.Fatalf("failed to simplify schema of %s %s: %s", groupKind.Group, crdRaw.Spec.Versions[i].Name, err)
			}
		}

		channels := channelsFromAnnotations(crdRaw)
		if len(channels) == 0 {
			continue
		}

		// For each channel, generate a CRD file (a CRD needs to have a channel marker to be generated).
		log.Printf("generating %v CRD for %v channels\n", groupKind, channels)
		for _, channel := range channelsFromAnnotations(crdRaw) {
			filePath := fmt.Sprintf("config/crd/%s/%s_%s.yaml", channel, crdRaw.Spec.Group, crdRaw.Spec.Names.Plural)
			generationCtx := &genall.GenerationContext{
				OutputRule: genall.OutputToDirectory(filepath.Dir(filePath)),
			}
			if err := generationCtx.WriteYAML(filepath.Base(filePath), "", []any{crdRaw}, yamlOpts...); err != nil {
				log.Fatalf("failed to write CRD: %s", err)
			}
		}
	}

	// Fail on parser errors and on schema/marker errors (e.g. validation
	// markers that could not be applied to a schema, reported as
	// packages.UnknownError). Without this, such errors are silently swallowed
	// and the generated CRDs quietly miss the corresponding validation.
	// TypeError kind is filtered out: it also collects benign type-check
	// noise from partially loaded packages (e.g. "undefined: X" for files
	// whose imports the loader does not need for CRD generation).
	if loader.PrintErrors(roots, packages.TypeError) {
		log.Fatalf("errors occurred while generating CRDs")
	}

	// For each channel, generate a kustomize file that includes all CRDs for that channel.
	for _, channel := range AllChannels {
		crdFiles, err := filepath.Glob(fmt.Sprintf("config/crd/%s/*_*.yaml", channel))
		if err != nil {
			log.Fatalf("failed to glob CRD files: %s", err)
		}

		kustomizeFile := fmt.Sprintf("config/crd/%s/kustomization.yaml", channel)
		kustomizeFileTemplate := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
%s`
		resources := strings.Join(lo.Map(crdFiles, func(f string, _ int) string {
			return fmt.Sprintf("  - %s", filepath.Base(f))
		}), "\n")
		kustomizeContent := fmt.Sprintf(kustomizeFileTemplate, resources)
		err = os.WriteFile(kustomizeFile, []byte(kustomizeContent+"\n"), 0o600)
		if err != nil {
			log.Fatalf("failed to write kustomization file: %s", err)
		}
	}
}

// ChannelsMarkerDef creates a marker definition for the channels marker that can be passed to the markers registry.
func ChannelsMarkerDef() (*markers.Definition, error) {
	return markers.MakeDefinition("kong:channels", markers.DescribesType, ChannelsMarker{})
}

// ChannelsMarker is a marker that can be used to specify the channels a CRD belongs to.
type ChannelsMarker []string

// ApplyToCRD applies the channels marker to the given CRD by adding the channels annotation.
// It implements the Marker interface.
func (m ChannelsMarker) ApplyToCRD(crd *apiext.CustomResourceDefinition, _ string) error { //nolint:unparam
	if crd.Annotations == nil {
		crd.Annotations = map[string]string{}
	}
	crd.Annotations[ChannelsAnnotation] = strings.Join(m, ",")
	return nil
}

// channelsFromAnnotations extracts the channels from the annotations of a CRD. It's used to determine which channels
// a CRD belongs to.
func channelsFromAnnotations(crd apiext.CustomResourceDefinition) []ChannelType {
	if crd.Annotations[ChannelsAnnotation] == "" {
		return nil
	}
	return lo.Map(strings.Split(crd.Annotations[ChannelsAnnotation], ","), func(s string, _ int) ChannelType {
		switch ChannelType(strings.TrimSpace(s)) {
		case IngressControllerIncubatorChannelType:
			return ChannelType(strings.TrimSpace(s))
		case IngressControllerChannelType, GatewayOperatorChannelType, KongOperatorChannelType:
			return KongOperatorChannelType
		default:
			log.Fatalf("unknown channel: %s", s)
			return ""
		}
	})
}

// transformRemoveCRDStatus ensures we do not write the CRD status field.
func transformRemoveCRDStatus(obj map[string]any) error {
	delete(obj, "status")
	return nil
}

// addVersion adds the version annotation to the CRD.
func addVersion(version string) func(obj map[string]any) error {
	return func(obj map[string]any) error {
		metadata, ok := obj["metadata"]
		if !ok {
			metadata = map[string]any{}
			obj["metadata"] = metadata
		}
		annotations, ok := metadata.(map[string]any)["annotations"]
		if !ok {
			annotations = map[string]any{}
			obj["metadata"].(map[string]any)["annotations"] = annotations
		}
		annotations.(map[string]any)[VersionAnnotation] = version
		return nil
	}
}

// simplifyAllOf removes duplicate identical entries from the allOf, anyOf and
// oneOf lists of the given schema and, when exactly one allOf entry remains,
// hoists it into the parent schema unless it conflicts with it.
//
// controller-tools hoists field-level validation markers that collide with the
// markers of the field's named type into allOf, even when both values are
// equal (flattenAllOfInto compares pointer-typed marker values by identity).
// Duplicate conjuncts are redundant, so they are dropped here to keep the
// generated schemas free of duplicated constraints.
func simplifyAllOf(props *apiext.JSONSchemaProps) error {
	if props == nil {
		return nil
	}

	props.AllOf = dedupeSchemas(props.AllOf)
	props.AnyOf = dedupeSchemas(props.AnyOf)
	props.OneOf = dedupeSchemas(props.OneOf)

	if len(props.AllOf) == 1 {
		merged, ok, err := hoistSingleAllOf(*props, props.AllOf[0])
		if err != nil {
			return err
		}
		if ok {
			*props = merged
			return nil
		}
	}

	for i := range props.AllOf {
		if err := simplifyAllOf(&props.AllOf[i]); err != nil {
			return err
		}
	}
	for i := range props.AnyOf {
		if err := simplifyAllOf(&props.AnyOf[i]); err != nil {
			return err
		}
	}
	for i := range props.OneOf {
		if err := simplifyAllOf(&props.OneOf[i]); err != nil {
			return err
		}
	}
	if props.Not != nil {
		if err := simplifyAllOf(props.Not); err != nil {
			return err
		}
	}
	if props.Items != nil {
		if props.Items.Schema != nil {
			if err := simplifyAllOf(props.Items.Schema); err != nil {
				return err
			}
		}
		for i := range props.Items.JSONSchemas {
			if err := simplifyAllOf(&props.Items.JSONSchemas[i]); err != nil {
				return err
			}
		}
	}
	if props.AdditionalProperties != nil && props.AdditionalProperties.Schema != nil {
		if err := simplifyAllOf(props.AdditionalProperties.Schema); err != nil {
			return err
		}
	}
	for name := range props.Properties {
		prop := props.Properties[name]
		if err := simplifyAllOf(&prop); err != nil {
			return err
		}
		props.Properties[name] = prop
	}

	return nil
}

// dedupeSchemas removes entries from in that are deep-equal to an earlier
// entry. The order of the remaining entries is preserved.
func dedupeSchemas(in []apiext.JSONSchemaProps) []apiext.JSONSchemaProps {
	out := make([]apiext.JSONSchemaProps, 0, len(in))
	for _, schema := range in {
		duplicate := false
		for _, prev := range out {
			if reflect.DeepEqual(prev, schema) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, schema)
		}
	}
	return out
}

// hoistSingleAllOf merges the single remaining allOf entry of parent into
// parent itself. Fields that the parent does not set are moved up, fields
// equal to the parent's are dropped. If the entry conflicts with the parent,
// hoisting is refused and the allOf is kept.
func hoistSingleAllOf(parent apiext.JSONSchemaProps, entry apiext.JSONSchemaProps) (apiext.JSONSchemaProps, bool, error) {
	parentMap, err := schemaToMap(parent)
	if err != nil {
		return apiext.JSONSchemaProps{}, false, fmt.Errorf("failed to serialize parent schema: %w", err)
	}

	entryMap, err := schemaToMap(entry)
	if err != nil {
		return apiext.JSONSchemaProps{}, false, fmt.Errorf("failed to serialize allOf entry: %w", err)
	}

	// The entry is being consumed, so the parent's allOf key is dropped.
	delete(parentMap, "allOf")

	for key, entryVal := range entryMap {
		parentVal, exists := parentMap[key]
		switch {
		case !exists:
			parentMap[key] = entryVal
		case reflect.DeepEqual(parentVal, entryVal):
			// Same value, drop the entry's copy.
		default:
			// Real conflict between the parent and the entry.
			return apiext.JSONSchemaProps{}, false, nil
		}
	}

	merged, err := mapToSchema(parentMap)
	if err != nil {
		return apiext.JSONSchemaProps{}, false, fmt.Errorf("failed to deserialize merged schema: %w", err)
	}

	return merged, true, nil
}

// schemaToMap converts a schema to a generic map via JSON round-trip.
func schemaToMap(schema apiext.JSONSchemaProps) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}

	return out, nil
}

// mapToSchema converts a generic map back to a schema via JSON round-trip.
func mapToSchema(in map[string]any) (apiext.JSONSchemaProps, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return apiext.JSONSchemaProps{}, fmt.Errorf("failed to marshal schema map: %w", err)
	}

	var out apiext.JSONSchemaProps
	if err := json.Unmarshal(data, &out); err != nil {
		return apiext.JSONSchemaProps{}, fmt.Errorf("failed to unmarshal schema map: %w", err)
	}

	return out, nil
}
