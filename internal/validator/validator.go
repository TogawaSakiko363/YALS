package validator

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"YALS_SSH/internal/config"
)

// CommandDetail represents a command with its description
type CommandDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// InputType represents the type of input
type InputType int

const (
	// InvalidInput represents an invalid input
	InvalidInput InputType = iota
	// IPAddress represents an IP address
	IPAddress
	// Domain represents a domain name
	Domain
)

// ValidateInput validates the input and returns its type
func ValidateInput(input string) InputType {
	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check if input is empty
	if input == "" {
		return InvalidInput
	}

	// Check if input is an IP address
	if net.ParseIP(input) != nil {
		return IPAddress
	}

	// Check if input is a valid domain name
	if isValidDomain(input) {
		return Domain
	}

	return InvalidInput
}

// isValidDomain checks if the input is a valid domain name
func isValidDomain(domain string) bool {
	// Domain name validation regex
	// This is a simplified version, real domain validation is more complex
	pattern := `^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, domain)
	return err == nil && matched
}

// CommandConfig defines the configuration for a command
var defaultCommandTemplates = map[string]string{
	"ping":      "ping -c 4",
	"mtr":       "mtr -r -c 4",
	"nexttrace": "nexttrace --nocolor --map --ipv4",
}

// SanitizeCommand ensures the command is safe to execute
func SanitizeCommand(command, target string, allowedCommands []string) (string, bool) {
	// Check if command is allowed
	commandAllowed := false
	for _, cmd := range allowedCommands {
		if cmd == command {
			commandAllowed = true
			break
		}
	}

	if !commandAllowed {
		return "", false
	}

	return BuildCommand(command, target), true
}

// BuildCommand constructs a command using configuration templates
func BuildCommand(command, target string) string {
	cfg := config.GetConfig()

	// Get command template from config
	cmdTemplate := ""
	if cfg.Commands != nil {
		if cmdConfig, exists := cfg.Commands[command]; exists {
			cmdTemplate = cmdConfig.Template
		}
	}

	// Use default template if not configured
	if cmdTemplate == "" {
		cmdTemplate = defaultCommandTemplates[command]
	}

	return fmt.Sprintf("%s %s", cmdTemplate, target)
}

// GetAvailableCommands returns commands in the exact order defined in the config file
// Returns a slice of command details
func GetAvailableCommands() []CommandDetail {
	cfg := config.GetConfig()
	var commands []CommandDetail

	// Define the exact order of commands based on config file
	orderedCommands := []string{"ping", "mtr", "nexttrace"}

	// Add commands from config in the exact defined order
	if cfg.Commands != nil {
		for _, cmdName := range orderedCommands {
			if cmdConfig, exists := cfg.Commands[cmdName]; exists {
				description := cmdConfig.Description
				if description == "" {
					description = fmt.Sprintf("Execute %s command", cmdName)
				}
				commands = append(commands, CommandDetail{
					Name:        cmdName,
					Description: description,
				})
			}
		}

		// Add any additional commands that might be in config but not in orderedCommands
		for cmdName, cmdConfig := range cfg.Commands {
			found := false
			for _, cmd := range commands {
				if cmd.Name == cmdName {
					found = true
					break
				}
			}
			if !found {
				description := cmdConfig.Description
				if description == "" {
					description = fmt.Sprintf("Execute %s command", cmdName)
				}
				commands = append(commands, CommandDetail{
					Name:        cmdName,
					Description: description,
				})
			}
		}
	}

	// Add default commands that might not be in config
	for _, cmdName := range orderedCommands {
		found := false
		for _, cmd := range commands {
			if cmd.Name == cmdName {
				found = true
				break
			}
		}
		if !found {
			commands = append(commands, CommandDetail{
				Name:        cmdName,
				Description: fmt.Sprintf("Execute %s command", cmdName),
			})
		}
	}

	return commands
}

// GetAgentCommands returns commands available for a specific agent
// Returns a slice of command details based on the agent's supported commands
func GetAgentCommands(agentCommands []string) []CommandDetail {
	cfg := config.GetConfig()
	var commands []CommandDetail

	// Define the exact order of commands based on config file
	orderedCommands := []string{"ping", "mtr", "nexttrace"}

	// Add commands from agent's supported commands in the exact defined order
	for _, cmdName := range orderedCommands {
		// Check if this command is supported by the agent
		supported := false
		for _, agentCmd := range agentCommands {
			if agentCmd == cmdName {
				supported = true
				break
			}
		}

		if supported {
			description := fmt.Sprintf("Execute %s command", cmdName)
			if cfg.Commands != nil {
				if cmdConfig, exists := cfg.Commands[cmdName]; exists {
					if cmdConfig.Description != "" {
						description = cmdConfig.Description
					}
				}
			}
			commands = append(commands, CommandDetail{
				Name:        cmdName,
				Description: description,
			})
		}
	}

	// Add any additional commands that might be supported by agent but not in orderedCommands
	for _, cmdName := range agentCommands {
		found := false
		for _, cmd := range commands {
			if cmd.Name == cmdName {
				found = true
				break
			}
		}
		if !found {
			description := fmt.Sprintf("Execute %s command", cmdName)
			if cfg.Commands != nil {
				if cmdConfig, exists := cfg.Commands[cmdName]; exists {
					if cmdConfig.Description != "" {
						description = cmdConfig.Description
					}
				}
			}
			commands = append(commands, CommandDetail{
				Name:        cmdName,
				Description: description,
			})
		}
	}

	return commands
}
