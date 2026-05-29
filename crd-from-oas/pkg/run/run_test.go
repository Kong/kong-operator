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
	reconcilerConditions := filepath.Join(dir, "zz_generated_reconciler_conditions.go")
	keepFile := filepath.Join(dir, "zz_generated_portal_funcs.go")

	require.NoError(t, os.WriteFile(legacyFuncs, []byte("legacy"), 0o600))
	require.NoError(t, os.WriteFile(sharedReconcilerFuncs, []byte("legacy"), 0o600))
	require.NoError(t, os.WriteFile(reconcilerConditions, []byte("legacy"), 0o600))
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
	require.NoFileExists(t, reconcilerConditions)
	require.FileExists(t, keepFile)
}

func TestCleanupRenamedGeneratedFiles(t *testing.T) {
	projectRoot := t.TempDir()
	dir := filepath.Join(projectRoot, "api", "konnect", "v1alpha1")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectRoot, "controller/konnect/ops"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectRoot, "controller/konnect"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(projectRoot, "internal/utils/index"), 0o755))

	oldFiles := []string{
		filepath.Join(dir, "zz_generated_identityproviderrequest_types.go"),
		filepath.Join(dir, "zz_generated_identityproviderrequest_sdkops.go"),
		filepath.Join(projectRoot, "controller/konnect/ops", "zz_generated_ops_identityproviderrequest.go"),
		filepath.Join(projectRoot, "controller/konnect/ops", "zz_generated_ops_identityproviderrequest_test.go"),
		filepath.Join(projectRoot, "controller/konnect", "zz_generated_watch_identityproviderrequest.go"),
		filepath.Join(projectRoot, "internal/utils/index", "zz_generated_identityproviderrequest.go"),
	}
	for _, filePath := range oldFiles {
		require.NoError(t, os.WriteFile(filePath, []byte("old"), 0o600))
	}

	newFile := filepath.Join(projectRoot, "controller/konnect/ops", "zz_generated_ops_portalidentityproviderrequest.go")
	require.NoError(t, os.WriteFile(newFile, []byte("new"), 0o600))

	require.NoError(t, cleanupRenamedGeneratedFiles(projectRoot, dir, []generatedEntityRename{{
		OldEntity: "IdentityProviderRequest",
		NewEntity: "PortalIdentityProviderRequest",
	}}))

	for _, filePath := range oldFiles {
		require.NoFileExists(t, filePath)
	}
	require.FileExists(t, newFile)
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
			"ops_eventgatewaydataplanecertificate_manual.go",
		},
		handWrittenGetForUIDHelperFileNames("EventGatewayDataPlaneCertificate"),
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
