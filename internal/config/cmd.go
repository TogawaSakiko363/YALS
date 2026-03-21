package config

// CommandInfo represents command information
type CommandInfo struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	UsePlugin    string `json:"use_plugin"`
	Description  string `json:"description"`
	IgnoreTarget bool   `json:"ignore_target"`
	MaximumQueue int    `json:"maxmium_queue"`
}

// GetAvailableCommands returns the list of available commands in the order they appear in the config file
func (c *AgentConfig) GetAvailableCommands() []CommandInfo {
	commands := make([]CommandInfo, 0, len(c.orderedCommands))

	for _, name := range c.orderedCommands {
		if template, exists := c.Commands[name]; exists {
			commands = append(commands, CommandInfo{
				Name:         name,
				Template:     template.Template,
				UsePlugin:    template.UsePlugin,
				Description:  template.Description,
				IgnoreTarget: template.IgnoreTarget,
				MaximumQueue: template.MaximumQueue,
			})
		}
	}

	return commands
}

// IsCommandAllowed checks if a command is allowed
func (c *AgentConfig) IsCommandAllowed(commandName string) bool {
	_, exists := c.Commands[commandName]
	return exists
}

// GetCommandTemplate returns the template for a command
func (c *AgentConfig) GetCommandTemplate(commandName string) (string, bool) {
	if template, exists := c.Commands[commandName]; exists {
		return template.Template, true
	}
	return "", false
}

// GetCommandConfig returns the full configuration for a command
func (c *AgentConfig) GetCommandConfig(commandName string) (CommandTemplate, bool) {
	if template, exists := c.Commands[commandName]; exists {
		return template, true
	}
	return CommandTemplate{}, false
}
