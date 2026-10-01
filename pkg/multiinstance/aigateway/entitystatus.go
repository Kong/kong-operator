package aigateway

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
)

// EntityFailure is a single configuration entity that failed to be translated
// into or applied as part of the configuration pushed to the data planes.
type EntityFailure struct {
	// Obj is the configuration entity the failure is attributed to.
	Obj client.Object
	// Err is the failure cause, reported in the entity's Programmed condition message.
	Err error
}

// statusKey identifies a configuration entity within the reporter's failure
// message map. It mirrors the GVK+namespace/name keying of
// status.ConfigurationStatusSet.
func statusKey(obj client.Object, gvk schema.GroupVersionKind) string {
	return gvk.String() + "/" + obj.GetNamespace() + "/" + obj.GetName()
}

// EntityStatusReporter records the per-entity outcome of the instance's
// configuration syncs (translation into the aigw.Document and the push to the
// data planes' Admin APIs) and serves it to the configuration-entity
// reconcilers, which turn it into the entities' Programmed conditions.
//
// It mirrors the KIC pattern of KongClient's kubernetesObjectReportsFilter:
// the whole status set is swapped after every sync and the status queue is
// notified so that the reconcilers re-read it.
type EntityStatusReporter struct {
	scheme *runtime.Scheme

	mu sync.RWMutex
	// set holds the configuration status of every entity reported by the last
	// sync. It is swapped wholesale on every Report call.
	set *status.ConfigurationStatusSet
	// failureMessages holds the error message of every entity reported as
	// failed by the last sync, keyed by statusKey.
	failureMessages map[string]string

	// queue notifies the configuration-entity reconcilers when the reported
	// status of entities changed, so they re-read it and update the entities'
	// Programmed conditions.
	queue *status.Queue
}

// NewEntityStatusReporter creates a new EntityStatusReporter. The scheme is
// used to resolve the GVK of the reported entities: typed objects read from
// the client cache carry no TypeMeta, and the underlying status set keys its
// entries on the GVK.
func NewEntityStatusReporter(scheme *runtime.Scheme) *EntityStatusReporter {
	return &EntityStatusReporter{
		scheme:          scheme,
		set:             status.NewConfigurationStatusSet(),
		failureMessages: map[string]string{},
		queue:           status.NewQueue(),
	}
}

// Queue returns the status queue the reporter publishes status update events
// on. It is wired into the configuration-entity reconcilers' StatusQueue.
func (r *EntityStatusReporter) Queue() *status.Queue {
	return r.queue
}

// Report stores the outcome of a single configuration sync and notifies the
// configuration-entity reconcilers: included are the entities that were
// successfully translated and pushed, failures the ones that failed
// translation or apply, along with the error cause.
func (r *EntityStatusReporter) Report(included []client.Object, failures []EntityFailure) {
	set := status.NewConfigurationStatusSet()
	failureMessages := make(map[string]string, len(failures))
	for _, obj := range included {
		set.Insert(r.withGVK(obj), true)
	}
	for _, f := range failures {
		gvkObj := r.withGVK(f.Obj)
		set.Insert(gvkObj, false)
		if f.Err != nil {
			failureMessages[statusKey(gvkObj, gvkObj.GetObjectKind().GroupVersionKind())] = f.Err.Error()
		}
	}

	r.mu.Lock()
	prevSet, prevMessages := r.set, r.failureMessages
	r.set = set
	r.failureMessages = failureMessages
	r.mu.Unlock()

	// Notify the reconcilers only after the new set is in place: they read the
	// set when handling the event. Publishing the objects themselves (not the
	// message map) is enough - the reconciler re-fetches the entity and asks
	// the reporter for its status and message. Publish only entities whose
	// reported status or message changed: every entity reconcile notifies the
	// sync loop, so an unconditional publish makes the loop re-sync every
	// gateway forever.
	for _, obj := range uniqueObjects(included, failures) {
		changed := prevSet.Get(obj) != set.Get(obj)
		key := statusKey(obj, obj.GetObjectKind().GroupVersionKind())
		if prevMessages[key] != failureMessages[key] {
			changed = true
		}
		if !changed {
			continue
		}
		r.queue.Publish(obj)
	}
}

// withGVK returns obj with its GVK resolved through the scheme and set on it.
// The objects passed to the reporter are cache copies (from List or Get), so
// mutating them is safe.
func (r *EntityStatusReporter) withGVK(obj client.Object) client.Object {
	gvk, err := apiutil.GVKForObject(obj, r.scheme)
	if err != nil {
		// Objects registered in the instance's scheme always resolve; a miss
		// leaves the GVK empty, which degrades keying to namespace/name only.
		return obj
	}
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	return obj
}

// uniqueObjects returns the deduplicated union of the included and failed
// entities. Current callers never list one entity in both slices; the dedup
// keeps a future caller safe.
func uniqueObjects(included []client.Object, failures []EntityFailure) []client.Object {
	all := make([]client.Object, 0, len(included)+len(failures))
	all = append(all, included...)
	for _, f := range failures {
		all = append(all, f.Obj)
	}
	seen := make(map[string]struct{}, len(all))
	unique := all[:0]
	for _, obj := range all {
		key := obj.GetObjectKind().GroupVersionKind().String() + "/" + obj.GetNamespace() + "/" + obj.GetName()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, obj)
	}
	return unique
}

// AreKubernetesObjectReportsEnabled always returns true: the reporter exists
// precisely to report entity configuration status.
func (r *EntityStatusReporter) AreKubernetesObjectReportsEnabled() bool {
	return true
}

// KubernetesObjectConfigurationStatus reports the configuration status of the
// provided entity: Succeeded when it was part of the last successfully pushed
// configuration, Failed when its translation or the push failed, Unknown when
// it has never been reported or its generation is newer than the last report.
func (r *EntityStatusReporter) KubernetesObjectConfigurationStatus(obj client.Object) status.ConfigurationStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.set.Get(r.withGVK(obj))
}

// KubernetesObjectIsConfigured reports whether the provided entity was part of
// the last successfully pushed configuration.
func (r *EntityStatusReporter) KubernetesObjectIsConfigured(obj client.Object) bool {
	return r.KubernetesObjectConfigurationStatus(obj) == status.ConfigurationStatusSucceeded
}

// KubernetesObjectConfigurationStatusMessage returns the error message
// reported for the provided entity by the last sync, or an empty string when
// the entity did not fail (or failed without a message).
func (r *EntityStatusReporter) KubernetesObjectConfigurationStatusMessage(obj client.Object) string {
	gvk, err := apiutil.GVKForObject(obj, r.scheme)
	if err != nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.failureMessages[statusKey(obj, gvk)]
}
