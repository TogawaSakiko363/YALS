package agent

import (
	"YALS/internal/config"
	"YALS/internal/logger"

	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Shell operators that require bash execution
var shellOperators = []string{"|", "&&", "||", ">", "<", ";"}

// ActiveCommand represents an active command with its details
type ActiveCommand struct {
	Cmd         *exec.Cmd
	FullCommand string
}

// Client represents an agent client that connects to the server
type Client struct {
	config         *config.AgentConfig
	activeCommands map[string]*ActiveCommand
	commandsLock   sync.RWMutex
}

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name"`
	Target      string `json:"target"`
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
	OutputMode string `json:"output_mode,omitempty"`
}

// NewClient creates a new agent client (deprecated, use NewClientWithConfig)
func NewClient(password string) *Client {
	agentConfig := &config.AgentConfig{}
	agentConfig.Server.Password = password
	return NewClientWithConfig(agentConfig)
}

// NewClientWithConfig creates a new agent client with configuration
func NewClientWithConfig(agentConfig *config.AgentConfig) *Client {

	return &Client{
		config:         agentConfig,
		activeCommands: make(map[string]*ActiveCommand),
	}
}

// ConnectToServer connects to the server and handles the WebSocket connection
func (c *Client) ConnectToServer() error {
	// Select protocol based on configuration
	protocol := "ws"
	if c.config.Server.TLS {
		protocol = "wss"
	}

	serverURL := fmt.Sprintf("%s://%s:%d/ws/agent", protocol, c.config.Server.Host, c.config.Server.Port)

	// Set up headers for authentication
	headers := http.Header{}
	headers.Set("X-Agent-Password", c.config.Server.Password)

	// Create dialer with 64KB buffers
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		ReadBufferSize:   65536,
		WriteBufferSize:  65536,
	}

	logger.Infof("Connecting to server at %s", serverURL)

	// Connect to server
	conn, _, err := dialer.Dial(serverURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	logger.Infof("Connected to server successfully")

	// Set up ping/pong handling
	conn.SetPongHandler(func(appData string) error {
		logger.Debugf("Received pong from server")
		return nil
	})

	// Send handshake with agent information
	handshake := map[string]any{
		"type":     "handshake",
		"name":     c.config.Agent.Name,
		"group":    c.config.Agent.Group,
		"details":  c.config.Agent.Details,
		"commands": c.config.GetAvailableCommands(),
	}

	if err := conn.WriteJSON(handshake); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	logger.Infof("Sent handshake with %d available commands", len(c.config.Commands))

	// Wait for handshake acknowledgment
	var ack map[string]any
	if err := conn.ReadJSON(&ack); err != nil {
		return fmt.Errorf("failed to read handshake ack: %w", err)
	}

	if ackType, ok := ack["type"].(string); !ok || ackType != "handshake_ack" {
		return fmt.Errorf("invalid handshake acknowledgment")
	}

	logger.Infof("Handshake completed successfully")

	// Handle incoming messages
	for {
		var req CommandRequest
		if err := conn.ReadJSON(&req); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("WebSocket error: %v", err)
			}
			break
		}

		switch req.Type {
		case "execute_command":
			go c.executeCommand(conn, req)
		case "stop_command":
			c.stopCommand(req.CommandID)
		default:
			logger.Warnf("Unknown message type: %s", req.Type)
		}
	}

	logger.Infof("Disconnected from server")
	return nil
}

// executeCommand executes a command and streams the output
func (c *Client) executeCommand(conn *websocket.Conn, req CommandRequest) {
	// Validate and prepare command
	fullCommand, cmd, err := c.prepareCommand(req)
	if err != nil {
		c.sendError(conn, req.CommandID, err.Error())
		return
	}

	logger.Infof("Executing command: %s", req.CommandID)

	// Store and manage active command
	c.storeActiveCommand(req.CommandID, cmd, fullCommand)
	defer c.removeActiveCommand(req.CommandID)

	// Execute command with streaming output
	if err := c.runCommandWithStreaming(conn, req.CommandID, cmd); err != nil {
		c.sendError(conn, req.CommandID, err.Error())
		return
	}

	c.sendCompletion(conn, req.CommandID)
}

