package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"strings"
	"text/template"
)

// -----------------------------------------------------------------------------
// Main
// -----------------------------------------------------------------------------

const (
	outputDir   = "controller/aigateway/onpremconfig"
	boilerplate = "hack/generators/boilerplate.go.txt"

	aiconfigurationv1alpha1 = "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

var inputControllersNeeded = &typesNeeded{
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayAgent",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewayagents",
		CacheType:                        "AIGatewayAgent",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayAuthStrategy",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewayauthstrategies",
		CacheType:                        "AIGatewayAuthStrategy",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayCACertificate",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaycacertificates",
		CacheType:                        "AIGatewayCACertificate",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayCertificate",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaycertificates",
		CacheType:                        "AIGatewayCertificate",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayConsumer",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewayconsumers",
		CacheType:                        "AIGatewayConsumer",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayConsumerGroup",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewayconsumergroups",
		CacheType:                        "AIGatewayConsumerGroup",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayDataPlaneCertificate",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaydataplanecertificates",
		CacheType:                        "AIGatewayDataPlaneCertificate",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayMCPServer",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaymcpservers",
		CacheType:                        "AIGatewayMCPServer",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayModel",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaymodels",
		CacheType:                        "AIGatewayModel",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayModelProvider",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaymodelproviders",
		CacheType:                        "AIGatewayModelProvider",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewayPolicy",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaypolicies",
		CacheType:                        "AIGatewayPolicy",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
	typeNeeded{
		Group:                            "aiconfiguration.konghq.com",
		Version:                          "v1alpha1",
		Kind:                             "AIGatewaySNI",
		PackageImportAlias:               "aiconfigurationv1alpha1",
		PackageAlias:                     "aiconfigurationv1alpha1",
		Package:                          aiconfigurationv1alpha1,
		Plural:                           "aigatewaysnis",
		CacheType:                        "AIGatewaySNI",
		NeedsStatusPermissions:           true,
		ConfigStatusNotificationsEnabled: true,
		ProgrammedCondition: ProgrammedConditionConfiguration{
			UpdatesEnabled: true,
		},
		RBACVerbs: []string{"get", "list", "watch"},
	},
}

func main() {
	needed := necessary{
		types: inputControllersNeeded,
	}
	if err := needed.generate(); err != nil {
		fmt.Fprintf(os.Stderr, "could not generate input controllers: %v", err)
		os.Exit(1)
	}
}

// -----------------------------------------------------------------------------
// Private Functions - Helper
// -----------------------------------------------------------------------------

// boilerplateHeader produces the license header shared by every generated file.
func boilerplateHeader() (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)

	boilerPlate, err := os.ReadFile(boilerplate)
	if err != nil {
		return nil, err
	}

	_, err = buf.Write(boilerPlate)
	return buf, err
}

// -----------------------------------------------------------------------------
// Generator
// -----------------------------------------------------------------------------

// typesNeeded is a list of Kubernetes API types which are supported.
type typesNeeded []typeNeeded

type necessary struct {
	types *typesNeeded
}

// generate generates one controller/aigateway/onpremconfig/zz_generated.controller_<name>.go
// file per supported type.
func (needed necessary) generate() error {
	for _, t := range *needed.types {
		contents, err := boilerplateHeader()
		if err != nil {
			return err
		}
		if err := t.generate(contents); err != nil {
			return err
		}
		filename := fmt.Sprintf("%s/zz_generated.controller_%s.go", outputDir, strings.ToLower(t.Kind))
		if err := writeFormatted(filename, contents); err != nil {
			return err
		}
	}
	return nil
}

// writeFormatted gofmt's contents before writing it to disk so that templates
// don't need to hand-align struct fields, indentation, etc.
func writeFormatted(filename string, contents *bytes.Buffer) error {
	formatted, err := format.Source(contents.Bytes())
	if err != nil {
		return err
	}
	return os.WriteFile(filename, formatted, 0o600)
}

type typeNeeded struct {
	Group   string
	Version string
	Kind    string

	PackageImportAlias string
	PackageAlias       string
	Package            string
	Plural             string
	CacheType          string
	RBACVerbs          []string

	// NeedsStatusPermissions indicates whether permissions for the object should also include permissions to update
	// its status
	NeedsStatusPermissions bool

	// ConfigStatusNotificationsEnabled indicates that the controller should receive updates via the StatusQueue when the
	// configuration status of the resource changes.
	ConfigStatusNotificationsEnabled bool

	// ProgrammedCondition contains the configuration for the Programmed condition for the resource.
	ProgrammedCondition ProgrammedConditionConfiguration
}

// ProgrammedConditionConfiguration contains the configuration for the Programmed condition for a resource.
type ProgrammedConditionConfiguration struct {
	// UpdatesEnabled indicates that the controllers should manage the Programmed condition for the
	// resource.
	UpdatesEnabled bool

	// CustomUnknownMessage is the message to use for the Programmed condition when the configuration status is Unknown.
	CustomUnknownMessage string
}

func parseTemplate(name, contents string) (*template.Template, error) {
	return template.New(name).Funcs(template.FuncMap{
		"join": func(separator string, values []string) string {
			return strings.Join(values, separator)
		},
	}).Parse(contents)
}

func (t *typeNeeded) generate(contents *bytes.Buffer) error {
	tmpl, err := parseTemplate("controller", controllerFileTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(contents, t)
}
