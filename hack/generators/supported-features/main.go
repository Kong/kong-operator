package main

import (
	"bytes"
	"go/format"
	"log"
	"os"

	"text/template"

	"github.com/Masterminds/sprig/v3"
	"sigs.k8s.io/yaml"

	conformancev1 "sigs.k8s.io/gateway-api/conformance/apis/v1"
)

const (
	conformanceReportPath = "current-conformance-report.yaml"
	outputFilePath        = "pkg/gatewayapi/zz_generated.supportedfeatures.go"
	templateName          = "supported-features"
)

func main() {
	rawReport, err := os.ReadFile(conformanceReportPath)
	if err != nil {
		log.Fatal(err)
	}

	report := &conformancev1.ConformanceReport{}
	if err = yaml.Unmarshal(rawReport, report); err != nil {
		log.Fatal("Failed to unmarshal conformance report:", err)
	}

	tpl, err := template.New(templateName).Funcs(sprig.TxtFuncMap()).Parse(supportedFeaturesTemplate)
	if err != nil {
		log.Fatalf("Failed to parse template %s: %v", templateName, err)
	}
	buf := &bytes.Buffer{}
	if err = tpl.Execute(buf, nil); err != nil {
		log.Fatalf("Failed to template %s: %v", templateName, err)
	}
	formattedBuf, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatalf("Failed to format %s: %v", templateName, err)
	}
	if err := os.WriteFile(outputFilePath, formattedBuf, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", outputFilePath, err)
	}
}
