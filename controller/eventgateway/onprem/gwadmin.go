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
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
)

// gwAdminNotReadyError tells the GWAdmin of a gateway can't be pushed to yet.
// It is not retried: a change of the GWAdmin, of a KongReferenceGrant of its
// namespace, of its Secrets or of its pods triggers the gateway again.
type gwAdminNotReadyError struct {
	msg string
	// reason is the reason of the GWAdminResolved condition of the gateway:
	// NotReady when empty.
	reason kcfgconsts.ConditionReason
	// nothingToRemove is true when no gw-admin pod Kong Operator could tell
	// to stop serving the gateway serves it (see
	// gwadminsync.NotReadyError.NothingToRemove).
	nothingToRemove bool
	// notAllowed is true when the gateway may not use the GWAdmin.
	notAllowed bool
	// gwAdmin is the GWAdmin, when it exists.
	gwAdmin *operatorv1alpha1.GWAdmin
}

func (e gwAdminNotReadyError) Error() string { return e.msg }

// resolvedReason returns the reason of the GWAdminResolved condition of the
// gateway.
func (e gwAdminNotReadyError) resolvedReason() kcfgconsts.ConditionReason {
	return cmp.Or(e.reason, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason)
}

// resolveGWAdmin returns the gw-admin of the gateway. forPush tells the
// configuration is pushed: the gateway must be allowed to use the GWAdmin,
// and the GWAdmin must not be being deleted. Otherwise the pods are only told
// to stop serving the gateway, which a GWAdmin being deleted still does.
func (r *Reconciler) resolveGWAdmin(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, forPush bool) (*gwadminsync.Target, error) {
	mode := gwadminsync.ForRemoval
	if forPush {
		reason, err := gwadminref.NotAllowed(ctx, r.Client, gateway)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return nil, gwAdminNotReadyError{
				msg:        "The gateway may not use its GWAdmin: " + reason,
				reason:     eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason,
				notAllowed: true,
			}
		}
		mode = gwadminsync.ForPush
	}
	target, err := r.GWAdminSync.Resolve(ctx, gateway.GWAdminKey(), mode)
	if notReady, ok := errors.AsType[gwadminsync.NotReadyError](err); ok {
		var reason kcfgconsts.ConditionReason
		if notReady.Cause == gwadminsync.CauseNotFound {
			reason = eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason
		}
		return nil, gwAdminNotReadyError{
			msg: notReady.Message, reason: reason, nothingToRemove: notReady.NothingToRemove, gwAdmin: notReady.GWAdmin,
		}
	}
	return target, err
}

// itemExpected is the item recording the expected gateways set on a gw-admin
// pod (gwadminsync.Syncer.Record), as their idsKey: gw-admin can't be asked
// for them.
const itemExpected = "expected"

// push makes the gw-admin pod serve the configuration of the gateway: the
// gateway, its entities, then the gateways gw-admin must serve. It first asks
// the pod for the entities of the gateway, and only writes what differs from
// what the pod holds: the entities it returns, and the expected gateways
// recorded of the pod. A pod that doesn't know the gateway, e.g. one
// restarted, as gw-admin keeps nothing across a restart, gets it all; one
// that knows it has it registered already, under its name and creation time,
// which don't change. Every call is idempotent.
//
// The expected gateways come last: a gw-admin pod turns ready once they are
// set, as long as each has a data plane certificate, so a pod restarted must
// hold the entities of the gateway by then.
func (r *Reconciler) push(
	ctx context.Context,
	target *gwadminsync.Target,
	pod gwadminsync.Pod,
	gwAdmin gwadminapi.ClientInterface,
	config gatewayConfig,
	cfg configuration,
	gatewayID uuid.UUID,
	expected []uuid.UUID,
) error {
	held, found, err := getEntities(ctx, gwAdmin, gatewayID)
	if err != nil {
		return err
	}

	if !found {
		if err := gwadminsync.Call("register the gateway", func() (*http.Response, error) {
			return gwAdmin.UpdateEventGateway(ctx, gatewayID, config.Gateway)
		}); err != nil {
			return err
		}
	}
	same, err := sameEntities(held, cfg.pushedKey)
	if err != nil {
		return err
	}
	if !found || !same {
		if err := gwadminsync.CallDescribingFields("push the entities of the gateway", cfg.describeField, func() (*http.Response, error) {
			return gwAdmin.UpdateEventGatewayEntities(ctx, gatewayID, config.Entities)
		}); err != nil {
			return err
		}
	}
	// A pod that doesn't know the gateway may have lost the expected gateways
	// too.
	if !found || r.GWAdminSync.Recorded(target, pod, itemExpected) != idsKey(expected) {
		if err := r.setExpected(ctx, target, pod, gwAdmin, expected); err != nil {
			return err
		}
	}
	return nil
}

