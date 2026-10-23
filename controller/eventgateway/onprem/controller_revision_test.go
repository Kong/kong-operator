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
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

func TestGatewayConfigHash(t *testing.T) {
	gateway := newTestGateway("gateway")
	config := deriveConfig(gateway)
	hash, err := config.hash()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "sha256:"), hash)

	again, err := deriveConfig(gateway).hash()
	require.NoError(t, err)
	assert.Equal(t, hash, again, "the same configuration, the same hash")

	withNil := config
	withNil.Entities = gwadminapi.GatewayEntities{}
	nilHash, err := withNil.hash()
	require.NoError(t, err)
	assert.Equal(t, hash, nilHash, "a list missing and an empty one are the same")

	renamed := deriveConfig(newTestGateway("other"))
	renamedHash, err := renamed.hash()
	require.NoError(t, err)
	assert.NotEqual(t, hash, renamedHash)

	withEntity := config
	withEntity.Entities.BackendClusters = []gwadminapi.EventGatewayBackendClusterOnPrem{{Name: "cluster"}}
	entityHash, err := withEntity.hash()
	require.NoError(t, err)
	assert.NotEqual(t, hash, entityHash)
	assert.Empty(t, config.Entities.BackendClusters, "hashing doesn't change the configuration")
}

func TestRecordRevision(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	config := deriveConfig(f.gateway)
	hash, err := config.hash()
	require.NoError(t, err)

	gateway := read(t, f.cl, f.gateway)
	require.NoError(t, f.r.recordRevision(t.Context(), gateway, config))
	stored := read(t, f.cl, f.gateway)
	assert.Equal(t, int64(1), stored.Status.ConfigRevision, "the first revision")
	assert.Equal(t, hash, stored.Status.ConfigHash)
	assert.Equal(t, stored.Status, gateway.Status, "the gateway given is updated")

	require.NoError(t, f.r.recordRevision(t.Context(), stored, config))
	assert.Equal(t, stored.ResourceVersion, read(t, f.cl, f.gateway).ResourceVersion, "the same configuration isn't recorded again")

	stale := stored.DeepCopy()
	changed := deriveConfig(f.gateway)
	changed.Entities.Listeners = []gwadminapi.EventGatewayListenerOnPrem{{Name: "listener"}}
	require.NoError(t, f.r.recordRevision(t.Context(), stored, changed))
	assert.Equal(t, int64(2), read(t, f.cl, f.gateway).Status.ConfigRevision, "a new configuration, a new revision")

	// A gateway read from a stale cache would reuse revision 2.
	err = f.r.recordRevision(t.Context(), stale, deriveConfig(newTestGateway("other")))
	require.True(t, apierrors.IsConflict(err), "a stale gateway conflicts: %v", err)
	assert.Equal(t, int64(1), stale.Status.ConfigRevision, "its status is left as read")
	assert.Equal(t, int64(2), read(t, f.cl, f.gateway).Status.ConfigRevision)
}

func TestReconcileRecordsTheRevisionBeforePushing(t *testing.T) {
	f := newPushFixture(t, interceptor.Funcs{}, nil)
	var revisionsSeen []int64
	f.pod().respond = func(call string) (*reply, error) {
		if call == callRegister {
			revisionsSeen = append(revisionsSeen, read(t, f.cl, f.gateway).Status.ConfigRevision)
		}
		return nil, nil
	}
	_, err := f.r.Reconcile(t.Context(), f.gateway)
	require.NoError(t, err)
	assert.Equal(t, []int64{1}, revisionsSeen, "the revision is stored before gw-admin is pushed it")
	gateway := read(t, f.cl, f.gateway)
	assert.Equal(t, int64(1), gateway.Status.ConfigRevision)
	assert.NotEmpty(t, gateway.Status.ConfigHash)

	_, err = f.r.Reconcile(t.Context(), gateway)
	require.NoError(t, err)
	assert.Equal(t, int64(1), read(t, f.cl, f.gateway).Status.ConfigRevision, "kept while the configuration is the same")

	// The status the reconcile applies leaves the revision as it is.
	f.pod().respond = failing(callGetEntities, http.StatusInternalServerError)
	_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
	require.NoError(t, err)
	gateway = read(t, f.cl, f.gateway)
	assert.Equal(t, int64(1), gateway.Status.ConfigRevision)
	assert.NotEmpty(t, gateway.Status.ConfigHash)
}
