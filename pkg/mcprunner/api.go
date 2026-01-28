package mcprunner

// Runner represents an MCP Runner entity.
type Runner struct {
	// ID is the unique identifier of the MCP Runner.
	ID string `json:"id"`

	// Name is the name of the MCP Runner.
	Name string `json:"name"`

	// Version is the version of the MCP Runner.
	Version string `json:"version"`
}

// ListRunnersResponse represents the response from listing MCP Runners.
// The API returns a direct JSON array, so this is just an alias for a slice of Runners.
type ListRunnersResponse []Runner
