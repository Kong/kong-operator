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
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
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
// gateway, its data plane certificates, its entities, then the gateways
// gw-admin must serve. It first asks the pod for the entities and the
// certificates of the gateway, and only writes what differs from what the pod
// holds: the entities, along with the version of the configuration, and the
// certificates it returns, and the expected gateways recorded of it. The
// certificates it holds that the configuration doesn't, e.g. one deleted,
// left out or moved to another gateway, or one of a gateway deleted earlier
// under the same id, are deleted. A pod that doesn't know the gateway, e.g.
// one restarted, as gw-admin keeps nothing across a restart, gets it all; one that knows it has it registered already, under
// its name and creation time, which don't change. Every call is idempotent.
// What the pod holds of each certificate once pushed is told to outcome.
//
// The certificates come before the entities: an entity gw-admin rejects
// doesn't keep them from being trusted, nor reported Programmed. The expected
// gateways come last: a gw-admin pod turns ready once they are set, as long
// as each has a data plane certificate, so a pod restarted must hold the
// entities and the certificates of the gateway by then. A certificate
// gw-admin rejects is trusted by no pod, and only reported on it (see
// certificateOutcome): it doesn't fail the push, which the rest of the
// configuration doesn't depend on.
func (r *Reconciler) push(
	ctx context.Context,
	target *gwadminsync.Target,
	pod gwadminsync.Pod,
	gwAdmin gwadminapi.ClientInterface,
	config gatewayConfig,
	cfg configuration,
	gatewayID uuid.UUID,
	expected []uuid.UUID,
	outcome *certificateOutcome,
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
	// Read for a gateway just registered too: gw-admin keeps the
	// certificates of a gateway it deletes, and trusts them again once the
	// gateway is registered again under the same id, e.g. once it may use
	// its GWAdmin again. They are removed before the pod may serve it.
	certificates, err := getCertificates(ctx, gwAdmin, gatewayID)
	if err != nil {
		return err
	}
	if err := pushCertificates(ctx, pod, gwAdmin, gatewayID, cfg, certificates, outcome); err != nil {
		return err
	}
	same, err := sameEntities(held.Entities, cfg.pushedKey)
	if err != nil {
		return err
	}
	if !found || !same || held.ConfigVersion != config.Version {
		if err := gwadminsync.CallDescribingFields("push the entities of the gateway", cfg.describeField, func() (*http.Response, error) {
			return gwAdmin.UpdateEventGatewayEntities(ctx, gatewayID, gwadminapi.GatewayEntities{ConfigVersion: config.Version, Entities: config.Entities})
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

// pushCertificates makes the gw-admin pod hold the data plane certificates
// of the configuration, as they are, and only them: those it holds
// otherwise, given, are deleted first, then those it doesn't hold as they
// are registered, telling outcome which it holds once done, and which it
// rejects: such a certificate isn't trusted with what it held before either.
func pushCertificates(
	ctx context.Context, pod gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface, gatewayID uuid.UUID, cfg configuration,
	held map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem, outcome *certificateOutcome,
) error {
	if err := removeUnheld(ctx, gwAdmin, gatewayID, cfg, held, false); err != nil {
		return err
	}
	for _, cert := range cfg.certificates {
		current, ok := held[cert.id]
		if ok && sameCertificate(current, cert.body) {
			outcome.hold(cert, pod)
			continue
		}
		err := gwadminsync.Call(fmt.Sprintf("register the data plane certificate %s", cert.key), func() (*http.Response, error) {
			return gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, gatewayID, cert.id, cert.body)
		})
		switch {
		case err == nil:
			outcome.hold(cert, pod)
		case rejectedByGWAdmin(err):
			if ok {
				if err := removeCertificate(ctx, gwAdmin, gatewayID, cert.id, cert.key.String()); err != nil {
					return err
				}
			}
			outcome.reject(cert, pod, err)
		default:
			return err
		}
	}
	return nil
}

// removeUnconfigured makes the gw-admin pod stop trusting the data plane
// certificates of the gateway the configuration doesn't hold as they are,
// e.g. a terminating one, which isn't pushed to: a certificate replaced in
// its Secret, e.g. a compromised one, is removed, not registered again.
func removeUnconfigured(ctx context.Context, gwAdmin gwadminapi.ClientInterface, gatewayID uuid.UUID, cfg configuration) error {
	held, err := getCertificates(ctx, gwAdmin, gatewayID)
	if err != nil {
		return err
	}
	return removeUnheld(ctx, gwAdmin, gatewayID, cfg, held, true)
}

// removeUnheld deletes those of the data plane certificates the gw-admin pod
// holds, given, that the configuration doesn't, and, when changed is set,
// those it holds otherwise than the configuration does.
func removeUnheld(
	ctx context.Context, gwAdmin gwadminapi.ClientInterface, gatewayID uuid.UUID, cfg configuration,
	held map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem, changed bool,
) error {
	configured := make(map[uuid.UUID]dataPlaneCertificate, len(cfg.certificates))
	for _, cert := range cfg.certificates {
		configured[cert.id] = cert
	}
	for _, id := range slices.SortedFunc(maps.Keys(held), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }) {
		if cert, ok := configured[id]; ok && (!changed || sameCertificate(held[id], cert.body)) {
			continue
		}
		if err := removeCertificate(ctx, gwAdmin, gatewayID, id, "with id "+id.String()); err != nil {
			return err
		}
	}
	return nil
}

// sameCertificate tells whether the data plane certificate gw-admin holds is
// the one the request registers.
func sameCertificate(held gwadminapi.EventGatewayDataPlaneCertificateOnPrem, body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem) bool {
	return held.Certificate == body.Certificate && held.CreatedAt.Equal(body.CreatedAt) &&
		lo.FromPtr(held.Name) == lo.FromPtr(body.Name) && lo.FromPtr(held.Description) == lo.FromPtr(body.Description)
}

// rejectedByGWAdmin tells gw-admin rejected what the call sent, e.g. a
// created_at it doesn't accept: sending it again gets the same answer.
func rejectedByGWAdmin(err error) bool {
	respErr, ok := errors.AsType[gwadminsync.ResponseError](err)
	return ok && (respErr.StatusCode == http.StatusBadRequest || respErr.StatusCode == http.StatusUnprocessableEntity)
}

// removeCertificate makes the gw-admin pod stop trusting the data plane
// certificate of the gateway, named name in errors. A certificate gw-admin
// doesn't know is removed already.
func removeCertificate(ctx context.Context, gwAdmin gwadminapi.ClientInterface, gatewayID, id uuid.UUID, name string) error {
	err := gwadminsync.Call("remove the data plane certificate "+name, func() (*http.Response, error) {
		return gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, gatewayID, id)
	})
	if err != nil && !gwadminsync.IsNotFound(err) {
		return err
	}
	return nil
}

// getCertificates returns the data plane certificates gw-admin holds for the
// gateway, by id: none for a gateway it doesn't know.
func getCertificates(
	ctx context.Context, gwAdmin gwadminapi.ClientInterface, gatewayID uuid.UUID,
) (map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem, error) {
	const action = "read the data plane certificates of the gateway"
	resp, err := gwAdmin.ListEventGatewayDataPlaneCertificates(ctx, gatewayID)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := gwadminsync.CheckResponse(action, resp, nil)
		switch {
		case gwadminsync.IsNotFound(err):
			return nil, nil
		case err == nil:
			return nil, fmt.Errorf("failed to %s: gw-admin returned %s, not %d", action, resp.Status, http.StatusOK)
		}
		return nil, err
	}
	parsed, err := gwadminapi.ParseListEventGatewayDataPlaneCertificatesResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", action, err)
	}
	if parsed.JSON200 == nil {
		return nil, fmt.Errorf("failed to %s: gw-admin returned %s without a JSON body", action, resp.Status)
	}
	held := make(map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem, len(parsed.JSON200.Data))
	for _, cert := range parsed.JSON200.Data {
		held[cert.Id] = cert
	}
	return held, nil
}

