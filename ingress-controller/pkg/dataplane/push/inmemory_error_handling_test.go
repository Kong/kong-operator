package push_test

import (
	"testing"

	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
)

func TestParseFlatEntityErrors(t *testing.T) {
	logger := zapr.NewLogger(zap.NewNop())

	t.Run("empty body", func(t *testing.T) {
		entities, err := dataplanepush.ParseFlatEntityErrors(nil, logger)
		require.NoError(t, err)
		require.Empty(t, entities)
	})

	t.Run("malformed body", func(t *testing.T) {
		entities, err := dataplanepush.ParseFlatEntityErrors([]byte(`{"message":`), logger)
		require.Empty(t, entities)

		var parseErr dataplanepush.ResponseParsingError
		require.ErrorAs(t, err, &parseErr)
		require.Equal(t, []byte(`{"message":`), parseErr.ResponseBody())
	})

	t.Run("flattened entity errors", func(t *testing.T) {
		body := []byte(`{
			"flattened_errors": [{
				"entity_type": "certificates",
				"entity_name": "ca-cert-a",
				"entity_id": "id-1",
				"entity_tags": ["ai-gateway-name:ca-cert-a"],
				"errors": [
					{"type": "field", "field": "cert", "message": "invalid certificate"},
					{"type": "entity", "message": "invalid entity"},
					{"type": "field", "field": "tags", "messages": ["bad tag 1", "bad tag 2"]}
				]
			}]
		}`)

		entities, err := dataplanepush.ParseFlatEntityErrors(body, logger)
		require.NoError(t, err)
		require.Len(t, entities, 1)
		require.Equal(t, "certificates", entities[0].Type)
		require.Equal(t, "ca-cert-a", entities[0].Name)
		require.Equal(t, "id-1", entities[0].ID)
		require.Equal(t, []string{"ai-gateway-name:ca-cert-a"}, entities[0].Tags)
		require.Len(t, entities[0].Errors, 3)
		require.Equal(t, dataplanepush.FlatErrorTypeField, entities[0].Errors[0].Type)
		require.Equal(t, "invalid certificate", entities[0].Errors[0].Message)
		require.Equal(t, dataplanepush.FlatErrorTypeEntity, entities[0].Errors[1].Type)
		require.Equal(t, []string{"bad tag 1", "bad tag 2"}, entities[0].Errors[2].Messages)
	})

	t.Run("response without flattened errors", func(t *testing.T) {
		entities, err := dataplanepush.ParseFlatEntityErrors([]byte(`{"message":"invalid config"}`), logger)
		require.NoError(t, err)
		require.Empty(t, entities)
	})
}
