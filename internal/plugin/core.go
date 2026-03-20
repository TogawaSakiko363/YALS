package plugin

import (
	"YALS/internal/config"
	"sync"
)

// Manager manages all registered plugins
type Manager struct {
	plugins        map[string]Plugin
	activeCommands map[string]interface{}
	commandsLock   sync.RWMutex
	config         *config.AgentConfig
}

// NewManager creates a new plugin manager
func NewManager() *Manager {
	return &Manager{
		plugins:        make(map[string]Plugin),
		activeCommands: make(map[string]interface{}),
	}
}

// SetConfig sets the agent configuration for the manager
func (m *Manager) SetConfig(cfg *config.AgentConfig) {
	m.config = cfg
}

// GetConfig returns the agent configuration
func (m *Manager) GetConfig() *config.AgentConfig {
	return m.config
}

// Register registers a plugin with the manager
func (m *Manager) Register(plugin Plugin) {
	m.plugins[plugin.GetName()] = plugin
}

// GetPlugin returns a plugin by name
func (m *Manager) GetPlugin(name string) (Plugin, bool) {
	plugin, exists := m.plugins[name]
	return plugin, exists
}

// ListPlugins returns a list of all registered plugin names
func (m *Manager) ListPlugins() []string {
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// GetRegisteredPlugins returns a list of all registered plugin names (deprecated, use ListPlugins)
func GetRegisteredPlugins() []string {
	return GetManager().ListPlugins()
}

// Global plugin manager instance
var globalManager *Manager

// Plugin registry for dynamic registration
var agentPluginRegistry = make(map[string]func() Plugin)
var serverPluginRegistry = make(map[string]func() Plugin)

// RegisterAgentPlugin registers an agent-side plugin constructor
func RegisterAgentPlugin(name string, constructor func() Plugin) {
	agentPluginRegistry[name] = constructor
}

// RegisterServerPlugin registers a server-side plugin constructor
func RegisterServerPlugin(name string, constructor func() Plugin) {
	serverPluginRegistry[name] = constructor
}

// GetManager returns the global plugin manager
func GetManager() *Manager {
	if globalManager == nil {
		globalManager = NewManager()
		registerPlugins()
	}
	return globalManager
}

// registerPlugins registers all available plugins based on build context
func registerPlugins() {
	for _, constructor := range agentPluginRegistry {
		pluginInstance := constructor()
		globalManager.Register(pluginInstance)
	}

	for _, constructor := range serverPluginRegistry {
		pluginInstance := constructor()
		globalManager.Register(pluginInstance)
	}
}
