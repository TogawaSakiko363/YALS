package config

import (
	"fmt"
	"os"

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
		LogLevel string `yaml:"log_level"`
	} `yaml:"server"`

	WebSocket struct {
		PingInterval int `yaml:"ping_interval"`
		PongWait     int `yaml:"pong_wait"`
	} `yaml:"websocket"`

	Agents []Agent `yaml:"agents"`

	Connection struct {
		Timeout       int `yaml:"timeout"`
		Keepalive     int `yaml:"keepalive"`
		RetryInterval int `yaml:"retry_interval"`
		MaxRetries    int `yaml:"max_retries"`
	} `yaml:"connection"`

	Commands map[string]CommandConfig `yaml:"commands"`
	Groups   []Group   `yaml:"group"`
}

// CommandConfig represents a command configuration
type CommandConfig struct {
	Template    string `yaml:"template"`
	Description string `yaml:"description"`
}

// AgentDetails represents additional agent information
type AgentDetails struct {
	Location   string `yaml:"location"`
	Datacenter string `yaml:"datacenter"`
	TestIP     string `yaml:"test_ip"`
	Description string `yaml:"description"`
}

// Agent represents an SSH agent configuration
type Agent struct {
	Name     string      `yaml:"name"`
	Host     string      `yaml:"host"`
	Port     int         `yaml:"port"`
	Username string      `yaml:"username"`
	Password string      `yaml:"password"`
	KeyFile  string      `yaml:"key_file"`
	Commands []string    `yaml:"commands"`
	Details  AgentDetails `yaml:"details"`
}

// Group represents a group configuration for agents
type Group struct {
	Name   string   `yaml:"name"`
	Agents []string `yaml:"agents"`
}

// AgentGroup represents an agent with its group information
type AgentGroup struct {
	Agent Agent  `json:"agent"`
	Group string `json:"group"`
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