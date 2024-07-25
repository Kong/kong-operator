package consts

const (
	// CleanupPluginBindingFinalizer is the finalizer attached to entities that are properly
	// referenced by KongPluginBindings, that should be cleaned up when the KongService
	// gets deleted.
	CleanupPluginBindingFinalizer = "gateway.konghq.com/cleanup-plugin-binding"
	// PluginInUseFinalizer is the finalizer attached to KongPlugin resources that are
	// properly referenced by KongPluginBindings.
	PluginInUseFinalizer = "gateway.konghq.com/plugin-in-use"
)
