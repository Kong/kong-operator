package crdsvalidation

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kong/kubernetes-configuration/pkg/clientset/scheme"
)

// TestCasesGroup is a group of test cases related to CRD validation.
type TestCasesGroup[T client.Object] []TestCase[T]

// RunWithConfig runs all test cases in the group against the provided rest.Config's cluster.
func (g TestCasesGroup[T]) RunWithConfig(t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) {
	for _, tc := range g {
		tc.RunWithConfig(t, cfg, scheme)
	}
}

// Run runs all test cases in the group.
func (g TestCasesGroup[T]) Run(t *testing.T) {
	cfg, err := config.GetConfig()
	require.NoError(t, err)
	g.RunWithConfig(t, cfg, scheme.Scheme)
}

// TestCase represents a test case for CRD validation.
type TestCase[T client.Object] struct {
	// Name is the name of the test case.
	Name string

	// TestObject is the object to be tested.
	TestObject T

	// ExpectedErrorMessage is the expected error message when creating the object.
	ExpectedErrorMessage *string

	// ExpectedUpdateErrorMessage is the expected error message when updating the object.
	ExpectedUpdateErrorMessage *string

	// Update is a function that updates the object in the test case after it's created.
	// It can be used to verify CEL rules that verify the previous object's version against the new one.
	Update func(T)
}

// RunWithConfig runs the test case against the provided rest.Config's cluster.
func (tc *TestCase[T]) RunWithConfig(t *testing.T, cfg *rest.Config, scheme *runtime.Scheme) {
	// Run the test case.
	t.Run(tc.Name, func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()

		// Create a new controller-runtime client.Client.
		cl, err := client.New(cfg, client.Options{
			Scheme: scheme,
		})
		require.NoError(t, err)

		// Take a copy so that we can update the status field if needed. Without copying, the Create call
		// overwrites the status field in tc.TestObject with the default server returns, and we lose the status
		// set in the test case.
		desiredObj := tc.TestObject.DeepCopyObject().(T)

		// Create the object and set a cleanup function to delete it after the test if created successfully.
		err = cl.Create(ctx, tc.TestObject)
		if err == nil {
			t.Cleanup(func() {
				assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, tc.TestObject)))
			})
		}

		// If the error message is expected, check if the error message contains the expected message and return.
		if tc.ExpectedErrorMessage != nil {
			require.NotNil(t, err)
			assert.Contains(t, err.Error(), *tc.ExpectedErrorMessage)
			return
		}

		// Otherwise, continue, expecting no error.
		require.NoError(t, err)

		// Check with reflect if the status field is set and Update the status if so before updating the object.
		// That's required to populate Status that is not set on Create.
		if statusToUpdate := !reflect.ValueOf(desiredObj).Elem().FieldByName("Status").IsZero(); statusToUpdate {
			// Populate name and resource version obtained from the server on Create.
			desiredObj.SetName(tc.TestObject.GetName())
			desiredObj.SetResourceVersion(tc.TestObject.GetResourceVersion())

			err = cl.Status().Update(ctx, desiredObj)
			require.NoError(t, err)

			err = cl.Get(ctx, client.ObjectKeyFromObject(tc.TestObject), tc.TestObject)
			require.NoError(t, err)
		}

		// If the Update function was defined, update the object and check if the update is allowed.
		if tc.Update != nil {
			// Update the object state and push the update to the server.
			tc.Update(tc.TestObject)
			err := cl.Update(ctx, tc.TestObject)

			// If the expected update error message is defined, check if the error message contains the expected message
			// and return. Otherwise, expect no error.
			if tc.ExpectedUpdateErrorMessage != nil {
				require.NotNil(t, err)
				assert.Contains(t, err.Error(), *tc.ExpectedUpdateErrorMessage)
				return
			}
			require.NoError(t, err)
		}
	})
}

// Run runs the test case.
func (tc *TestCase[T]) Run(t *testing.T) {
	cfg, err := config.GetConfig()
	require.NoError(t, err)

	tc.RunWithConfig(t, cfg, scheme.Scheme)
}
