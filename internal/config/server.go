package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the file-based server bootstrap configuration.
type Config struct {
	Server struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		Password    string `yaml:"password"`
		LogLevel    string `yaml:"log_level"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
		// TrustProxyHeaders controls whether X-Real-IP / X-Forwarded-For headers
		// are honored when determining the client IP. Only enable this when the
		// server sits behind a trusted reverse proxy that sets these headers;
		// otherwise clients can spoof them to forge logs or bypass rate limits.
		TrustProxyHeaders bool `yaml:"trust_proxy_headers"`
		// MetricsEnabled exposes the Prometheus /metrics endpoint when true.
		// It is off by default so an upgrade never silently publishes the agent
		// inventory.
		MetricsEnabled bool `yaml:"metrics_enabled"`
		// MetricsToken, when non-empty, requires scrapers to present
		// "Authorization: Bearer <token>" to read /metrics. When empty the
		// endpoint is unauthenticated and should be restricted at the network layer.
		MetricsToken string `yaml:"metrics_token"`
	} `yaml:"server"`

	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
}

// RuntimeSettings represents hot-reloadable server runtime options.
type RuntimeSettings struct {
	GRPC struct {
		PingInterval int `json:"ping_interval"`
		PongWait     int `json:"pong_wait"`
	} `json:"grpc"`

	RateLimit struct {
		Enabled     bool `json:"enabled"`
		MaxCommands int  `json:"max_commands"`
		TimeWindow  int  `json:"time_window"`
	} `json:"rate_limit"`
}

// AgentDetails represents additional agent information.
type AgentDetails struct {
	Location    string `yaml:"location" json:"location"`
	Datacenter  string `yaml:"datacenter" json:"datacenter"`
	TestIP      string `yaml:"test_ip" json:"test_ip"`
	Description string `yaml:"description" json:"description"`
}

// LoadConfig loads configuration from the specified file.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error parsing config file: %w", err)
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

// DefaultRuntimeSettings returns normalized built-in runtime defaults.
func (c *Config) DefaultRuntimeSettings() RuntimeSettings {
	settings := RuntimeSettings{}
	NormalizeRuntimeSettings(&settings)
	return settings
}

// NormalizeRuntimeSettings applies safe defaults and constraints.
func NormalizeRuntimeSettings(settings *RuntimeSettings) {
	if settings == nil {
		return
	}
	if settings.GRPC.PingInterval <= 0 {
		settings.GRPC.PingInterval = 30
	}
	if settings.GRPC.PongWait <= 0 {
		settings.GRPC.PongWait = 60
	}
	if settings.RateLimit.MaxCommands <= 0 {
		settings.RateLimit.MaxCommands = 10
	}
	if settings.RateLimit.TimeWindow <= 0 {
		settings.RateLimit.TimeWindow = 60
	}
}

// Global configuration instance.
var globalConfig *Config

// GetConfig returns the current bootstrap configuration.
func GetConfig() *Config {
	return globalConfig
}
