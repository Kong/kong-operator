package generator

// WatchFileInfo is metadata returned from generateReconcilerFiles so the
// caller (Runner) can assemble the cross-group watch dispatcher.
type WatchFileInfo struct {
	Entity         string // PascalCase entity name, e.g. "Portal"
	APIAlias       string // Go import alias, e.g. "konnectv1alpha1"
	APIPackagePath string // Go import path for the API types package
}

// GenerateWatchDispatcher emits controller/konnect/zz_generated_watch.go with
// reconciliationWatchOptionsForEntity[T,TEnt]. Call after all per-group
// generation has finished.
func GenerateWatchDispatcher(infos []*WatchFileInfo) (*GeneratedFile, error) {
	flat := make([]flatInfo, 0, len(infos))
	for _, info := range infos {
		flat = append(flat, flatInfo{
			Entity:         info.Entity,
			APIAlias:       info.APIAlias,
			APIPackagePath: info.APIPackagePath,
		})
	}
	return buildDispatcherFile("zz_generated_watch.go", watchDispatcherTemplate, "controller/konnect", flat)
}
