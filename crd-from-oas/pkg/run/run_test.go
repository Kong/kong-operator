package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

func TestCleanupLegacyGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	legacyFuncs := filepath.Join(dir, "portal_funcs.go")
	sharedReconcilerFuncs := filepath.Join(dir, "zz_generated_reconciler_funcs.go")
	keepFile := filepath.Join(dir, "zz_generated_portal_funcs.go")

	require.NoError(t, os.WriteFile(legacyFuncs, []byte("legacy"), 0o600))
	require.NoError(t, os.WriteFile(sharedReconcilerFuncs, []byte("legacy"), 0o600))
	require.NoError(t, os.WriteFile(keepFile, []byte("current"), 0o600))

	parsed := &parser.ParsedSpec{
		RequestBodies: map[string]*parser.Schema{
			"CreatePortal": {
				Name: "CreatePortal",
			},
		},
	}

	require.NoError(t, cleanupLegacyGeneratedFiles(dir, parsed))
	require.NoFileExists(t, legacyFuncs)
	require.NoFileExists(t, sharedReconcilerFuncs)
	require.FileExists(t, keepFile)
}
