package changenotifier

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChangeNotifier is responsible for notifying about changes in the AI Gateway configuration.
type ChangeNotifier struct {
	ch       chan Change
	closedCh chan struct{}
	once     sync.Once
	mu       sync.RWMutex
}

// Change is a single notification about a configuration entity change, addressed to the
// OnPremAIGateway identified by ParentNN.
type Change struct {
	ID       types.UID
	ParentNN *types.NamespacedName
	Object   client.Object
}

// New creates a new instance of ChangeNotifier.
func New() *ChangeNotifier {
	return &ChangeNotifier{
		// Buffered so that NotifyChange never blocks the caller: the configuration
		// controllers keep reconciling even when no instance is running yet.
		// ponytail: fixed-size buffer, the oldest change is evicted when full; per-parent
		// channels keyed by OnPremAIGateway NN if fan-out correctness ever matters.
		ch: make(chan Change, 128),
		// Set this to nil initially to make receiving from it block until allocated with make.
		closedCh: nil,
	}
}

// NotifyChannel returns a channel that receives change notifications.
func (c *ChangeNotifier) NotifyChannel() <-chan Change {
	return c.ch
}

// NotifyChange sends a change notification for the given AI Gateway instance.
//
// The notifier stores a snapshot of the object: the caller keeps owning (and
// mutating) the object afterwards. The generated configuration-entity
// reconcilers, for example, update the entity's status after notifying, and
// the status update's response decoding rewrites the object's fields, which
// would race with the consumer's reads if the object were shared.
func (c *ChangeNotifier) NotifyChange(
	ctx context.Context,
	parent *types.NamespacedName,
	obj client.Object,
) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Snapshot once, before either send: the caller is free to mutate the
	// object as soon as this call returns, so the channel must never carry it.
	snapshot, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		// Unreachable for every client.Object implementation.
		snapshot = obj
	}
	change := Change{
		ID:       snapshot.GetUID(),
		ParentNN: parent,
		Object:   snapshot,
	}

	select {
	case <-ctx.Done():
	// TODO: consider adding debouncing.
	case c.ch <- change:
	case <-c.closedCh:
	default:
		// The buffer is full: evict the oldest buffered change to admit this one, so that
		// bursts keep the freshest state and the newest change for a parent still reaches
		// the consumer, which coalesces changes per gateway before syncing. A change can
		// still be dropped if the buffer refills concurrently, and an evicted change whose
		// parent never sees another event is not re-driven.
		select {
		case <-c.ch:
		default:
		}
		select {
		case c.ch <- change:
		default:
		}
	}
}

// Close closes the ChangeNotifier and releases its resources.
// It is safe to call multiple times. It only closes the channel once.
func (c *ChangeNotifier) Close() {
	c.once.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		// Close and set the channel to nil to make receiving from it block indefinitely.
		close(c.ch)
		c.ch = nil
		// Mark the notifier as closed to make receiving from it always return immediately.
		c.closedCh = make(chan struct{})
		close(c.closedCh)
	})
}