// getEntities returns the entities gw-admin holds for the gateway, and
// whether it knows the gateway at all.
func getEntities(ctx context.Context, gwAdmin gwadminapi.ClientInterface, gatewayID uuid.UUID) (gwadminapi.GatewayEntities, bool, error) {
	const action = "read the entities of the gateway"
	resp, err := gwAdmin.GetEventGatewayEntities(ctx, gatewayID)
	if err != nil {
		return gwadminapi.GatewayEntities{}, false, fmt.Errorf("failed to %s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := gwadminsync.CheckResponse(action, resp, nil)
		switch {
		case gwadminsync.IsNotFound(err):
			return gwadminapi.GatewayEntities{}, false, nil
		case err == nil:
			return gwadminapi.GatewayEntities{}, false, fmt.Errorf("failed to %s: gw-admin returned %s, not %d", action, resp.Status, http.StatusOK)
		}
		return gwadminapi.GatewayEntities{}, false, err
	}
	parsed, err := gwadminapi.ParseGetEventGatewayEntitiesResponse(resp)
	if err != nil {
		return gwadminapi.GatewayEntities{}, false, fmt.Errorf("failed to %s: %w", action, err)
	}
	if parsed.JSON200 == nil {
		return gwadminapi.GatewayEntities{}, false, fmt.Errorf("failed to %s: gw-admin returned %s without a JSON body", action, resp.Status)
	}
	return *parsed.JSON200, true, nil
}

// sameEntities reports whether gw-admin holds the entities pushed, pushedKey
// being their comparisonKey once withGWAdminDefaults applied: a field left
// out and the default gw-admin sets on it are the same.
func sameEntities(held gwadminapi.GatewayEntities, pushedKey []byte) (bool, error) {
	heldKey, err := comparisonKey(held)
	if err != nil {
		return false, fmt.Errorf("failed to encode the entities gw-admin holds: %w", err)
	}
	return bytes.Equal(heldKey, pushedKey), nil
}

// comparisonKey returns the entities as sameEntities compares them: their
// JSON encoding, as gw-admin receives them, as the entities hold times, on
// whose unexported fields equality.Semantic.DeepEqual panics. Each list is
// sorted by id, as its order doesn't matter, and a missing list is empty.
func comparisonKey(e gwadminapi.GatewayEntities) ([]byte, error) {
	return json.Marshal(sortedByID(withEmptyLists(e)))
}

// sortedByID returns the entities with each list sorted by id.
func sortedByID(e gwadminapi.GatewayEntities) gwadminapi.GatewayEntities {
	e.BackendClusters = slices.SortedFunc(slices.Values(e.BackendClusters), func(a, b gwadminapi.EventGatewayBackendClusterOnPrem) int {
		return bytes.Compare(a.Id[:], b.Id[:])
	})
	return e
}

// withGWAdminDefaults returns the entities with the value gw-admin sets on
// each field left out, as it returns them: otherwise they would be pushed
// again on every reconcile. A default missing here has the same effect.
func withGWAdminDefaults(e gwadminapi.GatewayEntities) gwadminapi.GatewayEntities {
	e.BackendClusters = slices.Clone(e.BackendClusters)
	for i := range e.BackendClusters {
		if tls := &e.BackendClusters[i].Tls; tls.TlsVersions == nil {
			tls.TlsVersions = &[]gwadminapi.BackendClusterTLSTlsVersions{gwadminapi.Tls12, gwadminapi.Tls13}
		}
	}
	return e
}

// idsKey returns the ids, sorted, as one string, to tell whether the
// expected gateways changed since they were set.
func idsKey(ids []uuid.UUID) string {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, id.String())
	}
	slices.Sort(keys)
	return strings.Join(keys, ",")
}

// noEntities returns the entities of a gateway that has none: gw-admin
// requires every list.
func noEntities() gwadminapi.GatewayEntities {
	return gwadminapi.GatewayEntities{
		BackendClusters:  []gwadminapi.EventGatewayBackendClusterOnPrem{},
		Listeners:        []gwadminapi.EventGatewayListenerOnPrem{},
		SchemaRegistries: []gwadminapi.EventGatewaySchemaRegistryOnPrem{},
		StaticKeys:       []gwadminapi.EventGatewayStaticKeyOnPrem{},
		TlsTrustBundles:  []gwadminapi.EventGatewayTLSTrustBundleOnPrem{},
		VirtualClusters:  []gwadminapi.EventGatewayVirtualClusterOnPrem{},
	}
}

// removeGateway makes the gw-admin pod stop serving the gateway: it is left
// out of the gateways gw-admin serves, then deleted with its entities. It is
// sent in full every time: it only runs when the gateway is deleted or may
// not use its GWAdmin anymore, once.
func (r *Reconciler) removeGateway(
	ctx context.Context,
	target *gwadminsync.Target,
	pod gwadminsync.Pod,
	gwAdmin gwadminapi.ClientInterface,
	gatewayID uuid.UUID,
	expected []uuid.UUID,
) error {
	if err := r.setExpected(ctx, target, pod, gwAdmin, expected); err != nil {
		return err
	}
	err := gwadminsync.Call("delete the gateway", func() (*http.Response, error) {
		return gwAdmin.DeleteEventGateway(ctx, gatewayID)
	})
	// A gateway gw-admin doesn't know is deleted already.
	if err != nil && !gwadminsync.IsNotFound(err) {
		return err
	}
	return nil
}

// setExpected sets the gateways the gw-admin pod serves, and records them.
// They are unrecorded first: a call that fails may still be applied, and what
// was recorded before isn't what the pod holds anymore.
func (r *Reconciler) setExpected(
	ctx context.Context, target *gwadminsync.Target, pod gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface, expected []uuid.UUID,
) error {
	r.GWAdminSync.Unrecord(target, pod, itemExpected)
	if err := gwadminsync.Call("set the gateways gw-admin serves", func() (*http.Response, error) {
		return gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: expected})
	}); err != nil {
		return err
	}
	r.GWAdminSync.Record(target, pod, itemExpected, idsKey(expected))
	return nil
}

// uidV4 returns the UID of the object as a version 4 UUID, which gw-admin
// requires as an id.
func uidV4(obj client.Object) (uuid.UUID, bool) {
	id, err := uuid.Parse(string(obj.GetUID()))
	return id, err == nil && id.Version() == 4
}

// createdAt returns the creation time of the object, as gw-admin requires it:
// UTC, whole seconds.
func createdAt(obj client.Object) time.Time {
	return obj.GetCreationTimestamp().UTC().Truncate(time.Second)
}
