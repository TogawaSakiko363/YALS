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

// PluginWithConfig represents a plugin that can override configuration parameters
type PluginWithConfig interface {
	Plugin
	// GetIgnoreTarget returns whether this plugin ignores target parameter
	GetIgnoreTarget() bool
	// GetMaximumQueue returns the maximum queue size for this plugin (0 means no limit)
	GetMaximumQueue() int
}

// PluginWithQueueControl represents a plugin that handles its own queue management
type PluginWithQueueControl interface {
	Plugin
	// CheckQueueLimit checks if the plugin can accept a new execution
	// Returns (canExecute bool, customMessage string)
	// If canExecute is false, customMessage will be sent to the client as error
	CheckQueueLimit() (bool, string)
}
