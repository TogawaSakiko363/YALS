package plugin

// StreamingCallback is called for each chunk of output during plugin execution
type StreamingCallback func(output string, isError bool, isComplete bool)

// Plugin represents a command plugin interface
type Plugin interface {
	// Execute runs the plugin with the given target and returns formatted output
	Execute(target string) (string, error)
	// ExecuteStreaming runs the plugin with streaming output
	ExecuteStreaming(target string, callback StreamingCallback) error
	// GetName returns the plugin name
	GetName() string
	// GetDescription returns the plugin description
	GetDescription() string
}

// PluginWithID represents a plugin that supports command ID for stop functionality
type PluginWithID interface {
	Plugin
	ExecuteStreamingWithID(target, commandID string, callback StreamingCallback) error
}
