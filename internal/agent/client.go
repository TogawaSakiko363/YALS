package agent

import (
	"YALS/internal/config"
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents an agent client that connects to the server
type Client struct {
	config         *config.AgentConfig
	activeCommands map[string]*exec.Cmd
	commandsLock   sync.RWMutex
}

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name"` // Command name instead of full command
	Target      string `json:"target"`       // Target parameter (e.g., IP address)
	CommandID   string `json:"command_id"`
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

// NewClient creates a new agent client (deprecated, use NewClientWithConfig)
func NewClient(password string) *Client {
	// This is kept for backward compatibility but should not be used
	agentConfig := &config.AgentConfig{}
	agentConfig.Server.Password = password
	return &Client{
		config:         agentConfig,
		activeCommands: make(map[string]*exec.Cmd),
	}
}

// NewClientWithConfig creates a new agent client with configuration
func NewClientWithConfig(agentConfig *config.AgentConfig) *Client {
	return &Client{
		config:         agentConfig,
		activeCommands: make(map[string]*exec.Cmd),
	}
}

// ConnectToServer connects to the server and handles the WebSocket connection
func (c *Client) ConnectToServer() error {
	// Select protocol based on configuration
	protocol := "ws"
	if c.config.Server.TLS {
		protocol = "wss"
	}

	serverURL := fmt.Sprintf("%s://%s:%d/ws/api/agent", protocol, c.config.Server.Host, c.config.Server.Port)

	// Set up headers for authentication
	headers := http.Header{}
	headers.Set("X-Agent-Password", c.config.Server.Password)

	// Create dialer
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	log.Printf("Connecting to server at %s", serverURL)

	// Connect to server
	conn, _, err := dialer.Dial(serverURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	log.Printf("Connected to server successfully")

	// Set up ping/pong handling
	conn.SetPongHandler(func(appData string) error {
		log.Printf("Received pong from server")
		return nil
	})

	// Send handshake with agent information
	handshake := map[string]interface{}{
		"type":     "handshake",
		"name":     c.config.Agent.Name,
		"group":    c.config.Agent.Group,
		"details":  c.config.Agent.Details,
		"commands": c.config.GetAvailableCommands(),
	}

	if err := conn.WriteJSON(handshake); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	log.Printf("Sent handshake with %d available commands", len(c.config.Commands))

	// Wait for handshake acknowledgment
	var ack map[string]interface{}
	if err := conn.ReadJSON(&ack); err != nil {
		return fmt.Errorf("failed to read handshake ack: %w", err)
	}

	if ackType, ok := ack["type"].(string); !ok || ackType != "handshake_ack" {
		return fmt.Errorf("invalid handshake acknowledgment")
	}

	log.Printf("Handshake completed successfully")

	// Handle incoming messages
	for {
		var req CommandRequest
		if err := conn.ReadJSON(&req); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		switch req.Type {
		case "execute_command":
			go c.executeCommand(conn, req)
		case "stop_command":
			c.stopCommand(req.CommandID)
		default:
			log.Printf("Unknown message type: %s", req.Type)
		}
	}

	log.Printf("Disconnected from server")
	return nil
}

// executeCommand executes a command and streams the output
func (c *Client) executeCommand(conn *websocket.Conn, req CommandRequest) {
	// Security check: Verify command is allowed
	if !c.config.IsCommandAllowed(req.CommandName) {
		c.sendError(conn, req.CommandID, fmt.Sprintf("Command '%s' is not allowed", req.CommandName))
		log.Printf("SECURITY: Blocked unauthorized command '%s' from server", req.CommandName)
		return
	}

	// Get command template
	template, exists := c.config.GetCommandTemplate(req.CommandName)
	if !exists {
		c.sendError(conn, req.CommandID, fmt.Sprintf("Command template not found: %s", req.CommandName))
		return
	}

	// Build command with target parameter
	fullCommand := template
	if req.Target != "" {
		fullCommand = template + " " + req.Target
	}

	log.Printf("Executing command: %s (ID: %s)", fullCommand, req.CommandID)

	// Parse command
	parts := strings.Fields(fullCommand)
	if len(parts) == 0 {
		c.sendError(conn, req.CommandID, "Empty command")
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	// Store command for potential stopping
	c.commandsLock.Lock()
	c.activeCommands[req.CommandID] = cmd
	c.commandsLock.Unlock()

	// Clean up command when done
	defer func() {
		c.commandsLock.Lock()
		delete(c.activeCommands, req.CommandID)
		c.commandsLock.Unlock()
	}()

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.sendError(conn, req.CommandID, fmt.Sprintf("Failed to get stdout pipe: %v", err))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.sendError(conn, req.CommandID, fmt.Sprintf("Failed to get stderr pipe: %v", err))
		return
	}

	// Start command
	if err := cmd.Start(); err != nil {
		c.sendError(conn, req.CommandID, fmt.Sprintf("Failed to start command: %v", err))
		return
	}

	// Create channels for coordinating goroutines
	done := make(chan error, 1)
	outputDone := make(chan bool, 2)

	// Read stdout
	go func() {
		defer func() { outputDone <- true }()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			c.sendOutput(conn, req.CommandID, scanner.Text(), false)
		}
		if err := scanner.Err(); err != nil {
			// Ignore pipe closed errors, this is normal command completion behavior
			if !isClosedPipeError(err) {
				c.sendOutput(conn, req.CommandID, fmt.Sprintf("Error reading stdout: %v", err), true)
			}
		}
	}()

	// Read stderr
	go func() {
		defer func() { outputDone <- true }()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			c.sendOutput(conn, req.CommandID, scanner.Text(), true)
		}
		if err := scanner.Err(); err != nil {
			// Ignore pipe closed errors, this is normal command completion behavior
			if !isClosedPipeError(err) {
				c.sendOutput(conn, req.CommandID, fmt.Sprintf("Error reading stderr: %v", err), true)
			}
		}
	}()

	// Wait for command to complete
	go func() {
		err := cmd.Wait()
		done <- err

		// Give reading goroutines time to complete after command ends
		// Then close pipes to avoid reading from closed pipes
		time.Sleep(100 * time.Millisecond)
		stdout.Close()
		stderr.Close()
	}()

	// Wait for completion and all output to be read
	cmdErr := <-done
	<-outputDone
	<-outputDone

	// Send error message if command execution failed
	if cmdErr != nil {
		c.sendOutput(conn, req.CommandID, fmt.Sprintf("Command failed: %v", cmdErr), true)
	}

	// Send completion message
	c.sendCompletion(conn, req.CommandID)
}

