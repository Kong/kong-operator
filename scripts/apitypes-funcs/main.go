package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

type supportedTypesT struct {
	PackageVersion string

	AdditionalImports []string

	Types []templateDataT
}

type templateDataT struct {
	// Type is the name of the type.
	Type string

	// KonnectStatusType is the name of the konnect status type (.status.konnect).
	// If it's not provided Konnect status functions will not be generated.
	KonnectStatusType string

	// KonnectStatusEmbedded is true if the Konnect status is embedded in the type's status.
	KonnectStatusEmbedded bool

	// GetKonnectStatusReturnType is the return type of the GetKonnectStatus function.
	GetKonnectStatusReturnType string

	// ControlPlaneRefType is the ControlPlaneRef type to be used in the template (with the package name if it's outside
	// the type's package).
	ControlPlaneRefType string

	// ControlPlaneRefRequired is true if the ControlPlaneRef is required for the type.
	ControlPlaneRefRequired bool

	// ControlPlaneRefFieldPath is the field path to the ControlPlaneRef in the type.
	// If unspecified, the default is Spec.ControlPlaneRef.
	ControlPlaneRefFieldPath string

	// ServiceRefType is the ServiceRef type to be used in the template (with the package name if it's outside
	// the type's package).
	ServiceRefType string

	// NoStatusConditions is true if the type does not have status conditions.
	NoStatusConditions bool
}

const (
	apiPackageName             = "api"
	configurationPackageName   = "configuration"
	konnectPackageName         = "konnect"
	gatewayOperatorPackageName = "gateway-operator"
)

func main() {
	type render struct {
		templateContent string
		outputFile      string
		supportedTypes  []supportedTypesT
	}
	type templatePipeline struct {
		packagename string
		renders     []render
	}
	templateRenderingPipeline := []templatePipeline{
		{
			packagename: configurationPackageName,
			renders: []render{
				{
					templateContent: konnectFuncTemplate,
					outputFile:      konnectFuncOutputFileName,
					supportedTypes:  supportedKonnectTypesWithControlPlaneConfig,
				},
				{
					templateContent: listFuncTemplate,
					outputFile:      listFuncOutputFileName,
					supportedTypes:  supportedConfigurationPackageTypesWithList,
				},
				{
					templateContent: getAdoptFuncTemplate,
					outputFile:      adoptFuncOutputFileName,
					supportedTypes:  supportedConfigurationPackageTypesWithAdopt,
				},
			},
		},
		{
			packagename: konnectPackageName,
			renders: []render{
				{
					templateContent: konnectFuncTemplate,
					outputFile:      konnectFuncOutputFileName,
					supportedTypes:  supportedKonnectTypesWithControlPlaneRef,
				},
				{
					templateContent: konnectFuncStandaloneTemplate,
					outputFile:      konnectFuncOutputStandaloneFileName,
					supportedTypes:  supportedKonnectTypesStandalone,
				},
				{
					templateContent: konnectFuncNetworkRefTemplate,
					outputFile:      konnectFuncOutputCloudGatewayFilename,
					supportedTypes:  supportedKonnectV1Alpha1TypesWithNetworkRef,
				},
				{
					templateContent: listFuncTemplate,
					outputFile:      listFuncOutputFileName,
					supportedTypes:  supportedKonnectPackageTypesWithList,
				},
				{
					templateContent: getAdoptFuncTemplate,
					outputFile:      adoptFuncOutputFileName,
					supportedTypes:  supportedKonnectPackageTypesWithAdopt,
				},
			},
		},
		{
			packagename: gatewayOperatorPackageName,
			renders: []render{
				{
					templateContent: listFuncTemplate,
					outputFile:      listFuncOutputFileName,
					supportedTypes:  supportedGatewayOperatorPackageTypesWithList,
				},
			},
		},
	}

	for _, p := range templateRenderingPipeline {
		packagename := p.packagename
		for _, r := range p.renders {
			if err := renderTemplate(r.templateContent, r.outputFile, r.supportedTypes, packagename); err != nil {
				panic(err)
			}
		}
	}
}

func renderTemplate(
	templateContent string,
	outputFile string,
	supportedTypes []supportedTypesT,
	packagename string,
) error {
	log := slog.With("packageName", packagename, "outputFile", outputFile)

	tpl, err := template.New("tpl").Funcs(sprig.TxtFuncMap()).Parse(templateContent)
	if err != nil {
		return fmt.Errorf("failed to parse template for %s: %w", outputFile, err)
	}
	for _, st := range supportedTypes {
		contents := &bytes.Buffer{}
		path := filepath.Join(apiPackageName, packagename, st.PackageVersion, outputFile)
		if err := tpl.Execute(contents, st); err != nil {
			return fmt.Errorf("%s: failed to execute template for %s: %w", path, outputFile, err)
		}

		log.Info("Writing to file", "path", path)
		if err := os.WriteFile(path, contents.Bytes(), 0o600); err != nil {
			return fmt.Errorf("%s: failed to write file %s: %w", path, outputFile, err)
		}
	}
	return nil
}
