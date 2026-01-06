package validator

import (
	"net"
	"regexp"
	"strings"
)

// CommandDetail represents a command with its description
type CommandDetail struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	IgnoreTarget bool   `json:"ignore_target"` // Whether target parameter is ignored
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

	// Check if input length exceeds 256 characters
	if len(input) > 256 {
		return InvalidInput
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Check if input is empty
	if input == "" {
		return InvalidInput
	}

	// Check if input contains port (IP:port or domain:port)
	host := input
	if strings.Contains(input, ":") {
		parts := strings.Split(input, ":")
		if len(parts) == 2 {
			host = parts[0]
			// Validate port is numeric
			if _, err := regexp.MatchString(`^\d+$`, parts[1]); err != nil || parts[1] == "" {
				return InvalidInput
			}
		} else {
			return InvalidInput
		}
	}

	// Check if host is an IP address
	if net.ParseIP(host) != nil {
		return IPAddress
	}

	// Check if host is a valid domain name
	if isValidDomain(host) {
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

// SanitizeCommand ensures the command is safe to execute
// This function is now deprecated as command validation is handled by agents
func SanitizeCommand(command, target string, allowedCommands []string) (string, bool) {
	// Check if command is allowed
	if !contains(allowedCommands, command) {
		return "", false
	}

	// Return the command name and target separately for the new architecture
	return command + " " + target, true
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
