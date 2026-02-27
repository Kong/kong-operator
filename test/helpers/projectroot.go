package helpers

import (
	"path/filepath"
	"runtime"
)

// ProjectRootPath returns the root directory of this project.
func ProjectRootPath() string {
	_, b, _, _ := runtime.Caller(0) //nolint:dogsled

	// Returns root directory of this project.
	// NOTE: it depends on the path of this file itself. When the file is moved, the second param may need updating.
	return filepath.Join(filepath.Dir(b), "../..")
}