// certificateOutcome is what a push tells of the data plane certificates of
// the configuration, on each gw-admin pod it reaches: which pods hold each as
// it is, and which rejected it, with the reason. The pods of a push are
// pushed to at once. A nil one is told nothing.
type certificateOutcome struct {
	mu sync.Mutex
	// heldBy holds, by certificate id, the keys of the pods holding it.
	heldBy map[uuid.UUID]map[string]bool
	// rejectedBy holds, by certificate id, the reason of each pod, by name,
	// that rejected it.
	rejectedBy map[uuid.UUID]map[string]string
	// failedOn holds, by key, the pods whose push failed: one that doesn't
	// hold a certificate may have failed before it got to it.
	failedOn map[string]bool
}

func newCertificateOutcome() *certificateOutcome {
	return &certificateOutcome{heldBy: map[uuid.UUID]map[string]bool{}, rejectedBy: map[uuid.UUID]map[string]string{}, failedOn: map[string]bool{}}
}

// fail tells the push to the pod failed.
func (o *certificateOutcome) fail(pod gwadminsync.Pod) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.failedOn[pod.Key()] = true
}

// hold tells the pod holds the certificate, as it is.
func (o *certificateOutcome) hold(cert dataPlaneCertificate, pod gwadminsync.Pod) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.heldBy[cert.id] == nil {
		o.heldBy[cert.id] = map[string]bool{}
	}
	o.heldBy[cert.id][pod.Key()] = true
}

