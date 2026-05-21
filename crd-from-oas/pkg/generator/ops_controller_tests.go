package generator

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/config"
	"github.com/kong/kong-operator/v2/crd-from-oas/pkg/parser"
)

type opsControllerTestField struct {
	FieldName string
	TestValue string
}

type opsControllerRootUnionFixture struct {
	UnionTypeName   string
	TypeConstName   string
	VariantField    string
	VariantTypeName string
	VariantValue    string
}

type opsControllerCreateTestData struct {
	*opsCreateFuncData

	MockConstructorName string
	ResponseType        string
}

type opsControllerUpdateTestData struct {
	*opsUpdateFuncData

	MockConstructorName string
	ResponseType        string
}

type opsControllerDeleteTestData struct {
	*opsDeleteFuncData

	MockConstructorName string
	ResponseType        string
}

type opsControllerTestFileData struct {
	Entity                string
	FixtureName           string
	APIAlias              string
	APIPackagePath        string
	FixtureFields         []opsControllerTestField
	RootUnion             *opsControllerRootUnionFixture
	Create                *opsControllerCreateTestData
	Update                *opsControllerUpdateTestData
	Delete                *opsControllerDeleteTestData
	NeedsFakeClient       bool
	NeedsComponentsImport bool
}

func (g *Generator) generateEntityOpsTestFile(
	entityName string,
	schema *parser.Schema,
	opsConfig *config.EntityOpsConfig,
) (*GeneratedFile, error) {
	createData, err := g.generateOpsCreateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed create test data for %s: %w", entityName, err)
	}
	updateData, err := g.generateOpsUpdateFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed update test data for %s: %w", entityName, err)
	}
	deleteData, err := g.generateOpsDeleteFuncBody(entityName, schema, opsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed delete test data for %s: %w", entityName, err)
	}

	if createData == nil && updateData == nil && deleteData == nil {
		return nil, nil
	}

	fixtureFields := g.buildOpsControllerTestFields(entityName, schema.Properties)
	rootUnion := buildOpsControllerRootUnionFixture(entityName, schema, g.config.APIGroupPackageAlias)

	data := opsControllerTestFileData{
		Entity:         entityName,
		FixtureName:    EntityFilePrefix(entityName),
		APIAlias:       g.config.APIGroupPackageAlias,
		APIPackagePath: g.config.APIGroupPackagePath,
		FixtureFields:  fixtureFields,
		RootUnion:      rootUnion,
	}

	if createData != nil {
		data.Create = &opsControllerCreateTestData{
			opsCreateFuncData:   createData,
			MockConstructorName: mockSDKConstructorName(createData.SDKInterface),
			ResponseType:        createData.SDKMethod + "Response",
		}
		data.NeedsFakeClient = data.NeedsFakeClient || createData.NeedsClient
		data.NeedsComponentsImport = true
	}
	if updateData != nil {
		data.Update = &opsControllerUpdateTestData{
			opsUpdateFuncData:   updateData,
			MockConstructorName: mockSDKConstructorName(updateData.UpdateSDKInterface),
			ResponseType:        updateData.UpdateSDKMethod + "Response",
		}
		data.NeedsFakeClient = data.NeedsFakeClient || updateData.NeedsClient
	}
	if deleteData != nil {
		data.Delete = &opsControllerDeleteTestData{
			opsDeleteFuncData:   deleteData,
			MockConstructorName: mockSDKConstructorName(deleteData.DeleteSDKInterface),
			ResponseType:        deleteData.DeleteSDKMethod + "Response",
		}
		if strings.HasPrefix(deleteData.DeletePutReqQualifiedType, "sdkkonnectcomp.") {
			data.NeedsComponentsImport = true
		}
	}

	tmpl := template.Must(template.New("ops-controller-tests").Parse(opsControllerTestTemplate))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}

	return &GeneratedFile{
		Name:        "zz_generated_ops_" + EntityFilePrefix(entityName) + "_test.go",
		Content:     buf.String(),
		RelativeDir: "controller/konnect/ops",
	}, nil
}

func (g *Generator) buildOpsControllerTestFields(entityName string, props []*parser.Property) []opsControllerTestField {
	testFields := make([]opsControllerTestField, 0, len(props))
	for _, prop := range props {
		if skipProperty(prop) || prop.IsReference {
			continue
		}
		if _, ok := g.entityAPISpecSensitiveLeaf(entityName, jsonName(prop.Name)); ok {
			testFields = append(testFields, opsControllerTestField{
				FieldName: goFieldName(prop.Name),
				TestValue: fmt.Sprintf(
					`%s.SensitiveDataSource{Type: %s.SensitiveDataSourceTypeInline, Value: new("test-value")}`,
					g.config.APIGroupPackageAlias,
					g.config.APIGroupPackageAlias,
				),
			})
			continue
		}
		goType := g.goType(prop)
		testValue := controllerOpsTestValueForProperty(prop, goType, g.config.APIGroupPackageAlias)
		if testValue == "" {
			continue
		}
		testFields = append(testFields, opsControllerTestField{
			FieldName: goFieldName(prop.Name),
			TestValue: testValue,
		})
	}
	return testFields
}

