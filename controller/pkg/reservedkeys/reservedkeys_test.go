package reservedkeys

import (
	"maps"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// infoCountSink is a minimal logr.LogSink that counts Info() calls.
type infoCountSink struct{ count *int }

func (s infoCountSink) Init(logr.RuntimeInfo)             {}
func (s infoCountSink) Enabled(int) bool                  { return true }
func (s infoCountSink) Info(_ int, _ string, _ ...any)    { *s.count++ }
func (s infoCountSink) Error(_ error, _ string, _ ...any) {}
func (s infoCountSink) WithValues(_ ...any) logr.LogSink  { return s }
func (s infoCountSink) WithName(_ string) logr.LogSink    { return s }

func TestNewChecker(t *testing.T) {
	isReserved := NewChecker("app", "deployment.kubernetes.io/revision")

	assert.True(t, isReserved(consts.OperatorLabelPrefix+"foo"))
	assert.True(t, isReserved("app"))
	assert.True(t, isReserved("deployment.kubernetes.io/revision"))
	assert.False(t, isReserved("app.kubernetes.io/name"))
	assert.False(t, isReserved("safe-key"))
}

func TestFilter(t *testing.T) {
	isReserved := NewChecker("app")

	testCases := []struct {
		name           string
		keys           map[string]string
		expectedKept   map[string]string
		expectedIgnore int
	}{
		{
			name: "reserved keys dropped, safe keys kept",
			keys: map[string]string{
				"safe-key":                         "val",
				consts.OperatorLabelPrefix + "foo": "val",
				"app":                              "should-not-override-selector",
			},
			expectedKept:   map[string]string{"safe-key": "val"},
			expectedIgnore: 2,
		},
		{
			name:           "no reserved keys - nothing dropped, no warnings",
			keys:           map[string]string{"safe-key": "val", "another": "val2"},
			expectedKept:   map[string]string{"safe-key": "val", "another": "val2"},
			expectedIgnore: 0,
		},
		{
			name:           "nil/empty input returns nil",
			keys:           nil,
			expectedKept:   nil,
			expectedIgnore: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var infoCount int
			logger := logr.New(infoCountSink{count: &infoCount})

			filtered := Filter(logger, MetadataTypeAnnotation, tc.keys, isReserved)
			require.Equal(t, tc.expectedKept, filtered)
			assert.Equal(t, tc.expectedIgnore, infoCount, "expected a log line per reserved key dropped")
		})
	}
}

func TestMerge(t *testing.T) {
	testCases := []struct {
		name      string
		base      map[string]string
		additions map[string]string
		expected  map[string]string
	}{
		{
			name:      "additions merged over base, conflicts overridden",
			base:      map[string]string{"existing": "val", "conflict": "old"},
			additions: map[string]string{"new": "val", "conflict": "new"},
			expected:  map[string]string{"existing": "val", "new": "val", "conflict": "new"},
		},
		{
			name:      "nil base initialized correctly",
			base:      nil,
			additions: map[string]string{"k": "v"},
			expected:  map[string]string{"k": "v"},
		},
		{
			name:      "empty additions is a no-op and returns base unchanged",
			base:      map[string]string{"existing": "val"},
			additions: nil,
			expected:  map[string]string{"existing": "val"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var baseCopy map[string]string
			if tc.base != nil {
				baseCopy = make(map[string]string, len(tc.base))
				maps.Copy(baseCopy, tc.base)
			}

			merged := Merge(tc.base, tc.additions)
			require.Equal(t, tc.expected, merged)
			// base must never be mutated: it may be shared with another object
			// (e.g. a Deployment's labels map reused for its Pod template).
			assert.Equal(t, baseCopy, tc.base)
		})
	}
}

func TestMergeAnnotationsTracked(t *testing.T) {
	merged := MergeAnnotationsTracked(map[string]string{"existing": "val"}, map[string]string{"new": "val"})
	assert.Equal(t, "val", merged["existing"])
	assert.Equal(t, "val", merged["new"])
	assert.JSONEq(t, `{"new":"val"}`, merged[consts.AnnotationLastAppliedAnnotations])
}

func TestMergeAnnotationsTracked_EmptyAdditionsDoesNotMutateBase(t *testing.T) {
	base := map[string]string{"existing": "val"}

	merged := MergeAnnotationsTracked(base, nil)

	assert.Equal(t, map[string]string{"existing": "val"}, base, "base must never be mutated")
	assert.Equal(t, "val", merged["existing"])
	assert.JSONEq(t, `null`, merged[consts.AnnotationLastAppliedAnnotations])
}

func TestExtractOutdated(t *testing.T) {
	testCases := []struct {
		name        string
		currentSpec map[string]string
		existing    map[string]string
		expected    map[string]string
		wantErr     bool
	}{
		{
			name:     "nil existing annotations returns nil",
			existing: nil,
			expected: nil,
		},
		{
			name:     "no last-applied tracking annotation returns nil",
			existing: map[string]string{"foo": "bar"},
			expected: nil,
		},
		{
			name: "keys removed from spec since last reconcile are returned",
			existing: map[string]string{
				"foo":                                   "bar",
				"baz":                                   "qux",
				consts.AnnotationLastAppliedAnnotations: `{"foo":"bar","baz":"qux"}`,
			},
			currentSpec: map[string]string{"foo": "bar"},
			expected:    map[string]string{"baz": "qux"},
		},
		{
			name: "malformed tracking annotation returns an error",
			existing: map[string]string{
				consts.AnnotationLastAppliedAnnotations: `not-json`,
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			outdated, err := ExtractOutdated(tc.currentSpec, tc.existing)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, outdated)
		})
	}
}
