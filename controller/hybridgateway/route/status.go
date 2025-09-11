package route

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SharedRouteStatusMap is a thread-safe map to store the status of routes shared across multiple gateways
type SharedRouteStatusMap struct {
	sync.RWMutex
	SharedStatus map[string]SharedRouteStatus
}

// SharedRouteStatus holds the status of services associated with a specific route-gateway pair
type SharedRouteStatus struct {
	Services map[string]ServiceControllerStatus
}

// ServiceControllerStatus holds the status of a service as managed by a specific controller
type ServiceControllerStatus struct {
	ServiceControllerInit bool
	ProgrammedBackends    int
}

// StatusMapKey generates a unique key for a route-gateway pair
// routeKind: "HTTPRoute", "GRPCRoute", etc.
// routeNN: "namespace/name" of the route
// gatewayNN: "namespace/name" of the gateway
func StatusMapKey(routeKind, routeNN, gatewayNN string) string {
	return routeKind + "|" + routeNN + "|" + gatewayNN
}

// NewSharedStatusMap initializes a new SharedRouteStatusMap
func NewSharedStatusMap() *SharedRouteStatusMap {
	return &SharedRouteStatusMap{
		SharedStatus: make(map[string]SharedRouteStatus),
	}
}

// UpdateProgrammedServices updates the status of a service for a specific route-gateway pair
func (s *SharedRouteStatusMap) UpdateProgrammedServices(service corev1.Service, key string, programmedBackends int) {
	s.Lock()
	defer s.Unlock()

	status, ok := s.SharedStatus[key]
	if !ok {
		status = SharedRouteStatus{
			Services: make(map[string]ServiceControllerStatus),
		}
	}
	serviceMapKey := client.ObjectKeyFromObject(&service).String()
	serviceControllerStatus, ok := status.Services[serviceMapKey]
	if !ok {
		serviceControllerStatus = ServiceControllerStatus{}
	}
	serviceControllerStatus.ServiceControllerInit = true
	serviceControllerStatus.ProgrammedBackends = programmedBackends

	status.Services[serviceMapKey] = serviceControllerStatus
	s.SharedStatus[key] = status
}

// GetProgrammedServices retrieves the number of programmed backends and initialization status for a service
// associated with a specific route-gateway pair
func (s *SharedRouteStatusMap) GetProgrammedServices(key string, serviceNN string) (n int, initiated bool) {
	s.RLock()
	defer s.RUnlock()

	status, ok := s.SharedStatus[key]
	if !ok {
		return 0, false
	}
	serviceStatus, ok := status.Services[serviceNN]
	if !ok {
		return 0, false
	}
	return serviceStatus.ProgrammedBackends, serviceStatus.ServiceControllerInit
}
