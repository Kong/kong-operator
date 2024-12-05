package consts

const (
	// CleanupPluginBindingFinalizer is the finalizer that is attached to entities that
	// are referenced as targets by managed KongPluginBindings (binding instances created
	// by the controller out of entities' konghq.com/plugins annotations).
	// This finalizer is used by the controller to be sure that whenever an entity is deleted,
	// all the targeting managed KongPluginBindings are deleted as well.
	CleanupPluginBindingFinalizer = "gateway.konghq.com/cleanup-plugin-binding"
	// PluginInUseFinalizer is the finalizer attached to KongPlugin resources that are
	// properly referenced by KongPluginBindings.
	// It avoids that KongPlugins get deleted when KongPluginBindings are still referencing them.
	PluginInUseFinalizer = "gateway.konghq.com/plugin-in-use"
)
