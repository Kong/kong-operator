package helpers

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/environments"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// TODO https://github.com/Kong/kubernetes-testing-framework/issues/302
// Extract this into KTF to be shared across tests and different repos.

// SetupTestEnv is a helper function for tests which conveniently creates a cluster
// cleaner (to clean up test resources automatically after the test finishes)
// and creates a new namespace for the test to use.
// The namespace is being automatically deleted during the test teardown using t.Cleanup().
func SetupTestEnv(t *testing.T, ctx context.Context, env environments.Environment) (*corev1.Namespace, *clusters.Cleaner) {
	t.Helper()

	t.Log("performing test setup")
	cleaner := clusters.NewCleaner(env.Cluster())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
		defer cancel()
		assert.NoError(t, cleaner.Cleanup(ctx))
	})

	t.Log("creating a testing namespace")
	namespace, err := clusters.GenerateNamespace(ctx, env.Cluster(), labelValueForTest(t))
	require.NoError(t, err)
	t.Logf("using test namespace: %s", namespace.Name)
	cleaner.AddNamespace(namespace)

	t.Cleanup(func() {
		if t.Failed() {
			output, err := env.Cluster().DumpDiagnostics(context.Background(), t.Name())
			assert.NoError(t, err)
			t.Logf("%s failed, dumped diagnostics to %s", t.Name(), output)
		}
	})

	return namespace, cleaner
}

// ParseGoTestFlags is a helper function that allows usage of -run and -skip
// flags in go test. It performs filtering on its own (in the best-effort manner).
// Actual implementation from the standard library is quite complex, e.g. see:
// https://github.com/golang/go/blob/f719d5cffdb8298eff7a5ef533fe95290e8c869c/src/testing/match.go#L55,
// thus we only use regex - that should be good enough in most cases.
// Function returns a test suite that should be run in testRunner function (defined test with RunTestSuite(...)
// called inside).
func ParseGoTestFlags(testRunner func(t *testing.T), testSuiteToRun []func(t *testing.T)) []func(t *testing.T) {
	// Take values from -run and -skip flags to filter test cases in the test suite.
	flag.Parse()
	fRun := flag.Lookup("test.run")
	testFlagRunValue := fRun.Value.String()
	testFlagSkipValue := flag.Lookup("test.skip").Value.String()
	var err error
	testSuiteToRun, err = filterTestSuite(testSuiteToRun, testFlagRunValue, testFlagSkipValue)
	if err != nil {
		fmt.Println("testing:", err)
		os.Exit(1)
	}

	// Hack - set test.run flag to the name of the test function that runs the test suite
	// to execute it with tests that are returned from this function.
	// They are explicitly passed to RunTestSuite(...) as an argument.
	if err := fRun.Value.Set(getFunctionName(testRunner)); err != nil {
		fmt.Println("testing: unexpected error happened (it should never happen, check the code)", err)
		os.Exit(1)
	}

	return testSuiteToRun
}

// filterTestSuite filters test suite based on value of -run and -skip flags.
func filterTestSuite(testSuite []func(*testing.T), testFlagRunValue, testFlagSkip string) ([]func(*testing.T), error) {
	// Fail when the same test was added to the test suite more than once.
	duplicates := lo.FindDuplicatesBy(testSuite, func(f func(*testing.T)) string {
		return getFunctionName(f)
	})
	duplicatesNames := lo.Map(duplicates, func(f func(*testing.T), _ int) string {
		return getFunctionName(f)
	})
	if len(duplicates) > 0 {
		return nil, fmt.Errorf("duplicate test functions found in test suite: %v", duplicatesNames)
	}
	// Take care of -run flag.
	rg, err := regexp.Compile(testFlagRunValue)
	if err != nil {
		return nil, fmt.Errorf(`invalid regexp for -test.run ("%s"): %w`, testFlagRunValue, err)
	}
	testSuite = lo.Filter(testSuite, func(f func(*testing.T), _ int) bool {
		return rg.MatchString(getFunctionName(f))
	})
	// Take care of -skip flag.
	if testFlagSkip != "" {
		rg, err = regexp.Compile(testFlagSkip)
		if err != nil {
			return nil, fmt.Errorf(`invalid regexp for -test.skip ("%s"): %w`, testFlagSkip, err)
		}
		testSuite = lo.Filter(testSuite, func(f func(*testing.T), _ int) bool {
			return !rg.MatchString(getFunctionName(f))
		})
	}
	// Fail fast to avoid provisioning environment (executing TestMain) when no tests to run.
	if len(testSuite) == 0 {
		return nil, fmt.Errorf("no tests to run")
	}

	return testSuite, nil
}

// RunTestSuite runs all tests from the test suite.
// Import and call in a test function. Value for testSuite should be obtained from
// ParseGoTestFlags(...) (assign value to global variable and pass it as argument).
// Environment needs to be bootstrapped in TestMain or a test case itself. It is
// recommended for tests that need to be imported in other repositories.
func RunTestSuite(t *testing.T, testSuite []func(*testing.T)) {
	t.Log("INFO: running test suite")
	for _, test := range testSuite {
		t.Run(getFunctionName(test), test)
	}
}

// get function name returns name of the function passed as an argument.
func getFunctionName(i interface{}) string {
	r := strings.Split(runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name(), ".")
	return r[len(r)-1]
}

// labelValueForTest returns a sanitized test name that can be used as kubernetes
// label value.
func labelValueForTest(t *testing.T) string {
	s := strings.ReplaceAll(t.Name(), "/", ".")
	// Trim to adhere to k8s label requirements:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
	if len(s) > 63 {
		return s[:63]
	}
	return s
}
