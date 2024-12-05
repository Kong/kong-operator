package envtest

const (
	// ErrBodyDataConstraintError is a JSON error response body for a data constraint error.
	ErrBodyDataConstraintError = `{
			"code": 3,
			"message": "data constraint error",
			"details": [
				{
					"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
					"type": "ERROR_TYPE_REFERENCE",
					"field": "name",
					"messages": [
						"name (type: unique) constraint failed"
					]
				}
			]
		}`
)
