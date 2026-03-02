package mcpserver

import (
	"context"
	"net/http"
	"sync"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/jpillora/backoff"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

const (
	signalChannelBufSize = 100

	// initialOffset is the offset used in the first request to the MCP server signal API.
	initialOffset = "INITIAL"
)

// EventType represents the type of a control plane event.
type EventType string

const (
	// EventTypeSet signals that a KonnectGatewayControlPlane became available
	// and its signal-polling routine should be started.
	EventTypeSet EventType = "set"
	// EventTypeUnset signals that a KonnectGatewayControlPlane was removed
	// and its signal-polling routine should be stopped.
	EventTypeUnset EventType = "unset"
)

// CPEvent signals that a KonnectGatewayControlPlane event occurred.
// ControlPlane is always set. For EventTypeUnset it may be a stub carrying
// only the Name and Namespace needed for cancellation.
type CPEvent struct {
	Type          EventType
	KonnectClient sdkops.SDKWrapper
	ControlPlane  *konnectv1alpha1.KonnectGatewayControlPlane
}

// SignalManager manages a set of per-UUID background goroutines that periodically
// poll the Konnect MCP server signal API until stopped or the parent context is cancelled.
// Events are delivered via a channel and consumed by the internal dispatch loop.
type SignalManager struct {
	logger logr.Logger
	client client.Client
	scheme *runtime.Scheme

	mu       sync.Mutex
	parent   context.Context //nolint:containedctx // intentionally stored to derive per-CP child contexts in setControlPlane
	routines map[string]context.CancelFunc
	resetChs map[string]chan struct{}

	cpCh chan CPEvent
}

// NewSignalManager creates a new SignalManager.
func NewSignalManager(logger logr.Logger, client client.Client, scheme *runtime.Scheme) *SignalManager {
	return &SignalManager{
		logger:   logger,
		client:   client,
		scheme:   scheme,
		routines: make(map[string]context.CancelFunc),
		resetChs: make(map[string]chan struct{}),
		cpCh:     make(chan CPEvent, signalChannelBufSize),
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
				case EventTypeSet:
					s.setControlPlane(ev.KonnectClient, ev.ControlPlane)
				case EventTypeUnset:
					s.unsetControlPlane(ev.ControlPlane)
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

// setControlPlane registers the control plane and starts a dedicated goroutine for it.
// If the control plane is already registered, setControlPlane is a no-op.
func (s *SignalManager) setControlPlane(konnectClient sdkops.SDKWrapper, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := cp.Namespace + "/" + cp.Name
	if _, ok := s.routines[key]; ok {
		return
	}

	ctx, cancel := context.WithCancel(s.parent)
	s.routines[key] = cancel

	// wakeupCh is buffered (size 1) so the signal routine never blocks: if a
	// fetch is already pending the extra notification is safely dropped.
	wakeupCh := make(chan struct{}, 1)
	// resetCh is buffered (size 1): a pending reset notification is never lost
	// but multiple rapid deletions collapse into a single reset.
	resetCh := make(chan struct{}, 1)
	s.resetChs[key] = resetCh

	fetcher := NewMCPServersFetcher(s.logger, s.client, konnectClient, wakeupCh, cp, s.scheme)
	fetcher.run(ctx)

	go s.mcpCPSignalRoutine(ctx, cp, konnectClient, wakeupCh, resetCh)
}

// unsetControlPlane cancels and unregisters the goroutine for the given control plane.
// If the control plane is not registered, unsetControlPlane is a no-op.
func (s *SignalManager) unsetControlPlane(cp *konnectv1alpha1.KonnectGatewayControlPlane) {
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

func (s *SignalManager) mcpCPSignalRoutine(ctx context.Context, cp *konnectv1alpha1.KonnectGatewayControlPlane, konnectClient sdkops.SDKWrapper, wakeupCh chan<- struct{}, resetCh <-chan struct{}) {
	offset := lo.ToPtr(initialOffset)

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
			offset = lo.ToPtr(initialOffset)
		default:
		}

		capabilities := map[string]sdkkonnectcomp.MCPCapabilityRequest{
			"mcp": {Version: "v1", Offset: offset},
		}

		resp, err := konnectClient.GetMCPServersSDK().GetMcpServerSignal(ctx, cp.GetKonnectID(), capabilities)
		if err != nil {
			s.logger.Error(err, "failed to get MCP server signal", "name", cp.Name, "namespace", cp.Namespace)
			select {
			case <-time.After(b.Duration()):
			case <-resetCh:
				offset = lo.ToPtr(initialOffset)
			case <-ctx.Done():
				return
			}
			continue
		}

		b.Reset()

		if resp.StatusCode == http.StatusOK && resp.MCPServerSignals != nil {
			for _, signal := range resp.MCPServerSignals.Signals {
				s.logger.Info("MCP server signal received", "name", cp.Name, "namespace", cp.Namespace, "signal", signal)
				if signal.MCPServerSignalV1 != nil {
					off := signal.MCPServerSignalV1.Version
					offset = &off
				}
			}
			// Wake up the fetcher to refresh all MCP servers for this control plane.
			select {
			case wakeupCh <- struct{}{}:
			default:
			}
		}
	}
}
