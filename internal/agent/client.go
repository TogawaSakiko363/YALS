package agent

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Client represents an agent client that connects to the server
type Client struct {
	password       string
	upgrader       websocket.Upgrader
	activeCommands map[string]*exec.Cmd
	commandsLock   sync.RWMutex
}

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type      string `json:"type"`
	Command   string `json:"command"`
	CommandID string `json:"command_id"`
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

// NewClient creates a new agent client
func NewClient(password string) *Client {
	return &Client{
		password: password,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins
			},
		},
		activeCommands: make(map[string]*exec.Cmd),
	}
}

// HandleWebSocket handles WebSocket connections from the server
func (c *Client) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check password
	password := r.Header.Get("X-Agent-Password")
	if password != c.password {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Server connected from %s", conn.RemoteAddr())

	// Send authentication success
	authResp := map[string]interface{}{
		"type":    "auth_success",
		"message": "Agent authenticated successfully",
	}
	if err := conn.WriteJSON(authResp); err != nil {
		log.Printf("Failed to send auth response: %v", err)
		return
	}

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

	log.Printf("Server disconnected from %s", conn.RemoteAddr())
}

// executeCommand executes a command and streams the output
func (c *Client) executeCommand(conn *websocket.Conn, req CommandRequest) {
	log.Printf("Executing command: %s (ID: %s)", req.Command, req.CommandID)

	// Parse command
	parts := strings.Fields(req.Command)
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
			c.sendOutput(conn, req.CommandID, fmt.Sprintf("Error reading stdout: %v", err), true)
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
			c.sendOutput(conn, req.CommandID, fmt.Sprintf("Error reading stderr: %v", err), true)
		}
	}()

	// Wait for command to complete
	go func() {
		done <- cmd.Wait()
	}()

	// Wait for completion and all output to be read
	<-done
	<-outputDone
	<-outputDone

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

// sendOutput sends command output to the server
func (c *Client) sendOutput(conn *websocket.Conn, commandID, output string, isError bool) {
	resp := CommandResponse{
		Type:       "command_output",
		CommandID:  commandID,
		Output:     output,
		IsComplete: false,
		IsError:    isError,
	}

	if err := conn.WriteJSON(resp); err != nil {
		log.Printf("Failed to send output: %v", err)
	}
}

// sendError sends an error message to the server
func (c *Client) sendError(conn *websocket.Conn, commandID, errorMsg string) {
	resp := CommandResponse{
		Type:       "command_output",
		CommandID:  commandID,
		Error:      errorMsg,
		IsComplete: true,
		IsError:    true,
	}

	if err := conn.WriteJSON(resp); err != nil {
		log.Printf("Failed to send error: %v", err)
	}
}

// sendCompletion sends a completion message to the server
func (c *Client) sendCompletion(conn *websocket.Conn, commandID string) {
	resp := CommandResponse{
		Type:       "command_output",
		CommandID:  commandID,
		Output:     "",
		IsComplete: true,
		IsError:    false,
	}

	if err := conn.WriteJSON(resp); err != nil {
		log.Printf("Failed to send completion: %v", err)
	}
}
