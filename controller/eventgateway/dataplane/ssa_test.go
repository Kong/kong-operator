/*
Copyright 2025 Kong, Inc.

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

package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// fakeManagerWithConfig implements only ctrl.Manager.GetConfig(), which is the
// sole Manager method used by initTypeConverter / NewTypeConverter.
type fakeManagerWithConfig struct {
	ctrl.Manager

	config *rest.Config
}

func (f fakeManagerWithConfig) GetConfig() *rest.Config {
	return f.config
}

func Test_requiredSchemas(t *testing.T) {
	expected := []schema.GroupVersion{
		{Group: "", Version: "v1"},
		{Group: "apps", Version: "v1"},
		{Group: "eventgateway.konghq.com", Version: "v1alpha1"},
	}
	assert.Equal(t, expected, requiredSchemas)
}

func Test_initTypeConverter_errorPropagated(t *testing.T) {
	// A config pointing at a port with nothing listening causes Paths() to fail
	// with a connection-refused error, which initTypeConverter must surface.
	mgr := fakeManagerWithConfig{
		config: &rest.Config{Host: "http://127.0.0.1:0"},
	}
	_, err := initTypeConverter(mgr)
	require.Error(t, err)
}
