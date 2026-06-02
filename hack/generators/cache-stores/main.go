package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"text/template"

	"github.com/samber/lo"
)

type cacheStoreSupportedType struct {
	// Type is the name of the type that the store supports (e.g. Ingress, Service, etc.).
	Type string

	// Package is the package name of the type (e.g. gatewayapi, netv1, etc.).
	Package string

	// KeyFunc is a function to be used as the type's store's KeyFunc.
	// Defaults to `namespacedKeyFunc` if not provided.
	KeyFunc string

	// StoreField is the name of the field in the CacheStores struct that holds the cache store.
	// Optional: if not provided, the field name will be the same as the type name.
	StoreField string
}

const (
	clusterWideKeyFunc string = "clusterWideKeyFunc"
)

func main() {
	lo.Must0(renderTemplate(cacheStoresTemplate, cacheStoresOutputFile))
	lo.Must0(renderTemplate(cacheStoresTestTemplate, cacheStoresTestOutputFile))
}

func parseTemplate(templateContent string) (*template.Template, error) {
	return template.New("tpl").Funcs(template.FuncMap{
		"default": func(defaultValue, value string) string {
			if value == "" {
				return defaultValue
			}
			return value
		},
	}).Parse(templateContent)
}

func renderTemplate(templateContent string, outputFile string) error {
	tpl, err := parseTemplate(templateContent)
	if err != nil {
		return fmt.Errorf("failed to parse template for %s: %w", outputFile, err)
	}
	contents := &bytes.Buffer{}
	if err := tpl.Execute(contents, supportedTypes); err != nil {
		return fmt.Errorf("failed to execute template for %s: %w", outputFile, err)
	}
	formatted, err := format.Source(contents.Bytes())
	if err != nil {
		return fmt.Errorf("failed to format file %s: %w", outputFile, err)
	}
	if err := os.WriteFile(outputFile, formatted, 0o600); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outputFile, err)
	}
	return nil
}
