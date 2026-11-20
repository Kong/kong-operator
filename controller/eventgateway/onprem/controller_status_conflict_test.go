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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsFieldManagerConflict(t *testing.T) {
	applyConflict := apierrors.NewApplyConflict([]metav1.StatusCause{{
		Type:    metav1.CauseTypeFieldManagerConflict,
		Message: `conflict with "someone-else"`,
		Field:   ".status.conditions[type=\"Programmed\"].reason",
	}}, "Apply failed with 1 conflict")
	staleVersion := apierrors.NewConflict(schema.GroupResource{Group: "configuration.konghq.com", Resource: "eventgatewaystatickeys"},
		"key", errors.New("the object has been modified; please apply your changes to the latest version and try again"))

	assert.True(t, isFieldManagerConflict(applyConflict), "fields someone else owns")
	assert.True(t, isFieldManagerConflict(fmt.Errorf("wrapped: %w", applyConflict)), "wrapped")
	assert.False(t, isFieldManagerConflict(staleVersion), "an object changed since it was read")
	assert.False(t, isFieldManagerConflict(apierrors.NewNotFound(schema.GroupResource{}, "key")))
	assert.False(t, isFieldManagerConflict(errors.New("other")))
	assert.False(t, isFieldManagerConflict(nil))
}
