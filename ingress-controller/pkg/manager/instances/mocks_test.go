package instances_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/samber/lo"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/instances"
)

// mockInstance is a mock implementation of instances.Instance.
type mockInstance struct {
	id                 manager.ID
	returnErrOnRun     error
	wasStarted         atomic.Bool
	wasContextCanceled atomic.Bool
}

var _ instances.Instance = &mockInstance{}

// ConfigHash implements instances.Instance.
func (m *mockInstance) ConfigHash() (string, error) {
	return m.id.String(), nil
}

func newMockInstance(id manager.ID) *mockInstance {
	return &mockInstance{
		id: id,
	}
}

func (m *mockInstance) ID() manager.ID {
	return m.id
}

func (m *mockInstance) Run(ctx context.Context) error {
	m.wasStarted.Store(true)

	<-ctx.Done()
	m.wasContextCanceled.Store(true)

	return m.returnErrOnRun
}

func (m *mockInstance) IsReady() error {
	return nil
}

func (m *mockInstance) DiagnosticsHandler() http.Handler {
	return nil
}

// mockDiagnosticsExposer is a mock implementation of instances.DiagnosticsExposer.
type mockDiagnosticsExposer struct {
	registeredInstances map[manager.ID]struct{}
	lock                sync.Mutex
}

func newMockDiagnosticsExposer() *mockDiagnosticsExposer {
	return &mockDiagnosticsExposer{
		registeredInstances: make(map[manager.ID]struct{}),
	}
}

func (m *mockDiagnosticsExposer) RegisterInstance(id manager.ID, _ http.Handler) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.registeredInstances[id] = struct{}{}
}

func (m *mockDiagnosticsExposer) UnregisterInstance(id manager.ID) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.registeredInstances, id)
}

func (m *mockDiagnosticsExposer) RegisteredInstances() []manager.ID {
	m.lock.Lock()
	defer m.lock.Unlock()

	return lo.Keys(m.registeredInstances)
}
