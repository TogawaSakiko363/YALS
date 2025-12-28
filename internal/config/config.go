package config

import (
	"YALS/internal/logger"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the server configuration
type Config struct {
	Server struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		Password    string `yaml:"password"`
		LogLevel    string `yaml:"log_level"`
		TLS         bool   `yaml:"tls"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
	} `yaml:"server"`

	WebSocket struct {
		PingInterval int `yaml:"ping_interval"`
		PongWait     int `yaml:"pong_wait"`
	} `yaml:"websocket"`

	Connection struct {
		KeepAlive int `yaml:"keepalive"`
	} `yaml:"connection"`

	RateLimit struct {
		Enabled     bool `yaml:"enabled"`
		MaxCommands int  `yaml:"max_commands"`
		TimeWindow  int  `yaml:"time_window"`
	} `yaml:"rate_limit"`
}

// AgentDetails represents additional agent information
type AgentDetails struct {
	Location    string `yaml:"location"`
	Datacenter  string `yaml:"datacenter"`
	TestIP      string `yaml:"test_ip"`
	Description string `yaml:"description"`
}

// LoadConfig loads configuration from the specified file
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
	}

	// Validate and set default values
	if config.Connection.KeepAlive < 0 {
		logger.Warnf("keepalive cannot be negative, setting to 0 (disabled)")
		config.Connection.KeepAlive = 0
	}

	// Store the config for later retrieval
	globalConfig = &config

	return &config, nil
}

// Global configuration instance
var globalConfig *Config

// GetConfig returns the current configuration
func GetConfig() *Config {
	return globalConfig
}

// AgentConfig represents the agent configuration
type AgentConfig struct {
	Server struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
		TLS      bool   `yaml:"tls"`
	} `yaml:"server"`

	Agent struct {
		Name    string       `yaml:"name"`
		Group   string       `yaml:"group"`
		Details AgentDetails `yaml:"details"`
	} `yaml:"agent"`

	Log struct {
		LogLevel string `yaml:"log_level"`
	} `yaml:"log"`

	Commands map[string]CommandTemplate `yaml:"commands"`
	// Internal ordered command list
	orderedCommands []string
}

// CommandTemplate represents a command template configuration
type CommandTemplate struct {
	Template     string `yaml:"template"`
	Description  string `yaml:"description"`
	IgnoreTarget bool   `yaml:"ignore_target"` // Whether target parameter is ignored
	MaximumQueue int    `yaml:"maxmium_queue"` // Maximum concurrent executions (0 = no limit)
}

// LoadAgentConfig loads agent configuration from the specified file
func LoadAgentConfig(filename string) (*AgentConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading agent config file: %w", err)
	}

	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing agent config file: %w", err)
	}

	// Parse YAML to get original command order using yaml.Node for order preservation
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err == nil {
		config.orderedCommands = extractCommandOrder(&node)
	}

	// If no order parsed from YAML structure, fallback to parsing text
	if len(config.orderedCommands) == 0 {
		config.orderedCommands = extractCommandOrderFromText(string(data))
	}

	// Final fallback: use alphabetical order for consistency
	if len(config.orderedCommands) == 0 {
		config.orderedCommands = make([]string, 0, len(config.Commands))
		for cmdName := range config.Commands {
			config.orderedCommands = append(config.orderedCommands, cmdName)
		}
		// Sort alphabetically for consistent order
		slices.Sort(config.orderedCommands)
	}

	// Set default log level if not specified
	if config.Log.LogLevel == "" {
		config.Log.LogLevel = "info"
	}

	return &config, nil
}

// extractCommandOrder extracts command order from YAML node structure
func extractCommandOrder(node *yaml.Node) []string {
	var commands []string

	// Find the commands node
	for i := 0; i < len(node.Content); i++ {
		if node.Content[i].Kind == yaml.DocumentNode && len(node.Content[i].Content) > 0 {
			// Look for the mapping node
			mappingNode := node.Content[i].Content[0]
			if mappingNode.Kind == yaml.MappingNode {
				// Find "commands" key
				for j := 0; j < len(mappingNode.Content); j += 2 {
					if j+1 < len(mappingNode.Content) &&
						mappingNode.Content[j].Value == "commands" &&
						mappingNode.Content[j+1].Kind == yaml.MappingNode {

						// Extract command names in order
						commandsNode := mappingNode.Content[j+1]
						for k := 0; k < len(commandsNode.Content); k += 2 {
							if k < len(commandsNode.Content) {
								cmdName := commandsNode.Content[k].Value
								if cmdName != "" {
									commands = append(commands, cmdName)
								}
							}
						}
						return commands
					}
				}
			}
		}
	}

	return commands
}

// extractCommandOrderFromText extracts command order from text parsing (fallback method)
func extractCommandOrderFromText(data string) []string {
	var commands []string
	lines := strings.Split(data, "\n")
	inCommandsSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "commands:" {
			inCommandsSection = true
			continue
		}

		if inCommandsSection {
			// If encountering new top-level config item (no indentation), exit commands section
			if len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}

			// Check if it's a command definition line (has indentation and contains colon)
			if strings.Contains(trimmed, ":") && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) > 0 {
					cmdName := strings.TrimSpace(parts[0])
					// Skip command properties, only capture command names
					excludedFields := []string{"template", "description", "ignore_target", "maxmium_queue"}

					if cmdName != "" && !slices.Contains(excludedFields, cmdName) {
						// Check if this command is already in the list
						if !slices.Contains(commands, cmdName) {
							commands = append(commands, cmdName)
						}
					}
				}
			}
		}
	}

	return commands
}

// GetAvailableCommands returns the list of available commands in the order they appear in the config file
func (c *AgentConfig) GetAvailableCommands() []CommandInfo {
	commands := make([]CommandInfo, 0, len(c.orderedCommands))

	// Return commands in config file order
	for _, name := range c.orderedCommands {
		if template, exists := c.Commands[name]; exists {
			commands = append(commands, CommandInfo{
				Name:         name,
				Template:     template.Template,
				Description:  template.Description,
				IgnoreTarget: template.IgnoreTarget,
				MaximumQueue: template.MaximumQueue,
			})
		}
	}

	return commands
}

// CommandInfo represents command information
type CommandInfo struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	Description  string `json:"description"`
	IgnoreTarget bool   `json:"ignore_target"` // Whether target parameter is ignored
	MaximumQueue int    `json:"maxmium_queue"` // Maximum concurrent executions (0 = no limit)
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
