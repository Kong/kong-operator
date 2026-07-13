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
		if leafType, ok := g.entityAPISpecFieldSensitiveType(entityName, jsonName(prop.Name)); ok {
			typeName := "SensitiveDataSource"
			innerValue := `"test-value"`
			if leafType.DedicatedTypeName != "" {
				typeName = leafType.DedicatedTypeName
				innerValue = controllerOpsTestValueForProperty(prop, leafType.ValueGoType, g.config.APIGroupPackageAlias)
				if innerValue == "" {
					// No simple literal representation for this value type (e.g. a
					// free-form JSON object) — skip, matching how a non-sensitive
					// field of the same complex type is already skipped below.
					continue
				}
			}
			testFields = append(testFields, opsControllerTestField{
				FieldName: goFieldName(prop.Name),
				TestValue: fmt.Sprintf(
					`%s.%s{Type: %s.SensitiveDataSourceTypeInline, Value: new(%s)}`,
					g.config.APIGroupPackageAlias,
					typeName,
					g.config.APIGroupPackageAlias,
					innerValue,
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
				`&%[1]s.EventGatewayTLSListenerPolicy{Config: %[1]s.EventGatewayTLSListenerPolicyConfig{Certificates: []%[1]s.TLSCertificate{{Certificate: %[1]s.SensitiveDataSource{Type: %[1]s.SensitiveDataSourceTypeInline, Value: new("certificate")}, Key: %[1]s.SensitiveDataSource{Type: %[1]s.SensitiveDataSourceTypeInline, Value: new("key")}}}}}`,
				apiAlias,
			),
		}
	case "EventGatewayVirtualClusterPolicy":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "EventGatewayVirtualClusterPolicyConfig",
			TypeConstName:   "EventGatewayVirtualClusterPolicyConfigTypeEventGatewayACLsPolicy",
			VariantField:    "EventGatewayACLsPolicy",
			VariantTypeName: "EventGatewayACLsPolicy",
			VariantValue: fmt.Sprintf(
				`&%[1]s.EventGatewayACLsPolicy{Config: %[1]s.EventGatewayACLPolicyConfig{Rules: []%[1]s.EventGatewayACLRule{{Action: "allow", ResourceType: "topic", Operations: []%[1]s.EventGatewayACLOperation{{Name: "read"}}, ResourceNames: &%[1]s.EventGatewayACLRuleResourceNames{Type: %[1]s.EventGatewayACLRuleResourceNamesTypeStat, Stat: &%[1]s.EventGatewayACLRuleResourceNamesStaticArray{{Match: "orders.*"}}}}}}}`,
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
	case "AIGatewayModel":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "AIGatewayModelConfig",
			TypeConstName:   "AIGatewayModelConfigTypeAPI",
			VariantField:    "API",
			VariantTypeName: "AIGatewayModelAPI",
			VariantValue: fmt.Sprintf(
				`&%[1]s.AIGatewayModelAPI{DisplayName: "test-display-name", Name: "test-model", Capabilities: []string{"llm/v1/chat"}, Formats: []%[1]s.AIGatewayModelFormat{{Type: "openai"}}, Config: %[1]s.AIGatewayModelAPIConfig{Model: %[1]s.AIGatewayModelAPIConfigModel{Alias: "test-alias"}, Route: %[1]s.AIGatewayRouteConfig{Paths: []string{"/chat"}}}, Targets: []%[1]s.AIGatewayTarget{{Name: "target-model", Provider: "provider-1", Config: &%[1]s.AIGatewayTargetConfig{Type: %[1]s.AIGatewayTargetConfigTypeAnthropic, Anthropic: &%[1]s.AIGatewayTargetAnthropicConfig{}}}}}`,
				apiAlias,
			),
		}
	case "AIGatewayIdentityProvider":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "AIGatewayIdentityProviderConfig",
			TypeConstName:   "AIGatewayIdentityProviderConfigTypeKeyAuth",
			VariantField:    "KeyAuth",
			VariantTypeName: "AIGatewayIdentityProviderKeyAuth",
			VariantValue: fmt.Sprintf(
				`&%[1]s.AIGatewayIdentityProviderKeyAuth{DisplayName: "test-display-name", Name: "test-identity-provider"}`,
				apiAlias,
			),
		}
	case "AIGatewayModelProvider":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "AIGatewayModelProviderConfig",
			TypeConstName:   "AIGatewayModelProviderConfigTypeAnthropic",
			VariantField:    "Anthropic",
			VariantTypeName: "AIGatewayModelProviderAnthropic",
			VariantValue: fmt.Sprintf(
				`&%[1]s.AIGatewayModelProviderAnthropic{DisplayName: "test-display-name", Name: "test-provider", Config: %[1]s.AIGatewayModelProviderAnthropicConfig{Auth: %[1]s.AIGatewayModelProviderConfigAuthBasic{Headers: []%[1]s.AIGatewayModelProviderConfigAuthBasicHeaders{{Name: "x-api-key", Value: %[1]s.SensitiveDataSource{Type: %[1]s.SensitiveDataSourceTypeInline, Value: new("test-value")}}}}}}`,
				apiAlias,
			),
		}
	case "AIGatewayMCPServer":
		return &opsControllerRootUnionFixture{
			UnionTypeName:   "AIGatewayMCPServerConfig",
			TypeConstName:   "AIGatewayMCPServerConfigTypeConversionOnly",
			VariantField:    "ConversionOnly",
			VariantTypeName: "AIGatewayMCPServerConversionOnly",
			VariantValue: fmt.Sprintf(
				`&%[1]s.AIGatewayMCPServerConversionOnly{DisplayName: "test-display-name", Name: "test-mcp-server", Config: %[1]s.AIGatewayMCPServerWithUpstreamNoProxyConfigNoServerConfig{URL: "https://example.com/mcp"}}`,
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

	// Enum-typed string fields must use a valid enum value; some SDK enum types
	// reject unknown values during unmarshalling.
	if len(prop.Enum) > 0 {
		quoted := fmt.Sprintf("%q", fmt.Sprintf("%v", prop.Enum[0]))
		switch {
		case goType == "string":
			return quoted
		case goType == "*string":
			return fmt.Sprintf("new(%s)", quoted)
		case prop.RefName != "" && prop.Type == "string" && !strings.HasPrefix(goType, "*"):
			return quoted
		}
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
			if strings.HasPrefix(goType, "*") {
				return ""
			}
			return `"test-value"`
		case "object":
			if prop.AdditionalProperties != nil && prop.AdditionalProperties.Type == "string" {
				return fmt.Sprintf(`%s.%s{"test-key": "test-value"}`, apiAlias, goType)
			}
		}
	}

	return ""
}
