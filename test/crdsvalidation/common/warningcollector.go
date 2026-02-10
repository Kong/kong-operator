package common

import "sync"

// WarningCollector implements the rest.WarningHandler interface.
// It's safe for concurrent use.
type WarningCollector struct {
	mut sync.Mutex

	warnings []string
}

// HandleWarningHeader implements the rest.WarningHandler interface.
func (wc *WarningCollector) HandleWarningHeader(_ int, _ string, warning string) {
	wc.mut.Lock()
	defer wc.mut.Unlock()
	wc.warnings = append(wc.warnings, warning)
}

// GetWarnings returns the collected warnings.
func (wc *WarningCollector) GetWarnings() []string {
	wc.mut.Lock()
	defer wc.mut.Unlock()
	// Return a copy to avoid race conditions if the slice is modified elsewhere
	warningsCopy := make([]string, len(wc.warnings))
	copy(warningsCopy, wc.warnings)
	return warningsCopy
}
