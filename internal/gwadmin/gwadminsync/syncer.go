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

// Package gwadminsync brings the gw-admin pods of a GWAdmin in line with
// what a controller wants them to serve. It finds the pods, reaches each of
// them over mutual TLS, calls them all at once, remembers what was written
// to each, and backs off pushes that failed. What is written is the
// controller's: e.g. the OnPremEventGateway controller registers gateways.
//
// Each controller creates its own Syncer: the state it keeps, e.g. the lock
// of a GWAdmin or what was written to a pod, is the controller's own. Each
// gw-admin resource is written by one controller.
package gwadminsync

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// Syncer reaches the gw-admin pods of GWAdmins for one controller. Its
// exported fields are set when it is created; the zero value of the rest is
// ready to use.
type Syncer struct {
	// Client reads the GWAdmins, their Secrets and their EndpointSlices.
	Client client.Client

	// OperatorNamespace is the namespace the GWAdmin controller keeps the
	// operator client certificates in.
	OperatorNamespace string

	// SecretLabelSelector is the label the operator's Secret cache selects
	// Secrets by, if any: a Secret without it isn't found.
	SecretLabelSelector string

	// DialContext dials the gw-admin pods. When nil, a net.Dialer is used.
	// The envtests set it to reach fake gw-admin pods on local listeners
	// through the real HTTP and TLS stack.
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	// NewClient returns a client of the gw-admin pod at server, over
	// httpClient. When nil, gwadminapi.NewClient is used. The unit tests set
	// it to fake the gw-admin pods.
	NewClient func(server string, httpClient *http.Client) (gwadminapi.ClientInterface, error)

	// notServingTimeout overrides how long a pod not serving gets to accept
	// a connection (see ForEachPod): the tests shorten it.
	notServingTimeout time.Duration

	locks      keyedLocks
	epochs     epochs
	transports transportCache
	records    podRecords

	// wakeups carries the waiters of a lock to wake (see TryLock).
	wakeups     chan event.GenericEvent
	wakeupsOnce sync.Once
}

// Mode tells what a Target is resolved for.
type Mode int

const (
	// ForPush resolves the pods to push to: the GWAdmin must not be being
	// deleted, and must have a pod that isn't terminating.
	ForPush Mode = iota
	// ForRemoval resolves the pods to tell to stop serving something: those
	// of a GWAdmin being deleted, and the terminating ones, are too.
	ForRemoval
)

// Target is the gw-admin of a GWAdmin: its pods, and how Kong Operator
// reaches them.
type Target struct {
	// GWAdmin is the GWAdmin.
	GWAdmin *operatorv1alpha1.GWAdmin
	// Pods are the pods that aren't terminating.
	Pods []Pod
	// Terminating are the terminating pods still serving: they are told to
	// stop serving something, not pushed to.
	Terminating []Pod
	// TLSFingerprint tells the TLS material the pods are reached with: it
	// changes when a certificate is renewed or a CA rotated.
	TLSFingerprint string

	tlsConfig *tls.Config
	// epoch is the epoch of the GWAdmin when it was resolved: what is kept
	// of it is only kept while it doesn't change.
	epoch uint64
}

// Key returns the key of the GWAdmin.
func (t *Target) Key() types.NamespacedName { return client.ObjectKeyFromObject(t.GWAdmin) }

// Pod is a gw-admin pod behind the API Service of a GWAdmin.
type Pod struct {
	// Name is the name of the pod.
	Name string
	// UID is the UID of the pod, when its endpoint tells it.
	UID types.UID
	// Address is the host and port the API listener of the pod is reached on.
	Address string
	// Serving tells the endpoint of the pod is serving. A gw-admin pod only
	// turns Ready, and serving, once provisioned: one that isn't may still be
	// starting, e.g. pulling its image, and not listen yet. The endpoints of
	// the API Service are all Ready, as it publishes the pods not Ready.
	Serving bool
}

// Key identifies the pod in what is recorded of it: a pod replaced, e.g. in
// a rollout, is another pod even under the same name.
func (p Pod) Key() string {
	if p.UID != "" {
		return string(p.UID)
	}
	return p.Name + "@" + p.Address
}

// Cause tells why the pods of a GWAdmin can't be reached.
type Cause string

