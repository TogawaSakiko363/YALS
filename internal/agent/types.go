package agent

import (
	"os/exec"
	"sync"

	"YALS/internal/config"
	"YALS/internal/plugin"
	"YALS/internal/proto"
)

// Shell operators that require bash execution
var shellOperators = []string{"|", "&&", "||", ">", "<", ";"}

// targetPlaceholder, when present in a command template, marks where the target
// should be substituted. Templates without it keep the legacy behavior of having
// the target appended at the end.
const targetPlaceholder = "{target}"

// ActiveCommand represents an active command with its details
type ActiveCommand struct {
	Cmd         *exec.Cmd
	FullCommand string
	CommandName string
}

// Client represents an agent client that connects to the server
type Client struct {
	config         *config.AgentConfig
	activeCommands map[string]*ActiveCommand
	commandsLock   sync.RWMutex

	// sendMu serializes writes to the gRPC stream: command output, metrics and
	// probe reports are produced by separate goroutines, but a gRPC stream is not
	// safe for concurrent Send.
	sendMu sync.Mutex

	// probe configuration pushed by the server (hot-reloadable).
	probeMu       sync.Mutex
	probeCfg      proto.ProbeConfig
	probeReconfig chan struct{}
}

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name"`
	Target      string `json:"target"`
	CommandID   string `json:"command_id"`
	IPVersion   string `json:"ip_version,omitempty"`
}

// CommandResponse represents a command response to the server
type CommandResponse struct {
	Type       string `json:"type"`
	CommandID  string `json:"command_id"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	IsComplete bool   `json:"is_complete"`
	IsError    bool   `json:"is_error"`
}

// NewClient creates a new agent client with minimal bootstrap settings.
func NewClient(host string, port int, uuid string, token string) *Client {
	agentConfig := &config.AgentConfig{}
	agentConfig.Server.Host = host
	agentConfig.Server.Port = port
	agentConfig.Server.UUID = uuid
	agentConfig.Server.Token = token
	return NewClientWithConfig(agentConfig)
}

// NewClientWithConfig creates a new agent client with configuration. The agent
// verifies the server by pinning the built-in certificate; there is nothing to
// configure for TLS trust.
func NewClientWithConfig(agentConfig *config.AgentConfig) *Client {
	plugin.GetManager().SetConfig(agentConfig)

	return &Client{
		config:         agentConfig,
		activeCommands: make(map[string]*ActiveCommand),
		probeReconfig:  make(chan struct{}, 1),
	}
}
