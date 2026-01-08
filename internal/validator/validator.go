package validator

import (
	"YALS/internal/dns"
	"context"
	"net"
	"regexp"
	"strings"
	"time"
)

// IPVersion type alias for DNS IP version
type IPVersion = dns.IPVersion

const (
	IPVersionAuto = dns.IPVersionAuto
	IPVersionIPv4 = dns.IPVersionIPv4
	IPVersionIPv6 = dns.IPVersionIPv6
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

	// Extract host and port
	host, port := extractHostPort(input)
	if host == "" {
		return InvalidInput
	}

	// Validate port if present
	if port != "" {
		matched, err := regexp.MatchString(`^\d+$`, port)
		if err != nil || !matched {
			return InvalidInput
		}
	}

	// Check if host is an IP address (IPv4 or IPv6)
	if net.ParseIP(host) != nil {
		return IPAddress
	}

	// Check if host is a valid domain name
	if isValidDomain(host) {
		return Domain
	}

	return InvalidInput
}

// ResolveDomain resolves a domain name to IP addresses using the DNS resolver
func ResolveDomain(domain string) ([]net.IP, error) {
	return ResolveDomainWithVersion(domain, dns.IPVersionAuto)
}

// ResolveDomainWithVersion resolves a domain name with specific IP version preference
func ResolveDomainWithVersion(domain string, version dns.IPVersion) ([]net.IP, error) {
	resolver := dns.GetResolver()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return resolver.ResolveWithVersion(ctx, domain, version)
}

// extractHostPort extracts host and port from input
// Supports:
// - IPv4: 192.168.1.1 or 192.168.1.1:8080
// - IPv6: 2001:db8::1 or [2001:db8::1]:8080
// - Domain: example.com or example.com:8080
func extractHostPort(input string) (host, port string) {
	// Check for IPv6 with port: [2001:db8::1]:8080
	if strings.HasPrefix(input, "[") {
		closeBracket := strings.Index(input, "]")
		if closeBracket == -1 {
			return "", ""
		}
		host = input[1:closeBracket]

		// Check if there's a port after the bracket
		if len(input) > closeBracket+1 {
			if input[closeBracket+1] == ':' {
				port = input[closeBracket+2:]
			} else {
				return "", "" // Invalid format
			}
		}
		return host, port
	}

	// Check for IPv6 without port or IPv4/domain with port
	lastColon := strings.LastIndex(input, ":")
	if lastColon == -1 {
		// No port
		return input, ""
	}

	// Try to parse as IPv6 without port
	if net.ParseIP(input) != nil {
		return input, ""
	}

	// Split by last colon for IPv4/domain with port
	host = input[:lastColon]
	port = input[lastColon+1:]

	// Verify it's not an IPv6 address being incorrectly split
	if strings.Contains(host, ":") {
		// Multiple colons suggest IPv6 without brackets
		if net.ParseIP(input) != nil {
			return input, ""
		}
		return "", "" // Invalid format
	}

	return host, port
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
