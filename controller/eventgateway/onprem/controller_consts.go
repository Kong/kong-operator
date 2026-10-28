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

import "time"

// -----------------------------------------------------------------------------
// OnPremEventGateway - Names
// -----------------------------------------------------------------------------

// ControllerName is the name used for logging.
const ControllerName = "onprem-eventgateway"

// annotationCleanupStarted records on a deleted gateway when its cleanup
// started: its grace period counts from then, across restarts of the
// operator.
const annotationCleanupStarted = "gateway-operator.konghq.com/gw-admin-cleanup-started"

// FinalizerGWAdminCleanup is the finalizer that makes gw-admin stop serving a
// gateway before the gateway is deleted. Only this controller removes it: a
// gateway deleted while the controller is disabled stays until it is enabled
// again, or until the finalizer is removed by hand.
const FinalizerGWAdminCleanup = "gateway-operator.konghq.com/onprem-eventgateway-gw-admin-cleanup"

// -----------------------------------------------------------------------------
// OnPremEventGateway - Reconciles
// -----------------------------------------------------------------------------

// maxConcurrentReconciles is how many gateways are reconciled at once.
const maxConcurrentReconciles = 8

// lockRetryInterval is how long a reconcile waits before trying again when
// another gateway of its GWAdmin is being reconciled, should the unlock not
// wake it: those of a GWAdmin are reconciled one at a time (see
// gwadminsync.Syncer.TryLock), and a gw-admin pod that doesn't answer delays
// them, not the gateways of other GWAdmins.
const lockRetryInterval = 5 * time.Second

// terminatingPodTimeout bounds the requests to a terminating gw-admin pod,
// told to stop trusting the data plane certificates the configuration no
// longer holds: it serves no push, and one that doesn't answer would hold the
// lock of its GWAdmin as long as a pod pushed to.
const terminatingPodTimeout = 10 * time.Second

// The retries of the pushes to the gw-admin pods still pending, e.g. starting
// while their image is pulled: nothing tells once they listen, as a gw-admin
// pod only turns Ready once provisioned. The wait doubles from
// pendingRetryBase up to pendingRetryMax, e.g. for a pod crash looping.
const (
	pendingRetryBase = 2 * time.Second
	pendingRetryMax  = 30 * time.Second
)

// maxConditionMessageLength bounds the condition messages, which may hold
// the error of every gw-admin pod, each with the response of the pod: well
// under the 32768 bytes the API server accepts, so that the status stays
// readable.
const maxConditionMessageLength = 4096

// -----------------------------------------------------------------------------
// OnPremEventGateway - Cleanup of a deleted gateway
// -----------------------------------------------------------------------------

const (
	// cleanupGracePeriod is how long the deletion of a gateway waits for
	// every gw-admin pod to stop serving it. Past it, the gateway is deleted
	// anyway: a pod that can't be reached that long, e.g. one crash looping,
	// serves nothing, and keeps nothing across a restart.
	cleanupGracePeriod = 5 * time.Minute

	// cleanupRetryInterval is how often, at most, the deletion of a gateway
	// tries again to make every gw-admin pod stop serving it during
	// cleanupGracePeriod: a change to its GWAdmin or to its pods triggers it
	// too.
	cleanupRetryInterval = 30 * time.Second

	// minCleanupRetryInterval is the shortest wait before the deletion of a
	// gateway tries again, e.g. right before cleanupGracePeriod ends.
	minCleanupRetryInterval = time.Second
)

// -----------------------------------------------------------------------------
// OnPremEventGateway - Events
// -----------------------------------------------------------------------------

// The reasons and actions of the events of a gateway.
const (
	eventActionPushConfiguration = "PushConfiguration"
	eventActionDelete            = "Delete"
	// eventReasonCleanupTimedOut is the reason of the event of a gateway
	// deleted although some gw-admin pod may still serve it.
	eventReasonCleanupTimedOut = "GWAdminCleanupTimedOut"
)

// maxEventNoteLength bounds the notes of the events: the API server rejects
// an event whose note is longer than 1024 bytes.
const maxEventNoteLength = 1024
