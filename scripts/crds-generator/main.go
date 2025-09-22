// This script is responsible for generating CRDs.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
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
		"github.com/kong/kong-operator/api/configuration/v1",
		"github.com/kong/kong-operator/api/configuration/v1alpha1",
		"github.com/kong/kong-operator/api/configuration/v1beta1",

		// incubator.ingress-controller.konghq.com
		"github.com/kong/kong-operator/api/incubator/v1alpha1",

		// konnect.konghq.com
		"github.com/kong/kong-operator/api/konnect/v1alpha1",
		"github.com/kong/kong-operator/api/konnect/v1alpha2",

		// gateway-operator.konghq.com
		"github.com/kong/kong-operator/api/gateway-operator/v1alpha1",
		"github.com/kong/kong-operator/api/gateway-operator/v1beta1",
		"github.com/kong/kong-operator/api/gateway-operator/v2beta1",

		// common types
		"github.com/kong/kong-operator/api/common/v1alpha1",
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
		err = os.WriteFile(kustomizeFile, []byte(kustomizeContent), 0o600)
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
		case IngressControllerChannelType, GatewayOperatorChannelType:
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
		annotations, ok := metadata.(map[any]any)["annotations"]
		if !ok {
			annotations = map[string]any{}
			obj["metadata"].(map[string]any)["annotations"] = metadata
		}
		annotations.(map[any]any)[VersionAnnotation] = version
		return nil
	}
}
