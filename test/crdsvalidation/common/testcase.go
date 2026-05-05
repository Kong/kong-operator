package common

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kong/kong-operator/v2/pkg/clientset/scheme"
)

// TestCasesGroup is a group of test cases related to CRD validation.
type TestCasesGroup[T client.Object] []TestCase[T]

// RunWithConfig runs all test cases in the group against the provided rest.Config's cluster.
func (g TestCasesGroup[T]) RunWithConfig(t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) {
	for _, tc := range g {
		tc.
			RunWithConfig(t, cfg, scheme)
	}
}

// Run runs all test cases in the group.
func (g TestCasesGroup[T]) Run(t *testing.T) {
	cfg, err := config.GetConfig()
	require.NoError(t, err)
	g.RunWithConfig(t, cfg, scheme.Scheme)
}

const (
	// DefaultEventuallyTimeout is the default timeout for EventuallyConfig.
	DefaultEventuallyTimeout = 5 * time.Second
	// DefaultEventuallyPeriod is the default period for EventuallyConfig.
	DefaultEventuallyPeriod = 10 * time.Millisecond
)

// EventuallyConfig is the configuration for assert.Eventually() which is used to assert errors.
type EventuallyConfig struct {
	// Timeout is the maximum time to wait for the condition to be true.
	Timeout time.Duration
	// Period is the time to wait between retries.
	Period time.Duration
}

// TestCase represents a test case for CRD validation.
type TestCase[T client.Object] struct {
	// Name is the name of the test case.
	Name string

	// SkipReason is the reason to skip the test case.
	SkipReason string

	// TestObject is the object to be tested.
	TestObject T

	// ExpectedErrorMessage is the expected error message when creating the object.
	ExpectedErrorMessage *string

	// ExpectedErrorEventuallyConfig is the configuration for assert.Eventually() which is used to assert the create error.
	// If not provided the error is checked immediately, just once.
	ExpectedErrorEventuallyConfig EventuallyConfig

	// ExpectedUpdateErrorMessage is the expected error message when updating the object.
	ExpectedUpdateErrorMessage *string

	// ExpectedStatusUpdateErrorMessage is the expected error message when updating the object status.
	ExpectedStatusUpdateErrorMessage *string

	// Update is a function that updates the object in the test case after it's created.
	// It can be used to verify CEL rules that verify the previous object's version against the new one.
	Update func(T)

	// StatusUpdate is a function that updates the status of the object in the test case after it's created.
	StatusUpdate func(T)

	// WarningCollector (optional) collects API server warnings emitted during operations.
	// If set together with ExpectedWarningMessage, the test will assert that a warning containing the
	// expected message substring was produced.
	WarningCollector *WarningCollector

	// ExpectedWarningMessage is the substring expected to be found in at least one collected warning.
	ExpectedWarningMessage *string

	// Assert is an optional function to perform additional assertions on the created object.
	// It is called after the object is created and before an update (if specified).
	Assert func(*testing.T, T)
}

