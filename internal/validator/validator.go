package validator

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	
	"YALS_SSH/internal/config"
)

type CommandDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type InputType int

const (
	InvalidInput InputType = iota
	IPAddress
	Domain
)

func ValidateInput(input string) InputType {
	input = strings.TrimSpace(input)

	if input == "" {
		return InvalidInput
	}

	if net.ParseIP(input) != nil {
		return IPAddress
	}

	if isValidDomain(input) {
		return Domain
	}

	return InvalidInput
}

func isValidDomain(domain string) bool {
	pattern := `^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`
	matched, err := regexp.MatchString(pattern, domain)
	return err == nil && matched
}

var defaultCommandTemplates = map[string]string{
	"ping":      "ping -c 4",
	"mtr":       "mtr -r -c 4",
	"nexttrace": "nexttrace --nocolor --map --ipv4",
}

func SanitizeCommand(command, target string, allowedCommands []string) (string, bool) {
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

func BuildCommand(command, target string) string {
	cfg := config.GetConfig()
	
	cmdTemplate := ""
	if cfg.Commands != nil {
		if cmdConfig, exists := cfg.Commands[command]; exists {
			cmdTemplate = cmdConfig.Template
		}
	}

	if cmdTemplate == "" {
		cmdTemplate = defaultCommandTemplates[command]
	}

	return fmt.Sprintf("%s %s", cmdTemplate, target)
}

func GetAvailableCommands() []CommandDetail {
	cfg := config.GetConfig()
	var commands []CommandDetail
	
	orderedCommands := []string{"ping", "mtr", "nexttrace"}
	
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
