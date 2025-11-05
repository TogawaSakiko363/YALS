package plugin

import (
	"YALS/internal/config"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name"`
	Target      string `json:"target"`
	CommandID   string `json:"command_id"`
}

// Manager manages all registered plugins
type Manager struct {
	plugins        map[string]Plugin
	activeCommands map[string]*exec.Cmd // Track active plugin commands
	commandsLock   sync.RWMutex         // Protect activeCommands map
	config         *config.AgentConfig  // Agent configuration for plugin descriptions
}

// NewManager creates a new plugin manager
func NewManager() *Manager {
	return &Manager{
		plugins:        make(map[string]Plugin),
		activeCommands: make(map[string]*exec.Cmd),
	}
}

// SetConfig sets the agent configuration for the manager
func (m *Manager) SetConfig(cfg *config.AgentConfig) {
	m.config = cfg
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

// ExecutePlugin executes a plugin by name with the given target
func (m *Manager) ExecutePlugin(name, target string) (string, error) {
	plugin, exists := m.GetPlugin(name)
	if !exists {
		return "", fmt.Errorf("plugin '%s' not found", name)
	}

	return plugin.Execute(target)
}

// ExecutePluginStreaming executes a plugin by name with streaming output
func (m *Manager) ExecutePluginStreaming(name, target string, callback StreamingCallback) error {
	plugin, exists := m.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	return plugin.ExecuteStreaming(target, callback)
}

// ListPlugins returns a list of all registered plugin names
func (m *Manager) ListPlugins() []string {
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// IsPluginAvailable checks if a plugin is available
func (m *Manager) IsPluginAvailable(name string) bool {
	_, exists := m.plugins[name]
	return exists
}

// GetPluginDescription returns the description for a plugin from configuration
func (m *Manager) GetPluginDescription(pluginName string) string {
	if m.config == nil {
		// Fallback to plugin's built-in description
		if plugin, exists := m.plugins[pluginName]; exists {
			return plugin.GetDescription()
		}
		return ""
	}

	// Look for commands that use this plugin
	for cmdName, cmdTemplate := range m.config.Commands {
		if cmdTemplate.UsePlugin == pluginName {
			if cmdTemplate.Description != "" {
				return cmdTemplate.Description
			}
		}
		_ = cmdName // Avoid unused variable warning
	}

	// Fallback to plugin's built-in description
	if plugin, exists := m.plugins[pluginName]; exists {
		return plugin.GetDescription()
	}

	return ""
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
		// Register all available plugins
		registerPlugins()
	}
	return globalManager
}

// GetPluginDescription returns the description for a plugin (convenience function)
func GetPluginDescription(pluginName string) string {
	return GetManager().GetPluginDescription(pluginName)
}

// registerPlugins registers all available plugins based on build context
func registerPlugins() {
	// Register agent plugins
	for name, constructor := range agentPluginRegistry {
		pluginInstance := constructor()
		globalManager.Register(pluginInstance)
		_ = name // Use name if needed for logging
	}

	// Register server plugins
	for name, constructor := range serverPluginRegistry {
		pluginInstance := constructor()
		globalManager.Register(pluginInstance)
		_ = name // Use name if needed for logging
	}
}

// IsCommandAvailable checks if a command is available on the system
func IsCommandAvailable(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// SanitizeTarget sanitizes the target parameter to prevent command injection
func SanitizeTarget(target string) string {
	// Remove potentially dangerous characters
	target = strings.ReplaceAll(target, ";", "")
	target = strings.ReplaceAll(target, "&", "")
	target = strings.ReplaceAll(target, "|", "")
	target = strings.ReplaceAll(target, "`", "")
	target = strings.ReplaceAll(target, "$", "")
	target = strings.ReplaceAll(target, "(", "")
	target = strings.ReplaceAll(target, ")", "")
	target = strings.ReplaceAll(target, "<", "")
	target = strings.ReplaceAll(target, ">", "")
	target = strings.TrimSpace(target)
	return target
}

// ExecutePluginCommand executes a plugin command with WebSocket connection
func ExecutePluginCommand(pluginName, target, commandID string, callback StreamingCallback) error {
	manager := GetManager()
	return manager.ExecutePluginStreamingWithID(pluginName, target, commandID, callback)
}

// ExecutePluginStreamingWithID executes a plugin with command ID for stop functionality
func (m *Manager) ExecutePluginStreamingWithID(name, target, commandID string, callback StreamingCallback) error {
	plugin, exists := m.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	// Check if plugin supports ExecuteStreamingWithID interface
	if pluginWithID, ok := plugin.(PluginWithID); ok {
		return pluginWithID.ExecuteStreamingWithID(target, commandID, callback)
	}

	// Fallback to regular streaming for other plugins
	return plugin.ExecuteStreaming(target, callback)
}

// StopPluginCommand stops a running plugin command
func StopPluginCommand(commandID string) bool {
	manager := GetManager()

	manager.commandsLock.Lock()
	defer manager.commandsLock.Unlock()

	if cmd, exists := manager.activeCommands[commandID]; exists {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
		delete(manager.activeCommands, commandID)
		return true
	}

	return false
}

// RegisterActiveCommand registers an active command for stop functionality
func (m *Manager) RegisterActiveCommand(commandID string, cmd *exec.Cmd) {
	m.commandsLock.Lock()
	defer m.commandsLock.Unlock()
	m.activeCommands[commandID] = cmd
}

// UnregisterActiveCommand removes an active command
func (m *Manager) UnregisterActiveCommand(commandID string) {
	m.commandsLock.Lock()
	defer m.commandsLock.Unlock()
	delete(m.activeCommands, commandID)
}