func buildOpsControllerRootUnionFixture(entityName string, schema *parser.Schema, apiAlias string) *opsControllerRootUnionFixture {
	if !hasRootOneOf(schema) || len(schema.OneOf) == 0 {
		return nil
	}

	switch entityName {
	case "EventGatewayListenerPolicy":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "EventGatewayListenerPolicyConfig",
			TypeConstName:   "EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen",
			VariantField:    "EventGatewayTLSListen",
			VariantTypeName: "EventGatewayTLSListenerPolicy",
			VariantValue: fmt.Sprintf(
				`&%[1]s.EventGatewayTLSListenerPolicy{Config: %[1]s.EventGatewayTLSListenerPolicyConfig{Certificates: []%[1]s.TLSCertificate{{Certificate: %[1]s.GatewaySecretReferenceOrLiteral("certificate"), Key: %[1]s.GatewaySecret("key")}}}}`,
				apiAlias,
			),
		}
	case "EventGatewayVirtualClusterConsumePolicy":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "EventGatewayVirtualClusterConsumePolicyConfig",
			TypeConstName:   "EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate",
			VariantField:    "ModifyHeadersPolicyCreate",
			VariantTypeName: "EventGatewayModifyHeadersPolicyCreate",
			VariantValue: fmt.Sprintf(
				`&%[1]s.EventGatewayModifyHeadersPolicyCreate{Config: %[1]s.EventGatewayModifyHeadersPolicyCreateConfig{Actions: []%[1]s.EventGatewayModifyHeaderAction{{Op: %[1]s.EventGatewayModifyHeaderActionTypeSet, Set: &%[1]s.EventGatewayModifyHeaderSetAction{Key: "x-added-header", Value: "added-value"}}}}}`,
				apiAlias,
			),
		}
	case "EventGatewayVirtualClusterProducePolicy":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "EventGatewayVirtualClusterProducePolicyConfig",
			TypeConstName:   "EventGatewayVirtualClusterProducePolicyConfigTypeModifyHeadersPolicyCreate",
			VariantField:    "ModifyHeadersPolicyCreate",
			VariantTypeName: "EventGatewayModifyHeadersPolicyCreate",
			VariantValue: fmt.Sprintf(
				`&%[1]s.EventGatewayModifyHeadersPolicyCreate{Config: %[1]s.EventGatewayModifyHeadersPolicyCreateConfig{Actions: []%[1]s.EventGatewayModifyHeaderAction{{Op: %[1]s.EventGatewayModifyHeaderActionTypeSet, Set: &%[1]s.EventGatewayModifyHeaderSetAction{Key: "x-added-header", Value: "added-value"}}}}}`,
				apiAlias,
			),
		}
	}

	rootUnionTypeName := goFieldName(entityName + "Config")
	rawVariantNames := make([]string, 0, len(schema.OneOf))
	for _, variant := range schema.OneOf {
		variantName := variant.Name
		if variant.RefName != "" {
			variantName = variant.RefName
		}
		rawVariantNames = append(rawVariantNames, variantName)
	}
	variantNames := extractVariantNames(rawVariantNames)
	firstVariant := schema.OneOf[0]
	variantRefName := firstVariant.Name
	if firstVariant.RefName != "" {
		variantRefName = firstVariant.RefName
	}
	fieldName := fixInitialisms(variantNames[0])

	return &opsControllerRootUnionFixture{
		UnionTypeName:   rootUnionTypeName,
		TypeConstName:   fmt.Sprintf("%sType%s", rootUnionTypeName, fieldName),
		VariantField:    fieldName,
		VariantTypeName: fixInitialisms(variantRefName),
		VariantValue:    fmt.Sprintf(`&%s.%s{}`, apiAlias, fixInitialisms(variantRefName)),
	}
}

func controllerOpsTestValueForProperty(prop *parser.Property, goType, apiAlias string) string {
	if prop.Type == "boolean" {
		return `"Enabled"`
	}

	switch goType {
	case "string":
		return `"test-value"`
	case "*string":
		return `new("test-value")`
	case "bool":
		return "true"
	case "*bool":
		return `new(true)`
	case "int", "int32", "int64":
		return "1"
	case "float32", "float64":
		return "1.0"
	case "[]string":
		return `[]string{"test-value"}`
	}

	if prop.RefName != "" {
		switch prop.Type {
		case "string":
			if elementType, ok := strings.CutPrefix(goType, "*"); ok {
				return fmt.Sprintf(`new(%s.%s("test-value"))`, apiAlias, elementType)
			}
			return fmt.Sprintf(`%s.%s("test-value")`, apiAlias, goType)
		case "object":
			if prop.AdditionalProperties != nil && prop.AdditionalProperties.Type == "string" {
				return fmt.Sprintf(`%s.%s{"test-key": "test-value"}`, apiAlias, goType)
			}
		}
	}

	return ""
}
