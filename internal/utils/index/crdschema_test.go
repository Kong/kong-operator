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

package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCRDGroup(t *testing.T) {
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "widgets.example.com"},
		Spec:       apiextensionsv1.CustomResourceDefinitionSpec{Group: "example.com"},
	}
	assert.Equal(t, []string{"example.com"}, crdGroup(crd))
}

func TestOptionsForCRDSchema(t *testing.T) {
	options := OptionsForCRDSchema()
	require.Len(t, options, 1)
	opt := options[0]
	require.IsType(t, &apiextensionsv1.CustomResourceDefinition{}, opt.Object)
	require.Equal(t, IndexFieldCRDOnGroup, opt.Field)
	require.NotNil(t, opt.ExtractValueFn)
}
