package generator

import (
	"fmt"
	"strings"
)

// ParseSDKTypePath splits a fully qualified SDK type path like
// "github.com/Kong/sdk-konnect-go/models/components.CreatePortal"
// into its import path and type name by splitting on the last ".".
func ParseSDKTypePath(path string) (importPath, typeName string, err error) {
	lastDot := strings.LastIndex(path, ".")
	if lastDot == -1 || lastDot == 0 || lastDot == len(path)-1 {
		return "", "", fmt.Errorf("invalid SDK type path %q: must be in format 'importpath.TypeName'", path)
	}
	return path[:lastDot], path[lastDot+1:], nil
}