// RunWithConfig runs the test case against the provided rest.Config's cluster.
func (tc *TestCase[T]) RunWithConfig(t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) {
	if tc.SkipReason != "" {
		t.Skip(tc.SkipReason)
	}

	timeout := DefaultEventuallyTimeout
	if tc.ExpectedErrorEventuallyConfig.Timeout != 0 {
		timeout = tc.ExpectedErrorEventuallyConfig.Timeout
	}
	period := DefaultEventuallyPeriod
	if tc.ExpectedErrorEventuallyConfig.Period != 0 {
		period = tc.ExpectedErrorEventuallyConfig.Period
	}

	require.NotNil(t, tc.TestObject, "TestObject is nil in test %s", tc.Name)

	// Run the test case.
	t.Run(tc.Name, func(t *testing.T) {
		require.NotNil(t, tc.TestObject, "TestObject is nil in test %s", tc.Name)

		t.Parallel()
		ctx := context.Background()

		// Create a new controller-runtime client.Client.
		cl, err := client.New(cfg, client.Options{
			Scheme: scheme,
		})
		require.NoError(t, err)

		templateObj := tc.TestObject.DeepCopyObject().(T)

		// Take a copy so that we can update the status field if needed. Without copying, the Create call
		// overwrites the status field in tc.TestObject with the default server returns, and we lose the status
		// set in the test case.
		desiredObj := templateObj.DeepCopyObject().(T)

		tCleanupObject := func(ctx context.Context, t *testing.T, obj client.Object) {
			// NOTE: Deep copy the object as without this we end up causing a data race:
			objToDelete := obj.DeepCopyObject().(T)
			t.Cleanup(func() {
				assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, objToDelete)))
			})
		}

		waitForCondition := func(condition func() (bool, string), failureTitle string) {
			deadline := time.Now().Add(timeout)
			for {
				matched, details := condition()
				if matched {
					return
				}

				if !time.Now().Before(deadline) {
					require.FailNowf(t, failureTitle, "%s", details)
				}

				time.Sleep(period)
			}
		}

		warningFound := func() bool {
			if tc.WarningCollector == nil || tc.ExpectedWarningMessage == nil {
				return false
			}

			for _, msg := range tc.WarningCollector.GetWarnings() {
				if strings.Contains(msg, *tc.ExpectedWarningMessage) {
					return true
				}
			}

			return false
		}

		var (
			createdObj    T
			hasCreatedObj bool
		)
		waitForCondition(func() (bool, string) {
			if hasCreatedObj && warningFound() {
				return true, ""
			}

			toCreate := templateObj.DeepCopyObject().(T)

			createErr := cl.Create(ctx, toCreate)
			if createErr == nil {
				createdObj = toCreate
				hasCreatedObj = true
				tCleanupObject(ctx, t, toCreate)
			}

			if tc.ExpectedErrorMessage != nil {
				if createErr != nil && strings.Contains(createErr.Error(), *tc.ExpectedErrorMessage) {
					return true, ""
				}
				return false, fmt.Sprintf("Create error: %v; expected: %q", createErr, *tc.ExpectedErrorMessage)
			}

			if createErr != nil {
				return false, fmt.Sprintf("Create error: %v", createErr)
			}

			if tc.WarningCollector != nil && tc.ExpectedWarningMessage != nil {
				if warningFound() {
					return true, ""
				}

				return false, fmt.Sprintf("expected warning containing: %q, got: %#v", *tc.ExpectedWarningMessage, tc.WarningCollector.GetWarnings())
			}

			return true, ""
		}, "create condition not satisfied before timeout")

		if tc.ExpectedErrorMessage != nil {
			return
		}

		tc.TestObject = createdObj

		// Check with reflect if the status field is set and Update the status if so before updating the object.
		// That's required to populate Status that is not set on Create.
		if status := reflect.ValueOf(desiredObj).Elem().FieldByName("Status"); status.IsValid() && !status.IsZero() {
			// Populate name and resource version obtained from the server on Create.
			desiredObj.SetName(tc.TestObject.GetName())
			desiredObj.SetResourceVersion(tc.TestObject.GetResourceVersion())

			err = cl.Status().Update(ctx, desiredObj)
			require.NoError(t, err)

			err = cl.Get(ctx, client.ObjectKeyFromObject(tc.TestObject), tc.TestObject)
			require.NoError(t, err)
		}

		if tc.Assert != nil {
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(tc.TestObject), tc.TestObject))
			tc.Assert(t, tc.TestObject)
		}

		// If the Update function was defined, update the object and check if the update is allowed.
		if tc.Update != nil {
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				err := cl.Get(ctx, client.ObjectKeyFromObject(tc.TestObject), tc.TestObject)
				require.NoError(c, err)
				// Update the object state and push the update to the server.
				tc.Update(tc.TestObject)
				err = cl.Update(ctx, tc.TestObject)
				if tc.ExpectedWarningMessage != nil {
					found := false
					for _, w := range tc.WarningCollector.GetWarnings() {
						if strings.Contains(w, *tc.ExpectedWarningMessage) {
							found = true
							break
						}
					}
					if !assert.True(c, found, "Warning message not found: %s", *tc.ExpectedWarningMessage) {
						return
					}
				}
				// If the expected update error message is defined, check if the error message contains the expected message
				// and return. Otherwise, expect no error.
				if tc.ExpectedUpdateErrorMessage != nil {
					require.Error(c, err)
					assert.Contains(c, err.Error(), *tc.ExpectedUpdateErrorMessage)
					return
				}
				require.NoError(c, err)
			}, timeout, period)
		}

		// If the StatusUpdate function was defined, update the object status
		// and check if the update is allowed.
		if tc.StatusUpdate != nil {
			require.EventuallyWithT(t, func(c *assert.CollectT) {
				err := cl.Get(ctx, client.ObjectKeyFromObject(tc.TestObject), tc.TestObject)
				require.NoError(c, err)
				// Update the object status and push the update to the server.
				tc.StatusUpdate(tc.TestObject)
				err = cl.Status().Update(ctx, tc.TestObject)

				// If the expected status update error message is defined,
				// check if the error message contains the expected message
				// and return. Otherwise, expect no error.
				if tc.ExpectedStatusUpdateErrorMessage != nil {
					require.Error(c, err)
					assert.Contains(c, err.Error(), *tc.ExpectedStatusUpdateErrorMessage)
					return
				}
				require.NoError(c, err)
			}, timeout, period)
		}
	})
}

// Run runs the test case.
func (tc *TestCase[T]) Run(t *testing.T) {
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	tc.RunWithConfig(t, cfg, scheme.Scheme)
}
