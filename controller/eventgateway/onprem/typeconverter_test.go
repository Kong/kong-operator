/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package onprem

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"
)

// crdTypeConverter is the type converter of the Server-Side Apply of the
// status of the gateways and their entities in the fake client, built from
// their CRDs, as the API server does: the conditions are a list keyed by
// type, each owned by the field manager that applied it.
var crdTypeConverter = sync.OnceValue(func() managedfields.TypeConverter {
	dir := filepath.Join("..", "..", "..", "config", "crd", "kong-operator")
	files, err := os.ReadDir(dir)
	if err != nil {
		panic(fmt.Errorf("failed to read the CRDs: %w", err))
	}
	var specs []*spec3.OpenAPI
	for _, file := range files {
		name := file.Name()
		if !strings.HasPrefix(name, "configuration.konghq.com_eventgateway") && name != "eventgateway.konghq.com_onpremeventgateways.yaml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			panic(fmt.Errorf("failed to read the CRD %s: %w", name, err))
		}
		var crd apiextensionsv1.CustomResourceDefinition
		if err := yaml.Unmarshal(raw, &crd); err != nil {
			panic(fmt.Errorf("failed to decode the CRD %s: %w", name, err))
		}
		for _, v := range crd.Spec.Versions {
			s, err := aexbuilder.BuildOpenAPIV3(&crd, v.Name, aexbuilder.Options{})
			if err != nil {
				panic(fmt.Errorf("failed to build the OpenAPI schema of %s/%s: %w", crd.Name, v.Name, err))
			}
			specs = append(specs, s)
		}
	}
	merged, err := aexbuilder.MergeSpecsV3(specs...)
	if err != nil {
		panic(fmt.Errorf("failed to merge the OpenAPI schemas: %w", err))
	}
	schemas := map[string]*spec.Schema{}
	if merged.Components != nil {
		maps.Copy(schemas, merged.Components.Schemas)
	}
	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		panic(fmt.Errorf("failed to build the type converter: %w", err))
	}
	return tc
})