const (
	// CauseNotFound tells the GWAdmin doesn't exist.
	CauseNotFound Cause = "NotFound"
	// CauseBeingDeleted tells the GWAdmin is being deleted.
	CauseBeingDeleted Cause = "BeingDeleted"
	// CauseAPINotPublished tells the GWAdmin hasn't published its API.
	CauseAPINotPublished Cause = "APINotPublished"
	// CauseTLSNotReady tells the TLS material of the GWAdmin isn't there or
	// is invalid.
	CauseTLSNotReady Cause = "TLSNotReady"
	// CauseNoPod tells the GWAdmin has no gw-admin pod to reach.
	CauseNoPod Cause = "NoPod"
)

// NotReadyError tells the pods of a GWAdmin can't be reached yet. Retrying
// doesn't help: a change of the GWAdmin, of its Secrets or of its pods does,
// which Watch triggers the controller on.
type NotReadyError struct {
	// Message tells why, as a sentence.
	Message string
	// Cause tells why.
	Cause Cause
	// NothingToRemove is true when no gw-admin pod Kong Operator could tell
	// to stop serving something serves anything: the GWAdmin has no pod, its
	// pods never got anything, or they are torn down along with the GWAdmin
	// and can't be reached anymore.
	NothingToRemove bool
	// GWAdmin is the GWAdmin, when it exists.
	GWAdmin *operatorv1alpha1.GWAdmin
}

func (e NotReadyError) Error() string { return e.Message }

// TryLock locks the GWAdmin for the controller, unless it is locked
// already, and returns the function unlocking it: e.g. to compute and write
// something gw-admin replaces as a whole, from the objects of a GWAdmin, one
// reconcile at a time. It doesn't wait: a reconcile that doesn't get the lock
// returns, so that it doesn't hold a worker of the controller, which the
// objects of other GWAdmins wait for, while another one calls gw-admin.
// Then waiter, the object reconciled, waits in line for the lock: each unlock
// requests a reconcile of the first one in line (see Watch). It should still
// be requeued, after a while, in case that request is lost, e.g. on a
// restart.
func (s *Syncer) TryLock(gwAdmin, waiter types.NamespacedName) (unlock func(), ok bool) {
	unlockKey, ok := s.locks.tryLock(gwAdmin, waiter)
	if !ok {
		return nil, false
	}
	return func() {
		if wake, ok := unlockKey(); ok {
			s.wake(wake)
		}
	}, true
}

// wakeupBuffer is how many waiters of a lock to wake are kept until the
// controller reconciles them: beyond, they are requeued anyway.
const wakeupBuffer = 1024

// wake requests a reconcile of the object, unless too many are waiting
// already: it is requeued anyway.
func (s *Syncer) wake(object types.NamespacedName) {
	select {
	case s.wakeupChannel() <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{
		Namespace: object.Namespace, Name: object.Name,
	}}:
	default:
	}
}

// wakeupChannel returns the channel of the waiters of a lock to wake.
func (s *Syncer) wakeupChannel() chan event.GenericEvent {
	s.wakeupsOnce.Do(func() { s.wakeups = make(chan event.GenericEvent, wakeupBuffer) })
	return s.wakeups
}

