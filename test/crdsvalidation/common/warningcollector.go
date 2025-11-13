package common

import "sync"

// WarningCollector implements the rest.WarningHandler interface.
type WarningCollector struct {
	sync.Mutex
	warnings []string
}

// HandleWarningHeader implements the rest.WarningHandler interface.
func (wc *WarningCollector) HandleWarningHeader(_ int, _ string, warning string) {
	wc.Lock()
	defer wc.Unlock()
	wc.warnings = append(wc.warnings, warning)
}

// GetWarnings returns the collected warnings.
func (wc *WarningCollector) GetWarnings() []string {
	wc.Lock()
	defer wc.Unlock()
	// Return a copy to avoid race conditions if the slice is modified elsewhere
	warningsCopy := make([]string, len(wc.warnings))
	copy(warningsCopy, wc.warnings)
	return warningsCopy
}
