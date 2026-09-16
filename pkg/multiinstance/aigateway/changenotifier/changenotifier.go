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
		// ponytail: fixed-size buffer, changes are dropped when full; per-parent
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
func (c *ChangeNotifier) NotifyChange(
	ctx context.Context,
	parent *types.NamespacedName,
	obj client.Object,
) {
	select {
	case <-ctx.Done():
	// TODO: consider adding debouncing.
	case c.ch <- Change{
		ID:       obj.GetUID(),
		ParentNN: parent,
		Object:   obj,
	}:
	case <-c.closedCh:
	default:
		// The buffer is full: drop the change rather than block the caller. The next
		// change for the same parent, or the instance's periodic sync, catches up.
	}
}

// Close closes the ChangeNotifier and releases its resources.
// It is safe to call multiple times. It only closes the channel once.
func (c *ChangeNotifier) Close() {
	c.once.Do(func() {
		// Close and set the channel to nil to make receiving from it block indefinitely.
		close(c.ch)
		c.ch = nil
		// Mark the notifier as closed to make receiving from it always return immediately.
		c.closedCh = make(chan struct{})
		close(c.closedCh)
	})
}