// Resolve returns the gw-admin of the GWAdmin. Each pod is reached on its
// address, and verified with the name of the API Service: gw-admin keeps
// nothing across a restart, so a new pod, e.g. one of a rollout, only turns
// Ready once provisioned, and calling through the Service would reach only
// one of them. It returns a NotReadyError when the pods can't be reached
// yet. What is recorded of a GWAdmin that doesn't exist is forgotten, and,
// once its pods are resolved, what is recorded of those gone.
func (s *Syncer) Resolve(ctx context.Context, key types.NamespacedName, mode Mode) (*Target, error) {
	epoch := s.epochs.current(key)
	var gw operatorv1alpha1.GWAdmin
	if err := s.Client.Get(ctx, key, &gw); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get the GWAdmin %s: %w", key, err)
		}
		s.forgetGWAdmin(key)
		// The pods of a deleted GWAdmin are collected after it: until then,
		// they may still serve. They can't be told anything: the GWAdmin
		// controller deleted the operator client certificate, named after
		// the UID of the GWAdmin, before letting the GWAdmin go.
		running, err := apiPodsRunning(ctx, s.Client, key)
		if err != nil {
			return nil, err
		}
		if running {
			return nil, NotReadyError{
				Message:         fmt.Sprintf("The GWAdmin %s does not exist, but its gw-admin pods are still running", key),
				Cause:           CauseNotFound,
				NothingToRemove: true,
			}
		}
		return nil, NotReadyError{
			Message:         fmt.Sprintf("The GWAdmin %s does not exist", key),
			Cause:           CauseNotFound,
			NothingToRemove: true,
		}
	}
	deleting := !gw.DeletionTimestamp.IsZero()
	if mode == ForPush && deleting {
		return nil, NotReadyError{Message: fmt.Sprintf("The GWAdmin %s is being deleted", key), Cause: CauseBeingDeleted, GWAdmin: &gw}
	}
	if gw.Status.API == nil || gw.Status.TLS == nil || gw.Status.TLS.APICABundleSecretName == "" {
		// Without its status, the pods of the GWAdmin can't be reached. They
		// got nothing, unless its status was lost: then they still run, and
		// the GWAdmin controller publishes its status again.
		running, err := apiPodsRunning(ctx, s.Client, key)
		if err != nil {
			return nil, err
		}
		return nil, NotReadyError{
			Message:         fmt.Sprintf("The GWAdmin %s hasn't published its API yet", key),
			Cause:           CauseAPINotPublished,
			NothingToRemove: !running,
			GWAdmin:         &gw,
		}
	}

	pods, terminating, err := gwAdminPods(ctx, s.Client, &gw)
	if err != nil {
		return nil, err
	}
	noPod := NotReadyError{Message: fmt.Sprintf("The GWAdmin %s has no gw-admin pod", key), Cause: CauseNoPod, NothingToRemove: true, GWAdmin: &gw}
	if mode == ForRemoval && len(pods) == 0 && len(terminating) == 0 {
		// gw-admin keeps nothing across a restart: with no pod, none serves
		// anything.
		return nil, noPod
	}
	tlsConfig, fingerprint, err := s.tlsConfig(ctx, &gw)
	if err != nil {
		if notReady, ok := errors.AsType[NotReadyError](err); ok {
			notReady.GWAdmin = &gw
			// The GWAdmin controller deletes the TLS material of a GWAdmin
			// being deleted, whose pods are torn down with it.
			notReady.NothingToRemove = deleting
			return nil, notReady
		}
		return nil, err
	}
	if mode == ForPush && len(pods) == 0 {
		return nil, noPod
	}
	// What was recorded of a pod gone, e.g. in a rollout, is forgotten.
	s.records.keepPods(key, podKeys(slices.Concat(pods, terminating)))
	return &Target{GWAdmin: &gw, Pods: pods, Terminating: terminating, TLSFingerprint: fingerprint, tlsConfig: tlsConfig, epoch: epoch}, nil
}

// podKeys returns the keys of the pods.
func podKeys(pods []Pod) []string {
	keys := make([]string, 0, len(pods))
	for _, pod := range pods {
		keys = append(keys, pod.Key())
	}
	return keys
}

// Recorded returns what was recorded of the item on the pod of the target,
// or "" when nothing was: e.g. a fingerprint of what was written to it, to
// write it again only when it changes. A write that fails may still have
// been applied by gw-admin: the item is unrecorded before it is written, and
// recorded again once it succeeds, so that a record is never trusted after a
// failed write.
func (s *Syncer) Recorded(target *Target, pod Pod, item string) string {
	return s.records.get(target.Key(), pod.Key(), item)
}

// Record records the value of the item on the pod of the target, once it is
// written to it, unless what is kept of the GWAdmin was forgotten since it
// was resolved, e.g. once it is deleted.
func (s *Syncer) Record(target *Target, pod Pod, item, value string) {
	s.epochs.keep(target.Key(), target.epoch, func() {
		s.records.set(target.Key(), pod.Key(), item, value)
	})
}

// Unrecord forgets the item on the pod of the target, e.g. once deleted from
// it.
func (s *Syncer) Unrecord(target *Target, pod Pod, item string) {
	s.records.unset(target.Key(), pod.Key(), item)
}

// forgetGWAdmin forgets what is kept of the GWAdmin: the connections to its
// pods, and what was recorded of them.
func (s *Syncer) forgetGWAdmin(key types.NamespacedName) {
	s.epochs.forget(key, func() {
		s.transports.forget(key)
		s.records.forgetGWAdmin(key)
	})
}
