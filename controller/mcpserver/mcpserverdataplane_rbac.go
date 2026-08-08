package mcpserver

// +kubebuilder:rbac:groups=mcp.konghq.com,resources=mcpserverdataplanes,verbs=create;get;list;watch;update;patch
// +kubebuilder:rbac:groups=mcp.konghq.com,resources=mcpserverdataplanes/status,verbs=update;patch
// +kubebuilder:rbac:groups=mcp.konghq.com,resources=mcpserverdataplanes/finalizers,verbs=update
