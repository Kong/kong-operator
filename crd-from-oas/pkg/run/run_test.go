package run

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/generator"
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

	require.NoError(t, cleanupLegacyGeneratedFiles(t.TempDir(), dir, parsed))
	require.NoFileExists(t, legacyFuncs)
	require.NoFileExists(t, sharedReconcilerFuncs)
	require.FileExists(t, keepFile)
}

func TestHandWrittenOpsFileNamesIncludesLegacyKonnectName(t *testing.T) {
	require.Equal(
		t,
		[]string{"ops_konnect_eventgateway.go", "ops_konnecteventgateway.go"},
		handWrittenOpsFileNames("KonnectEventGateway"),
	)
}

func TestHandWrittenGetForUIDHelperFileNamesIncludesLegacyKonnectName(t *testing.T) {
	require.Equal(
		t,
		[]string{
			"ops_konnect_eventdataplanecertificate_manual.go",
			"ops_konnecteventdataplanecertificate_manual.go",
		},
		handWrittenGetForUIDHelperFileNames("KonnectEventDataPlaneCertificate"),
	)
}

func TestGeneratedOpsFileNameMatchesGeneratorConvention(t *testing.T) {
	require.Equal(t, "zz_generated_ops_portal.go", generatedOpsFileName("Portal"))
	require.Equal(
		t,
		"zz_generated_ops_konnect_eventgateway.go",
		generatedOpsFileName("KonnectEventGateway"),
	)
}

func TestEmitDispatcherFileWritesManagerControllerSetupDispatcher(t *testing.T) {
	projectRoot := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, emitDispatcherFile(
		projectRoot,
		logger,
		"modules/manager",
		"zz_generated_konnect_controller_setup.go",
		func() (*generator.GeneratedFile, error) {
			return &generator.GeneratedFile{
				Name:        "zz_generated_konnect_controller_setup.go",
				Content:     "package manager\n",
				RelativeDir: "modules/manager",
			}, nil
		},
	))

	require.FileExists(t, filepath.Join(projectRoot, "modules/manager", "zz_generated_konnect_controller_setup.go"))
}

func TestEmitDispatcherFileWritesManagerIndexOptionsDispatcher(t *testing.T) {
	projectRoot := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, emitDispatcherFile(
		projectRoot,
		logger,
		"modules/manager",
		"zz_generated_konnect_index_options.go",
		func() (*generator.GeneratedFile, error) {
			return &generator.GeneratedFile{
				Name:        "zz_generated_konnect_index_options.go",
				Content:     "package manager\n",
				RelativeDir: "modules/manager",
			}, nil
		},
	))

	require.FileExists(t, filepath.Join(projectRoot, "modules/manager", "zz_generated_konnect_index_options.go"))
}

func TestEmitDispatcherFileWritesGeneratedConstraintsDispatcher(t *testing.T) {
	projectRoot := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, emitDispatcherFile(
		projectRoot,
		logger,
		"controller/konnect/constraints",
		"zz_generated_supported_types.go",
		func() (*generator.GeneratedFile, error) {
			return &generator.GeneratedFile{
				Name:        "zz_generated_supported_types.go",
				Content:     "package constraints\n",
				RelativeDir: "controller/konnect/constraints",
			}, nil
		},
	))

	require.FileExists(t, filepath.Join(projectRoot, "controller/konnect/constraints", "zz_generated_supported_types.go"))
}

func TestEmitDispatcherFileWritesGeneratedKonnectAPIAuthDispatcher(t *testing.T) {
	projectRoot := t.TempDir()
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, emitDispatcherFile(
		projectRoot,
		logger,
		"controller/konnect",
		"zz_generated_konnectapiauth_watch.go",
		func() (*generator.GeneratedFile, error) {
			return &generator.GeneratedFile{
				Name:        "zz_generated_konnectapiauth_watch.go",
				Content:     "package konnect\n",
				RelativeDir: "controller/konnect",
			}, nil
		},
	))

	require.FileExists(t, filepath.Join(projectRoot, "controller/konnect", "zz_generated_konnectapiauth_watch.go"))
}
