package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentBootstrapConfig represents the minimal startup configuration for an agent.
type AgentBootstrapConfig struct {
	Server struct {
		Host  string `yaml:"host"`
		Port  int    `yaml:"port"`
		Token string `yaml:"token"`
	} `yaml:"server"`

	Agent struct {
		UUID string `yaml:"uuid"`
	} `yaml:"agent"`

	Log struct {
		LogLevel string `yaml:"log_level"`
	} `yaml:"log"`
}

// AgentConfig represents the runtime agent configuration delivered by the server.
type AgentConfig struct {
	Server struct {
		Host  string `yaml:"host" json:"host"`
		Port  int    `yaml:"port" json:"port"`
		UUID  string `yaml:"uuid" json:"uuid"`
		Token string `yaml:"token,omitempty" json:"token,omitempty"`
	} `yaml:"server" json:"server"`

	Agent struct {
		Name    string       `yaml:"name" json:"name"`
		Group   string       `yaml:"group" json:"group"`
		Details AgentDetails `yaml:"details" json:"details"`
	} `yaml:"agent" json:"agent"`

	Log struct {
		LogLevel string `yaml:"log_level" json:"log_level"`
	} `yaml:"log" json:"log"`

	Commands        map[string]CommandTemplate `yaml:"commands" json:"commands"`
	OrderedCommands []string                   `yaml:"ordered_commands,omitempty" json:"ordered_commands,omitempty"`
	orderedCommands []string
}

// CommandTemplate represents a command template configuration
type CommandTemplate struct {
	Template     string `yaml:"template" json:"template"`
	UsePlugin    string `yaml:"use_plugin" json:"use_plugin"`
	IgnoreTarget bool   `yaml:"ignore_target" json:"ignore_target"`
	MaximumQueue int    `yaml:"maxmium_queue" json:"maxmium_queue"`
}

// LoadAgentBootstrapConfig loads the minimal local bootstrap configuration.
func LoadAgentBootstrapConfig(filename string) (*AgentBootstrapConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading agent bootstrap config file: %w", err)
	}

	var config AgentBootstrapConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing agent bootstrap config file: %w", err)
	}

	if config.Log.LogLevel == "" {
		config.Log.LogLevel = "info"
	}

	return &config, nil
}

// LoadAgentConfig loads a full agent configuration from YAML.
func LoadAgentConfig(filename string) (*AgentConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading agent config file: %w", err)
	}

	var config AgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing agent config file: %w", err)
	}

	return NormalizeAgentConfig(&config, data), nil
}

// NormalizeAgentConfig normalizes ordering and defaults for a runtime agent config.
func NormalizeAgentConfig(config *AgentConfig, rawYAML []byte) *AgentConfig {
	if config == nil {
		return nil
	}

	if len(config.OrderedCommands) > 0 {
		config.orderedCommands = append([]string(nil), config.OrderedCommands...)
	}

	if len(config.orderedCommands) == 0 && len(rawYAML) > 0 {
		var node yaml.Node
		if err := yaml.Unmarshal(rawYAML, &node); err == nil {
			config.orderedCommands = extractCommandOrder(&node)
		}
	}

	if len(config.orderedCommands) == 0 && len(rawYAML) > 0 {
		config.orderedCommands = extractCommandOrderFromText(string(rawYAML))
	}

	if len(config.orderedCommands) == 0 {
		config.orderedCommands = make([]string, 0, len(config.Commands))
		for cmdName := range config.Commands {
			config.orderedCommands = append(config.orderedCommands, cmdName)
		}
		slices.Sort(config.orderedCommands)
	}

	config.OrderedCommands = append([]string(nil), config.orderedCommands...)

	if config.Log.LogLevel == "" {
		config.Log.LogLevel = "info"
	}

	if config.Commands == nil {
		config.Commands = map[string]CommandTemplate{}
	}

	return config
}

// extractCommandOrder extracts command order from YAML node structure
func extractCommandOrder(node *yaml.Node) []string {
	var commands []string

	for i := 0; i < len(node.Content); i++ {
		if node.Content[i].Kind == yaml.DocumentNode && len(node.Content[i].Content) > 0 {
			mappingNode := node.Content[i].Content[0]
			if mappingNode.Kind == yaml.MappingNode {
				for j := 0; j < len(mappingNode.Content); j += 2 {
					if j+1 < len(mappingNode.Content) &&
						mappingNode.Content[j].Value == "commands" &&
						mappingNode.Content[j+1].Kind == yaml.MappingNode {

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

// extractCommandOrderFromText extracts command order from text parsing
func extractCommandOrderFromText(data string) []string {
	var commands []string
	lines := strings.Split(data, "\n")
	inCommandsSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "commands:" {
			inCommandsSection = true
			continue
		}

		if inCommandsSection {
			if len(trimmed) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				break
			}

			if strings.Contains(trimmed, ":") && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
				parts := strings.SplitN(trimmed, ":", 2)
				if len(parts) > 0 {
					cmdName := strings.TrimSpace(parts[0])
					excludedFields := []string{"template", "ignore_target", "maxmium_queue", "use_plugin"}

					if cmdName != "" && !slices.Contains(excludedFields, cmdName) {
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
