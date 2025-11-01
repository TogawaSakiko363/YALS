package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppConfig represents application-level configuration
type AppConfig struct {
	Version string `yaml:"version"`
}

// Config represents the server configuration
type Config struct {
	App AppConfig `yaml:"app"`

	Server struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"server"`

	WebSocket struct {
		PingInterval int `yaml:"ping_interval"`
		PongWait     int `yaml:"pong_wait"`
	} `yaml:"websocket"`

	Connection struct {
		Timeout             int `yaml:"timeout"`
		Keepalive           int `yaml:"keepalive"`
		RetryInterval       int `yaml:"retry_interval"`
		MaxRetries          int `yaml:"max_retries"`
		DeleteOfflineAgents int `yaml:"delete_offline_agents"`
	} `yaml:"connection"`
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
	if config.Connection.DeleteOfflineAgents < 0 {
		log.Printf("Warning: delete_offline_agents cannot be negative, setting to 0 (disabled)")
		config.Connection.DeleteOfflineAgents = 0
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

	Commands map[string]CommandTemplate `yaml:"commands"`
	// Internal ordered command list
	orderedCommands []string
}

// CommandTemplate represents a command template configuration
type CommandTemplate struct {
	Template    string `yaml:"template"`
	Description string `yaml:"description"`
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

	// Parse YAML to get original command order
	var rawConfig map[string]interface{}
	if err := yaml.Unmarshal(data, &rawConfig); err == nil {
		if commands, ok := rawConfig["commands"].(map[string]interface{}); ok {
			// Extract command order from YAML
			config.orderedCommands = make([]string, 0, len(commands))

			// Since Go maps are unordered, we need to extract order from raw YAML data
			// Use a simple method: order as they appear in config file
			lines := strings.Split(string(data), "\n")
			inCommandsSection := false

			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "commands:" {
					inCommandsSection = true
					continue
				}

				if inCommandsSection {
					// If encountering new top-level config item, exit commands section
					if len(trimmed) > 0 && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") {
						break
					}

					// Check if it's a command definition line
					if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "#") && (strings.HasPrefix(trimmed, " ") || strings.HasPrefix(trimmed, "\t")) {
						parts := strings.SplitN(trimmed, ":", 2)
						if len(parts) > 0 {
							cmdName := strings.TrimSpace(parts[0])
							if cmdName != "" && cmdName != "template" && cmdName != "description" {
								// Check if this command is already in the list
								if !contains(config.orderedCommands, cmdName) {
									config.orderedCommands = append(config.orderedCommands, cmdName)
								}
							}
						}
					}
				}
			}
		}
	}

	// If no order parsed, use default order
	if len(config.orderedCommands) == 0 {
		for cmdName := range config.Commands {
			config.orderedCommands = append(config.orderedCommands, cmdName)
		}
	}

	return &config, nil
}

// contains checks if a slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// GetAvailableCommands returns the list of available commands in the order they appear in the config file
func (c *AgentConfig) GetAvailableCommands() []CommandInfo {
	commands := make([]CommandInfo, 0, len(c.orderedCommands))

	// Return commands in config file order
	for _, name := range c.orderedCommands {
		if template, exists := c.Commands[name]; exists {
			commands = append(commands, CommandInfo{
				Name:        name,
				Template:    template.Template,
				Description: template.Description,
			})
		}
	}

	return commands
}

// CommandInfo represents command information
type CommandInfo struct {
	Name        string `json:"name"`
	Template    string `json:"template"`
	Description string `json:"description"`
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
