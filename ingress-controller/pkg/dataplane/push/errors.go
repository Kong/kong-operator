package push

// ResponseParsingError is an error type that is returned when the response from Kong is not in the expected format.
type ResponseParsingError struct {
	responseBody []byte
}

// NewResponseParsingError creates a ResponseParsingError for the given response body.
func NewResponseParsingError(responseBody []byte) ResponseParsingError {
	return ResponseParsingError{responseBody: responseBody}
}

// Error implements the Error interface.
func (e ResponseParsingError) Error() string {
	return "failed to parse Kong error response"
}

// ResponseBody returns the raw HTTP response body from Kong that could not be parsed.
func (e ResponseParsingError) ResponseBody() []byte {
	return e.responseBody
}
