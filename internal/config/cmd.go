package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentConfig represents the agent configuration
type AgentConfig struct {
	Server struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
	} `yaml:"server"`

	Agent struct {
		Name    string       `yaml:"name"`
		Group   string       `yaml:"group"`
		Details AgentDetails `yaml:"details"`
	} `yaml:"agent"`

	Log struct {
		LogLevel string `yaml:"log_level"`
	} `yaml:"log"`

	Commands        map[string]CommandTemplate `yaml:"commands"`
	orderedCommands []string
}

// CommandTemplate represents a command template configuration
type CommandTemplate struct {
	Template     string `yaml:"template"`
	UsePlugin    string `yaml:"use_plugin"`
	Description  string `yaml:"description"`
	IgnoreTarget bool   `yaml:"ignore_target"`
	MaximumQueue int    `yaml:"maxmium_queue"`
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

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err == nil {
		config.orderedCommands = extractCommandOrder(&node)
	}

	if len(config.orderedCommands) == 0 {
		config.orderedCommands = extractCommandOrderFromText(string(data))
	}

	if len(config.orderedCommands) == 0 {
		config.orderedCommands = make([]string, 0, len(config.Commands))
		for cmdName := range config.Commands {
			config.orderedCommands = append(config.orderedCommands, cmdName)
		}
		slices.Sort(config.orderedCommands)
	}

	if config.Log.LogLevel == "" {
		config.Log.LogLevel = "info"
	}

	return &config, nil
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
					excludedFields := []string{"template", "description", "ignore_target", "maxmium_queue"}

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