// prepareCommand validates and prepares a command for execution
func (c *Client) prepareCommand(req CommandRequest) (string, *exec.Cmd, error) {
	// Security check: Verify command is allowed
	if !c.config.IsCommandAllowed(req.CommandName) {
		logger.Warnf("SECURITY: Blocked unauthorized command '%s' from server", req.CommandName)
		return "", nil, fmt.Errorf("command '%s' is not allowed", req.CommandName)
	}

	// Get command configuration
	cmdConfig, exists := c.config.GetCommandConfig(req.CommandName)
	if !exists {
		return "", nil, fmt.Errorf("command configuration not found: %s", req.CommandName)
	}

	// Get command template for traditional commands
	template := cmdConfig.Template
	if template == "" {
		return "", nil, fmt.Errorf("command template not found: %s", req.CommandName)
	}

	// Build full command with target parameter (only if not ignored)
	fullCommand := template
	if req.Target != "" && !cmdConfig.IgnoreTarget {
		fullCommand = template + " " + req.Target
	}

	// Create command based on complexity
	cmd := c.createCommand(fullCommand)
	if cmd == nil {
		return "", nil, fmt.Errorf("empty command")
	}

	return fullCommand, cmd, nil
}

// createCommand creates an exec.Cmd based on command complexity
func (c *Client) createCommand(fullCommand string) *exec.Cmd {
	// Check if command contains shell operators
	for _, op := range shellOperators {
		if strings.Contains(fullCommand, op) {
			return exec.Command("/bin/bash", "-c", fullCommand)
		}
	}

	// Simple command - parse normally
	parts := strings.Fields(fullCommand)
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], parts[1:]...)
}

// storeActiveCommand stores a command for potential stopping
func (c *Client) storeActiveCommand(commandID string, cmd *exec.Cmd, fullCommand string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()
	c.activeCommands[commandID] = &ActiveCommand{
		Cmd:         cmd,
		FullCommand: fullCommand,
	}
}

// removeActiveCommand removes a command from active commands
func (c *Client) removeActiveCommand(commandID string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()
	delete(c.activeCommands, commandID)
}

// runCommandWithStreaming executes a command and streams its output with complete replacement
func (c *Client) runCommandWithStreaming(conn *websocket.Conn, commandID string, cmd *exec.Cmd) error {
	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Accumulate output with periodic updates
	var stdoutLines []string
	var stderrLines []string
	var stdoutMutex, stderrMutex sync.Mutex

	done := make(chan error, 1)
	outputDone := make(chan bool, 2)

	// Read stdout and stderr concurrently with accumulation
	go c.accumulateOutput(stdout, &stdoutLines, &stdoutMutex, outputDone)
	go c.accumulateOutput(stderr, &stderrLines, &stderrMutex, outputDone)

	// Send periodic updates
	updateTicker := time.NewTicker(250 * time.Millisecond)
	defer updateTicker.Stop()

	go func() {
		for range updateTicker.C {
			// Combine stdout and stderr
			stdoutMutex.Lock()
			stderrMutex.Lock()

			var allLines []string
			allLines = append(allLines, stdoutLines...)
			allLines = append(allLines, stderrLines...)

			if len(allLines) > 0 {
				output := strings.Join(allLines, "\n")
				c.sendOutput(conn, commandID, output, false)
			}

			stderrMutex.Unlock()
			stdoutMutex.Unlock()
		}
	}()

	// Wait for command completion
	go func() {
		err := cmd.Wait()
		done <- err
		time.Sleep(200 * time.Millisecond) // Allow output readers to finish
		stdout.Close()
		stderr.Close()
		updateTicker.Stop()
	}()

	// Wait for completion and output processing
	cmdErr := <-done
	<-outputDone
	<-outputDone

	// Send final output
	stdoutMutex.Lock()
	stderrMutex.Lock()
	var allLines []string
	allLines = append(allLines, stdoutLines...)
	allLines = append(allLines, stderrLines...)
	stderrMutex.Unlock()
	stdoutMutex.Unlock()

	if len(allLines) > 0 {
		finalOutput := strings.Join(allLines, "\n")
		if cmdErr != nil {
			finalOutput += fmt.Sprintf("\nCommand failed: %v", cmdErr)
		}
		c.sendOutput(conn, commandID, finalOutput, cmdErr != nil)
	} else if cmdErr != nil {
		c.sendOutput(conn, commandID, fmt.Sprintf("Command failed: %v", cmdErr), true)
	}

	time.Sleep(100 * time.Millisecond)

	return nil
}