// reject tells the pod rejected the certificate, with the error.
func (o *certificateOutcome) reject(cert dataPlaneCertificate, pod gwadminsync.Pod, err error) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rejectedBy[cert.id] == nil {
		o.rejectedBy[cert.id] = map[string]string{}
	}
	o.rejectedBy[cert.id][pod.Name] = err.Error()
}

// held reports whether every gw-admin pod of the target the push didn't find
// pending holds the certificate, and names those that don't, those of them
// whose push failed apart, as they may hold it still, and those that
// rejected it, with the reason. A pod pending, e.g. starting, or on a node
// cut off, serves no data plane: it gets every certificate before it turns
// ready (see push).
func (o *certificateOutcome) held(
	target *gwadminsync.Target, pending []gwadminsync.Pod, cert dataPlaneCertificate,
) (held bool, missing, unconfirmed []string, rejected map[string]string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	reached := 0
	for _, pod := range target.Pods {
		if slices.Contains(pending, pod) {
			continue
		}
		reached++
		switch {
		case o.heldBy[cert.id][pod.Key()]:
		case o.failedOn[pod.Key()]:
			unconfirmed = append(unconfirmed, pod.Name)
		default:
			missing = append(missing, pod.Name)
		}
	}
	return reached > 0 && len(missing) == 0 && len(unconfirmed) == 0, missing, unconfirmed, o.rejectedBy[cert.id]
}

// getEntities returns the entities gw-admin holds for the gateway, with the
// version of the configuration they were sent with, and whether it knows the
// gateway at all.
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
func noEntities() gwadminapi.GatewayEntitySet {
	return gwadminapi.GatewayEntitySet{
		BackendClusters:  []gwadminapi.EventGatewayBackendClusterOnPrem{},
		Listeners:        []gwadminapi.EventGatewayListenerOnPrem{},
		SchemaRegistries: []gwadminapi.EventGatewaySchemaRegistryOnPrem{},
		StaticKeys:       []gwadminapi.EventGatewayStaticKeyOnPrem{},
		TlsTrustBundles:  []gwadminapi.EventGatewayTLSTrustBundleOnPrem{},
		VirtualClusters:  []gwadminapi.EventGatewayVirtualClusterOnPrem{},
	}
}

// removeGateway makes the gw-admin pod stop serving the gateway: it is left
// out of the gateways gw-admin serves, then deleted with its entities and its
// data plane certificates. It is sent in full every time: it only runs when
// the gateway is deleted or may not use its GWAdmin anymore, once.
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
