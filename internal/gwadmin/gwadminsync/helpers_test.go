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

package gwadminsync

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// ns is the namespace of the GWAdmins of the tests, and the operator
// namespace.
const ns = "default"

// newFakeClient returns a fake client with the objects, its calls going
// through funcs.
func newFakeClient(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.WithWatch {
	t.Helper()
	return fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

// fakeAPIClient is a gw-admin pod, by its address.
type fakeAPIClient struct {
	gwadminapi.ClientInterface

	address string
}