// accumulateOutput reads from a pipe and accumulates output lines
func (c *Client) accumulateOutput(pipe interface{ Read([]byte) (int, error) }, lines *[]string, mutex *sync.Mutex, done chan<- bool) {
	defer func() { done <- true }()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		mutex.Lock()
		*lines = append(*lines, line)
		mutex.Unlock()
	}

	if err := scanner.Err(); err != nil && !isClosedPipeError(err) {
		mutex.Lock()
		*lines = append(*lines, fmt.Sprintf("Error reading output: %v", err))
		mutex.Unlock()
	}
}

// isComplexCommand checks if a command needs shell execution
func (c *Client) isComplexCommand(fullCommand string) bool {
	complexOperators := shellOperators[:3]
	for _, op := range complexOperators {
		if strings.Contains(fullCommand, op) {
			return true
		}
	}
	return false
}

// stopCommand stops a running command
func (c *Client) stopCommand(commandID string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()

	activeCmd, exists := c.activeCommands[commandID]
	if !exists || activeCmd.Cmd.Process == nil {
		logger.Warnf("No active command found to stop: %s", commandID)
		return
	}

	logger.Infof("Stopping command: %s", commandID)

	// Determine timeout based on command complexity
	timeout := 1 * time.Second
	if c.isComplexCommand(activeCmd.FullCommand) {
		timeout = 500 * time.Millisecond
	}

	// Try graceful termination first
	if err := activeCmd.Cmd.Process.Signal(os.Interrupt); err != nil {
		activeCmd.Cmd.Process.Kill()
		return
	}

	// Force kill after timeout
	go func() {
		time.Sleep(timeout)
		activeCmd.Cmd.Process.Kill()
	}()
}

// sendCommandResponse sends a command response to the server
func (c *Client) sendCommandResponse(conn *websocket.Conn, commandID, output, errorMsg string, isComplete, isError bool) {
	c.sendCommandResponseWithMode(conn, commandID, output, errorMsg, isComplete, isError, "append")
}

// sendCommandResponseWithMode sends a command response to the server with specified output mode
func (c *Client) sendCommandResponseWithMode(conn *websocket.Conn, commandID, output, errorMsg string, isComplete, isError bool, outputMode string) {
	resp := CommandResponse{
		Type:       "command_output",
		CommandID:  commandID,
		Output:     output,
		Error:      errorMsg,
		IsComplete: isComplete,
		IsError:    isError,
		OutputMode: outputMode,
	}

	if err := conn.WriteJSON(resp); err != nil {
		logger.Errorf("Failed to send command response: %v", err)
	}
}

// sendOutput sends command output to the server (uses replace mode by default)
func (c *Client) sendOutput(conn *websocket.Conn, commandID, output string, isError bool) {
	c.sendCommandResponseWithMode(conn, commandID, output, "", false, isError, "replace")
}

// sendError sends an error message to the server
func (c *Client) sendError(conn *websocket.Conn, commandID, errorMsg string) {
	c.sendCommandResponse(conn, commandID, errorMsg, errorMsg, true, true)
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
	errStr := err.Error()
	return strings.Contains(errStr, "file already closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "use of closed file") ||
		err == os.ErrClosed
}
