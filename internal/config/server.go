package config

import (
	"fmt"
	"os"
	"path/filepath"

	"YALS/internal/logger"

	"gopkg.in/yaml.v3"
)

// Config represents the server configuration
type Config struct {
	Server struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		Password    string `yaml:"password"`
		LogLevel    string `yaml:"log_level"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
	} `yaml:"server"`

	GRPC struct {
		PingInterval int `yaml:"ping_interval"`
		PongWait     int `yaml:"pong_wait"`
	} `yaml:"grpc"`

	Connection struct {
		KeepAlive int `yaml:"keepalive"`
	} `yaml:"connection"`

	RateLimit struct {
		Enabled     bool `yaml:"enabled"`
		MaxCommands int  `yaml:"max_commands"`
		TimeWindow  int  `yaml:"time_window"`
	} `yaml:"rate_limit"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
}

// AgentDetails represents additional agent information
type AgentDetails struct {
	Location    string `yaml:"location" json:"location"`
	Datacenter  string `yaml:"datacenter" json:"datacenter"`
	TestIP      string `yaml:"test_ip" json:"test_ip"`
	Description string `yaml:"description" json:"description"`
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

	if config.Connection.KeepAlive < 0 {
		logger.Warnf("keepalive cannot be negative, setting to 0 (disabled)")
		config.Connection.KeepAlive = 0
	}

	if config.Server.LogLevel == "" {
		config.Server.LogLevel = "info"
	}

	if config.Database.Path == "" {
		config.Database.Path = filepath.Clean("./data/yals.db")
	}

	globalConfig = &config

	return &config, nil
}

// Global configuration instance
var globalConfig *Config

// GetConfig returns the current configuration
func GetConfig() *Config {
	return globalConfig
}
