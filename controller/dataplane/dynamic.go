package dataplane

import (
	"context"
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

// DataPlaneCallbacks holds callback managers for the DataPlane controller.
type DataPlaneCallbacks struct {
	// BeforeDeployment runs before the controller starts building its Deployment.
	BeforeDeployment CallbackManager
	// AfterDeployment runs after the controller has initially built its Deployment, but before it applies
	// user patches and sets default EnvVars.
	AfterDeployment CallbackManager
}

// TODO the Callback signature, CallbackManager struct, and CallbackRunner struct definitions below are currently
// specific to DataPlanes, but probably could be refactored to be more generic. Nothing in them actually cares about
// the specifics of the owning controller type. Individual Callbacks hold all the type-specific logic; the Manager and
// Runner just plumb the structs down to them. However, I'm not 100% on the best approach for this without actually
// trying to support multiple controllers, so I've left the absolute type in place for now.

// CallbackRunner runs a set of callbacks for a DataPlane on a subject child resource. All callbacks executed by a
// CallbackRunner instance must share the same subject type.
type CallbackRunner struct {
	// subjectType is the type of subject resource this controller modifies. For example, subjectType is Deployment if
	// the callback sets an environment variable in a Deployment originally built by the DataPlane controller.
	subjectType reflect.Type
	// dataplane is the DataPlane being reconciled by the controller invoking the Callbacks.
	dataplane *operatorv1beta1.DataPlane
	// manager holds the Callbacks this CallbackRunner will run.
	manager CallbackManager
	// client is a controller-runtime client. Callbacks can use this client to perform CRUD operations on resources
	// other than their subject and DataPlane.
	client client.Client
}

// NewCallbackRunner creates a callback runner.
func NewCallbackRunner(cl client.Client) *CallbackRunner {
	return &CallbackRunner{
		client: cl,
	}
}

// Do runs all registered callbacks in the runner's manager on a subject and returns a slice of callback errors.
func (r *CallbackRunner) Do(ctx context.Context, subject any) []error {
	// Check the subject type if needed. Some callbacks modify a resource originally built by the controller while others
	// create entirely new resources. If the callback does modify an in-progress resource, that resource needs to be
	// the correct type.
	if r.subjectType != nil {
		subjType := reflect.TypeOf(subject)
		subjTypePointer := reflect.PointerTo(r.subjectType)
		if subjType != subjTypePointer {
			return []error{
				fmt.Errorf("callback manager expected type %s, got type %s", subjTypePointer, subjType),
			}
		}
	}
	return r.manager.Do(ctx, r.dataplane, r.client, subject)
}

// Runs provides the callback set manager the callback runner will run.
func (r *CallbackRunner) Runs(m CallbackManager) *CallbackRunner {
	r.manager = m
	return r
}

// For sets the owner of the callback runner.
func (r *CallbackRunner) For(dataplane *operatorv1beta1.DataPlane) *CallbackRunner {
	r.dataplane = dataplane
	return r
}

// Modifies sets the child resource type this callback runner operates on.
func (r *CallbackRunner) Modifies(subjType reflect.Type) *CallbackRunner {
	r.subjectType = subjType
	return r
}

// CallbackManager collects a set of callbacks.
type CallbackManager struct {
	// calls is a map of callback names to Callback functions.
	calls map[string]Callback
}

// CreateCallbackManager creates a new CallbackManager, with an empty callback set.
func CreateCallbackManager() CallbackManager {
	return CallbackManager{
		calls: map[string]Callback{},
	}
}

// Callback is a function that performs operations on some resource owned by a DataPlane.
type Callback func(ctx context.Context, d *operatorv1beta1.DataPlane, c client.Client, s any) error

// Register adds a callback to the manager's callback set. Returns an error on duplicate names.
func (m *CallbackManager) Register(c Callback, name string) error {
	if _, exists := m.calls[name]; exists {
		return fmt.Errorf("callback %s already registered", name)
	}
	m.calls[name] = c
	return nil
}

// Unregister removes a callback with a given name from the callback set.
func (m *CallbackManager) Unregister(name string) error {
	if _, exists := m.calls[name]; !exists {
		return fmt.Errorf("callback %s not registered", name)
	}
	delete(m.calls, name)
	return nil
}

// Do runs all callbacks in the manager's callback set on a subject. It returns a slice of errors wrapped with the
// callback name.
func (m *CallbackManager) Do(ctx context.Context, d *operatorv1beta1.DataPlane, c client.Client, s any) []error {
	errs := []error{}
	for name, call := range m.calls {
		if err := call(ctx, d, c, s); err != nil {
			errs = append(errs, fmt.Errorf("callback %s failed: %w", name, err))
		}
	}
	return errs
}
