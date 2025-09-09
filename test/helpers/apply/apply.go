package apply

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestlog "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/kong/kong-operator/pkg/utils/test"
)

var decUnstructured = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

// Template applies templated resources to the cluster using the given rest.Config.
func Template(t *testing.T, cfg *rest.Config, chartPath string, templates []string) {
	t.Helper()

	helmArgs := []string{
		"--api-versions",
		"admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy",
		"--api-versions",
		"admissionregistration.k8s.io/v1/ValidatingAdmissionPolicyBinding",
	}

	data := templateRenderer(t, chartPath, templates, helmArgs...)
	res, err := apply(t.Context(), cfg, []byte(data))
	require.NoError(t, err)
	for _, r := range res {
		t.Logf("Result: %s", r)
	}
}

func templateRenderer(t *testing.T, chartPath string, templates []string, helmArgs ...string) string {
	t.Helper()
	releaseName := "ko"
	valuesFile := path.Join(chartPath, "values.yaml")

	// Discard terratest stdout logging
	terratestlog.Default = terratestlog.Discard

	res := helm.RenderTemplate(t, &helm.Options{
		ValuesFiles: []string{valuesFile},
	}, chartPath, releaseName, templates, helmArgs...)

	return res
}

// Kustomization applies a kustomization to the cluster using the given rest.Config.
func Kustomization(ctx context.Context, t *testing.T, cfg *rest.Config, dir string) {
	t.Helper()

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fSys := filesys.MakeFsOnDisk()
	resmap, err := k.Run(fSys, path.Join(test.ProjectRootPath(), dir))
	require.NoError(t, err)

	b, err := resmap.AsYaml()
	require.NoError(t, err)

	res, err := apply(ctx, cfg, b)
	require.NoError(t, err)
	for _, r := range res {
		t.Logf("Result: %s", r)
	}
}

func apply(ctx context.Context, restConfig *rest.Config, data []byte) (result []string, err error) {
	chanMes, chanErr := readYaml(data)
	for {
		select {
		case dataBytes, ok := <-chanMes:
			{
				if !ok {
					return result, err
				}

				// Get obj and dr
				obj, dr, errClient := buildDynamicResourceClient(restConfig, dataBytes)
				if errClient != nil {
					err = errors.Join(errClient, err)
					continue
				}

				// Create or Update
				_, errPatch := dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, dataBytes, metav1.PatchOptions{
					FieldManager: "test",
				})

				if errPatch != nil {
					err = errors.Join(errPatch, err)
				} else {
					result = append(result, obj.GetName()+" applied.")
				}
			}
		case errChan, ok := <-chanErr:
			if !ok {
				return result, err
			}
			if errChan == nil {
				continue
			}
			err = errors.Join(errChan, err)
		}
	}
}

func readYaml(data []byte) (<-chan []byte, <-chan error) {
	var (
		chanErr        = make(chan error)
		chanBytes      = make(chan []byte)
		multidocReader = utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	)

	go func() {
		defer close(chanErr)
		defer close(chanBytes)

		for {
			buf, err := multidocReader.Read()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				chanErr <- fmt.Errorf("failed to read yaml data : %w", err)
				return
			}
			chanBytes <- buf
		}
	}()
	return chanBytes, chanErr
}

func buildDynamicResourceClient(restConfig *rest.Config, data []byte) (obj *unstructured.Unstructured, dr dynamic.ResourceInterface, err error) {
	// Decode YAML manifest into unstructured.Unstructured
	obj = &unstructured.Unstructured{}
	_, gvk, err := decUnstructured.Decode(data, nil, obj)
	if err != nil {
		return obj, dr, fmt.Errorf("Decode yaml failed.  : %w", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("new dc failed : %w", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	// Find GVR
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return obj, dr, fmt.Errorf("Mapping kind with version failed : %w", err)
	}

	// Prepare dynamic client
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return obj, dr, fmt.Errorf("Prepare dynamic client failed. : %w", err)
	}

	// Obtain REST interface for the GVR
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// namespaced resources should specify the namespace
		dr = dynamicClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		// for cluster-wide resources
		dr = dynamicClient.Resource(mapping.Resource)
	}
	return obj, dr, nil
}