// stopCommand stops a running command
func (c *Client) stopCommand(commandID string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()

	if cmd, exists := c.activeCommands[commandID]; exists {
		if cmd.Process != nil {
			log.Printf("Stopping command: %s", commandID)
			cmd.Process.Kill()
		}
	}
}

// sendCommandResponse sends a command response to the server
func (c *Client) sendCommandResponse(conn *websocket.Conn, commandID, output, errorMsg string, isComplete, isError bool) {
	resp := CommandResponse{
		Type:       "command_output",
		CommandID:  commandID,
		Output:     output,
		Error:      errorMsg,
		IsComplete: isComplete,
		IsError:    isError,
	}

	if err := conn.WriteJSON(resp); err != nil {
		log.Printf("Failed to send command response: %v", err)
	}
}

// sendOutput sends command output to the server
func (c *Client) sendOutput(conn *websocket.Conn, commandID, output string, isError bool) {
	c.sendCommandResponse(conn, commandID, output, "", false, isError)
}

// sendError sends an error message to the server
func (c *Client) sendError(conn *websocket.Conn, commandID, errorMsg string) {
	c.sendCommandResponse(conn, commandID, "", errorMsg, true, true)
}

// sendCompletion sends a completion message to the server
func (c *Client) sendCompletion(conn *websocket.Conn, commandID string) {
	c.sendCommandResponse(conn, commandID, "", "", true, false)
}

// isClosedPipeError checks if the error is a closed pipe error
func isClosedPipeError(err error) bool {
	if err == nil {
		return false
	}

	// Check common pipe closed error messages
	errStr := err.Error()
	return strings.Contains(errStr, "file already closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "use of closed file") ||
		err == os.ErrClosed
}
