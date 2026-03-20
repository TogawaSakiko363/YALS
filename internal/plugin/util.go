package plugin

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetPluginDescription returns the description for a plugin from configuration
func (m *Manager) GetPluginDescription(pluginName string) string {
	if m.config == nil {
		if plugin, exists := m.plugins[pluginName]; exists {
			return plugin.GetDescription()
		}
		return ""
	}

	for _, cmdTemplate := range m.config.Commands {
		if cmdTemplate.UsePlugin == pluginName {
			if cmdTemplate.Description != "" {
				return cmdTemplate.Description
			}
		}
	}

	if plugin, exists := m.plugins[pluginName]; exists {
		return plugin.GetDescription()
	}

	return ""
}

// GetPluginDescription returns the description for a plugin (convenience function)
func GetPluginDescription(pluginName string) string {
	return GetManager().GetPluginDescription(pluginName)
}

// IsCommandAvailable checks if a command is available on the system
func IsCommandAvailable(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// SanitizeTarget sanitizes the target parameter to prevent command injection
func SanitizeTarget(target string) string {
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

// GetPluginIgnoreTarget checks if a plugin overrides ignore_target setting
// Returns (hasOverride bool, ignoreTarget bool)
func GetPluginIgnoreTarget(pluginName string) (bool, bool) {
	manager := GetManager()
	plugin, exists := manager.GetPlugin(pluginName)
	if !exists {
		return false, false
	}

	if configPlugin, ok := plugin.(PluginWithConfig); ok {
		return true, configPlugin.GetIgnoreTarget()
	}

	return false, false
}

// GetPluginMaximumQueue checks if a plugin overrides maximum_queue setting
// Returns (hasOverride bool, maximumQueue int)
func GetPluginMaximumQueue(pluginName string) (bool, int) {
	manager := GetManager()
	plugin, exists := manager.GetPlugin(pluginName)
	if !exists {
		return false, 0
	}

	if configPlugin, ok := plugin.(PluginWithConfig); ok {
		return true, configPlugin.GetMaximumQueue()
	}

	return false, 0
}

// CheckPluginQueueLimit checks if a plugin can accept a new execution
// Returns (canExecute bool, customMessage string)
func CheckPluginQueueLimit(pluginName string) (bool, string) {
	manager := GetManager()
	plugin, exists := manager.GetPlugin(pluginName)
	if !exists {
		return true, ""
	}

	if queuePlugin, ok := plugin.(PluginWithQueueControl); ok {
		return queuePlugin.CheckQueueLimit()
	}

	return true, ""
}

// ExecutePluginCommand executes a plugin command with WebSocket connection
func ExecutePluginCommand(pluginName, target, commandID string, callback StreamingCallback) error {
	canExecute, customMessage := CheckPluginQueueLimit(pluginName)
	if !canExecute {
		if callback != nil {
			callback(customMessage, true, true)
		}
		return nil
	}

	manager := GetManager()
	return manager.ExecutePluginStreamingWithID(pluginName, target, commandID, callback)
}

// ExecutePluginStreamingWithID executes a plugin with command ID for stop functionality
func (m *Manager) ExecutePluginStreamingWithID(name, target, commandID string, callback StreamingCallback) error {
	plugin, exists := m.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	if pluginWithID, ok := plugin.(PluginWithID); ok {
		return pluginWithID.ExecuteStreamingWithID(target, commandID, callback)
	}

	return plugin.ExecuteStreaming(target, callback)
}

// StopPluginCommand stops a running plugin command
func StopPluginCommand(commandID string) bool {
	manager := GetManager()

	manager.commandsLock.Lock()
	defer manager.commandsLock.Unlock()

	if cmd, exists := manager.activeCommands[commandID]; exists {
		if cmd != nil {
			if c, ok := cmd.(*exec.Cmd); ok && c.Process != nil {
				c.Process.Kill()
			}
		}
		delete(manager.activeCommands, commandID)
		return true
	}

	return false
}

// RegisterActiveCommand registers an active command for stop functionality
func (m *Manager) RegisterActiveCommand(commandID string, cmd interface{}) {
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
