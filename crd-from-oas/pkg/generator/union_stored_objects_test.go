package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmitDiscriminatedUnionCode_StoredObjectsPass(t *testing.T) {
	variants := []unionVariant{
		{discValue: "id", fieldName: "ID", goTypeName: "SchemaRegistryReferenceByID"},
		{discValue: "name", fieldName: "Name", goTypeName: "SchemaRegistryReferenceByName"},
	}

	code := emitDiscriminatedUnionCode("EventGatewayProduceSchemaValidationPolicyJSONConfigSchemaRegistry", "schema_registry", "type", variants)
	assert.Contains(t, code,
		`rule="(oldSelf.hasValue() && oldSelf.value() == self) || (self.type == 'id' ? has(self.id) && has(self.id.id) : !has(self.id))",message="id must be set only when type is id",optionalOldSelf=true`,
		"a union added after its CRD was released lets unchanged values pass, and requires the id of a by-id member",
	)
	assert.Contains(t, code,
		`rule="(oldSelf.hasValue() && oldSelf.value() == self) || (self.type == 'name' ? has(self.name) : !has(self.name))",message="name must be set only when type is name",optionalOldSelf=true`,
	)

	code = emitDiscriminatedUnionCode("EncryptionKey", "encryption_key", "type", variants)
	assert.Contains(t, code, `rule="self.type == 'id' ? has(self.id) : !has(self.id)",message="id must be set only when type is id"`+"\n",
		"the rules of other unions are unchanged",
	)
}
