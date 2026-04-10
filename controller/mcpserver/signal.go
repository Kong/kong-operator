package mcpserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/jpillora/backoff"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

const (
	signalChannelBufSize = 100

	// initialOffset is the offset used in the first request to the MCP server signal API.
	initialOffset = "INITIAL"
)

// EventType represents the type of a control plane event.
type EventType string

const (
	// EventTypeRegister signals that a KonnectGatewayControlPlane became available
	// and its signal-polling routine should be started.
	EventTypeRegister EventType = "register"
	// EventTypeDeregister signals that a KonnectGatewayControlPlane was removed
	// and its signal-polling routine should be stopped.
	EventTypeDeregister EventType = "deregister"
)

// CPEvent signals that a KonnectGatewayControlPlane event occurred.
// ControlPlane is always set. For EventTypeDeregister it may be a stub carrying
// only the Name and Namespace needed for cancellation.
type CPEvent struct {
	Type          EventType
	KonnectClient sdkops.SDKWrapper
	ControlPlane  *konnectv1alpha2.KonnectGatewayControlPlane
}

// SignalManager manages a set of per-UUID background goroutines that periodically
// poll the Konnect MCP server signal API until stopped or the parent context is cancelled.
// Events are delivered via a channel and consumed by the internal dispatch loop.
type SignalManager struct {
	loggingMode      logging.Mode
	client           client.Client
	scheme           *runtime.Scheme
	reconcileEventCh chan<- event.GenericEvent

	mu       sync.Mutex
	parent   context.Context //nolint:containedctx // intentionally stored to derive per-CP child contexts in setControlPlane
	routines map[string]context.CancelFunc
	resetChs map[string]chan struct{}

	cpCh chan CPEvent
}

// NewSignalManager creates a new SignalManager.
func NewSignalManager(loggingMode logging.Mode, client client.Client, scheme *runtime.Scheme, reconcileEventCh chan<- event.GenericEvent) *SignalManager {
	return &SignalManager{
		loggingMode:      loggingMode,
		client:           client,
		scheme:           scheme,
		reconcileEventCh: reconcileEventCh,
		routines:         make(map[string]context.CancelFunc),
		resetChs:         make(map[string]chan struct{}),
		cpCh:             make(chan CPEvent, signalChannelBufSize),
	}
}

// run stores the parent context and starts the event-dispatch goroutine.
// It must be called before Emit.
func (s *SignalManager) run(ctx context.Context) {
	s.mu.Lock()
	s.parent = ctx
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-s.cpCh:
				switch ev.Type {
				case EventTypeRegister:
					s.registerControlPlane(ev.KonnectClient, ev.ControlPlane)
				case EventTypeDeregister:
					s.deregisterControlPlane(ev.ControlPlane)
				}
			}
		}
	}()
}

// EmitControlPlaneEvent sends ev to the dispatch channel. It blocks until the event is
// accepted or ctx is done.
func (s *SignalManager) EmitControlPlaneEvent(ctx context.Context, ev CPEvent) {
	select {
	case s.cpCh <- ev:
	case <-ctx.Done():
	}
}

// NotifyMCPServerDeleted sends a reset signal to the signal routine of the
// control plane identified by namespace/cpName, so that the next poll uses the
// initial offset and picks up any changes caused by the deletion.
func (s *SignalManager) NotifyMCPServerDeleted(namespace, cpName string) {
	s.mu.Lock()
	resetCh, ok := s.resetChs[namespace+"/"+cpName]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case resetCh <- struct{}{}:
	default:
	}
}

// registerControlPlane registers the control plane and starts a dedicated goroutine for it.
// If the control plane is already registered, registerControlPlane is a no-op.
func (s *SignalManager) registerControlPlane(konnectClient sdkops.SDKWrapper, cp *konnectv1alpha2.KonnectGatewayControlPlane) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := cp.Namespace + "/" + cp.Name
	if _, ok := s.routines[key]; ok {
		return
	}

	ctx, cancel := context.WithCancel(s.parent)
	s.routines[key] = cancel

	// fetchEventCh is buffered (size 1) so the signal routine never blocks: if a
	// fetch is already pending the extra notification is safely dropped.
	fetchEventCh := make(chan struct{}, 1)
	// resetCh is buffered (size 1): a pending reset notification is never lost
	// but multiple rapid deletions collapse into a single reset.
	resetCh := make(chan struct{}, 1)
	s.resetChs[key] = resetCh

	fetcher := NewMCPServersFetcher(s.loggingMode, s.client, konnectClient, fetchEventCh, s.reconcileEventCh, cp, s.scheme)
	fetcher.run(ctx)

	go func() {
		defer cancel()
		s.mcpCPSignalRoutine(ctx, cp, konnectClient, fetchEventCh, resetCh)
	}()
}

// CPEvents returns a read-only channel delivering CP lifecycle events.
// It is intended for use in tests.
func (s *SignalManager) CPEvents() <-chan CPEvent {
	return s.cpCh
}

// deregisterControlPlane cancels and unregisters the goroutine for the given control plane.
// If the control plane is not registered, deregisterControlPlane is a no-op.
func (s *SignalManager) deregisterControlPlane(cp *konnectv1alpha2.KonnectGatewayControlPlane) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := cp.Namespace + "/" + cp.Name
	cancel, ok := s.routines[key]
	if !ok {
		return
	}
	cancel()
	delete(s.routines, key)
	delete(s.resetChs, key)
}

func (s *SignalManager) mcpCPSignalRoutine(ctx context.Context, cp *konnectv1alpha2.KonnectGatewayControlPlane, konnectClient sdkops.SDKWrapper, fetchEventCh chan<- struct{}, resetCh <-chan struct{}) {
	logger := log.GetLogger(ctx, "mcpserver-signal", s.loggingMode)
	offset := new(initialOffset)

	b := &backoff.Backoff{
		Min:    time.Second,
		Max:    time.Minute,
		Factor: 2,
	}

	for {
		if ctx.Err() != nil {
			return
		}

		// Reset offset if an MCPServer deletion was signalled before the next poll.
		select {
		case <-resetCh:
			offset = new(initialOffset)
		default:
		}

		capabilities := &sdkkonnectcomp.MCPCapabilitiesMap{
			Mcp: &sdkkonnectcomp.MCPCapabilityRequest{
				Version: "v1",
				Offset:  offset,
			},
		}

		resp, err := konnectClient.GetMCPServersSDK().GetMcpServerSignals(ctx, cp.GetKonnectID(), capabilities)
		if err != nil {
			log.Error(logger, err, "failed to get MCP server signal", "name", cp.Name, "namespace", cp.Namespace)
			select {
			case <-time.After(b.Duration()):
			case <-resetCh:
				offset = new(initialOffset)
			case <-ctx.Done():
				return
			}
			continue
		}

		b.Reset()

		if resp.StatusCode == http.StatusOK && resp.MCPServerSignals != nil {
			for _, signal := range resp.MCPServerSignals.Signals {
				log.Debug(logger, "MCP server signal received", "name", cp.Name, "namespace", cp.Namespace, "signal", signal)
				if signal.MCPServerSignalV1 != nil {
					off := signal.MCPServerSignalV1.Offset
					offset = &off
				}
			}
			// Wake up the fetcher to refresh all MCP servers for this control plane.
			select {
			case fetchEventCh <- struct{}{}:
			default:
			}
		}
	}
}
