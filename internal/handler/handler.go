package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/validator"

	"github.com/gorilla/websocket"
)

// Handler handles HTTP and WebSocket requests
type Handler struct {
	agentManager   *agent.Manager
	upgrader       websocket.Upgrader
	clients        map[*websocket.Conn]bool
	clientIPs      map[*websocket.Conn]string // Track client IPs
	clientsLock    sync.RWMutex
	pingInterval   time.Duration
	pongWait       time.Duration
	activeCommands map[string]chan bool // Channel for stopping commands
	commandsLock   sync.RWMutex
	webDir         string // Frontend files directory
}

// CommandRequest represents a command request from the client
type CommandRequest struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
}

// CommandResponse represents a command response to the client
type CommandResponse struct {
	Success bool   `json:"success"`
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Target  string `json:"target"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// StreamingCommandResponse represents a streaming command response
type StreamingCommandResponse struct {
	Type       string `json:"type"`
	Success    bool   `json:"success"`
	Agent      string `json:"agent"`
	Command    string `json:"command"`
	Target     string `json:"target"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	IsComplete bool   `json:"is_complete"`
}

// AgentStatusUpdate represents an agent status update
type AgentStatusUpdate struct {
	Type   string           `json:"type"`
	Groups []map[string]any `json:"groups"`
}

// CommandsListResponse represents the response for available commands
type CommandsListResponse struct {
	Type     string                    `json:"type"`
	Commands []validator.CommandDetail `json:"commands"`
}

// AppConfigResponse represents the application configuration response
type AppConfigResponse struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Config  map[string]string `json:"config"`
}

// NewHandler creates a new handler
func NewHandler(agentManager *agent.Manager, pingInterval, pongWait time.Duration) *Handler {
	return &Handler{
		agentManager: agentManager,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  65536, // 64KB read buffer
			WriteBufferSize: 65536, // 64KB write buffer
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in this example
			},
		},
		clients:        make(map[*websocket.Conn]bool),
		clientIPs:      make(map[*websocket.Conn]string),
		pingInterval:   pingInterval,
		pongWait:       pongWait,
		activeCommands: make(map[string]chan bool),
	}
}

// SetupRoutes sets up the HTTP routes
func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	// Store webDir for use in handlers
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/ws", h.handleWebSocket)
	mux.HandleFunc("/ws/api/agent", h.handleAgentWebSocket)

	// Serve static files from specified web directory
	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

// handleIndex handles the index page
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Handle specific paths
	switch r.URL.Path {
	case "/":
		indexPath := filepath.Join(h.webDir, "index.html")
		http.ServeFile(w, r, indexPath)
		return
	default:
		// Try to serve from web directory
		filePath := filepath.Join(h.webDir, r.URL.Path[1:])
		if _, err := http.Dir(h.webDir).Open(r.URL.Path[1:]); err == nil {
			http.ServeFile(w, r, filePath)
			return
		}

		// Handle SPA routing - serve index.html for all other paths
		// This implements the rule from _redirects: /* /index.html 200
		if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "application/json") {
			indexPath := filepath.Join(h.webDir, "index.html")
			http.ServeFile(w, r, indexPath)
			return
		}

		http.NotFound(w, r)
		return
	}
}

// handleWebSocket handles WebSocket connections from web clients
func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}

	// Get client IP
	clientIP := h.getRealIP(r)

	// Register client
	h.clientsLock.Lock()
	h.clients[conn] = true
	h.clientIPs[conn] = clientIP
	h.clientsLock.Unlock()

	// Set up connection handling
	conn.SetReadLimit(32768) // 32KB limit for web client messages
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	// Send initial agent status
	h.sendAgentStatus(conn)

	// Start ping routine
	go h.pingClient(conn)

	// Handle incoming messages
	go h.readPump(conn, clientIP)
}

// handleAgentWebSocket handles WebSocket connections from agents
func (h *Handler) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check password
	password := r.Header.Get("X-Agent-Password")
	cfg := config.GetConfig()
	if cfg == nil || password != cfg.Server.Password {
		realIP := h.getRealIP(r)
		logger.Warnf("Unauthorized agent connection attempt from %s", realIP)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade agent connection: %v", err)
		return
	}

	// Set up connection handling for agents
	conn.SetReadLimit(65536) // 64KB limit for agent messages (handshake can be large)
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	realIP := h.getRealIP(r)
	logger.Infof("Agent connected from %s", realIP)

	// Start ping routine for agent (keep connection alive)
	go h.pingAgent(conn)

	// Handle agent connection
	h.agentManager.HandleAgentConnection(conn)
}

// getRealIP gets the real client IP address, supports reverse proxy
func (h *Handler) getRealIP(r *http.Request) string {
	// First try X-Real-IP header
	ip := r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	// Then try X-Forwarded-For header (take first IP)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For may contain multiple IPs, take the first one
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}

	// Finally use RemoteAddr
	return r.RemoteAddr
}

// pingClient sends periodic pings to the client
func (h *Handler) pingClient(conn *websocket.Conn) {
	ticker := time.NewTicker(h.pingInterval)
	defer func() {
		ticker.Stop()
		conn.Close()

		// Unregister client
		h.clientsLock.Lock()
		delete(h.clients, conn)
		delete(h.clientIPs, conn)
		h.clientsLock.Unlock()
	}()

	for range ticker.C {
		if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
			return
		}
	}
}

// pingAgent sends periodic pings to keep agent connection alive
func (h *Handler) pingAgent(conn *websocket.Conn) {
	ticker := time.NewTicker(h.pingInterval)
	defer func() {
		ticker.Stop()
		conn.Close()
		logger.Debug("Agent ping routine stopped, connection closed")
	}()

	for range ticker.C {
		if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
			logger.Errorf("Failed to send ping to agent: %v", err)
			return
		}
	}
}

// readPump handles incoming messages from the client
func (h *Handler) readPump(conn *websocket.Conn, clientIP string) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("WebSocket error: %v", err)
			}
			break
		}

		var req CommandRequest
		if err := json.Unmarshal(message, &req); err != nil {
			logger.Errorf("Failed to parse command request: %v", err)
			continue
		}

		switch req.Type {
		case "get_commands":
			h.handleGetCommands(conn)
		case "get_agent_commands":
			h.handleGetAgentCommands(conn, req)
		case "get_config":
			h.handleGetConfig(conn)
		case "get_agent_stats":
			h.handleGetAgentStats(conn)
		case "execute_command":
			go h.handleCommand(conn, req, clientIP)
		case "stop_command":
			h.handleStopCommand(req, clientIP)
		default:
			logger.Warnf("Unknown message type: %s", req.Type)
		}
	}
}

// handleCommand handles a command request
func (h *Handler) handleCommand(conn *websocket.Conn, req CommandRequest, clientIP string) {
	resp := h.createCommandResponse(req, false)

	// Get agent and check if command requires target validation
	agents := h.agentManager.GetAgents()
	var agentCommands []string
	var requiresTarget bool = true

	for _, a := range agents {
		if a["name"] == req.Agent {
			// Check agent status: 1=online, 0=offline in frontend format
			if status, ok := a["status"].(int); !ok || status != 1 {
				resp.Error = "Agent is not connected"
				h.sendResponse(conn, resp)
				return
			}

			if cmds, ok := a["commands"].([]string); ok {
				agentCommands = cmds
			}
			break
		}
	}

	// Check if this command ignores target (requires target validation)
	agentCommands2 := h.agentManager.GetAgentCommands(req.Agent)
	for _, cmd := range agentCommands2 {
		if cmd.Name == req.Command {
			// This is a bit of a hack since GetAgentCommands returns validator.CommandDetail
			// but we need to check the actual command config. Let's get it directly.
			break
		}
	}

	// Get command configuration to check ignore_target
	if cmdConfig, exists := h.getCommandConfig(req.Agent, req.Command); exists && cmdConfig.IgnoreTarget {
		requiresTarget = false
	}

	// Validate input only if target is required
	if requiresTarget {
		inputType := validator.ValidateInput(req.Target)
		if inputType == validator.InvalidInput {
			resp.Error = "Invalid target: must be an IP address or domain name"
			h.sendResponse(conn, resp)
			return
		}
	}

	// Sanitize command
	cmd, ok := validator.SanitizeCommand(req.Command, req.Target, agentCommands)
	if !ok {
		resp.Error = "Invalid command"
		h.sendResponse(conn, resp)
		return
	}

	// Create stop channel
	commandID := h.generateCommandID(req.Command, req.Target, req.Agent)
	stopChan := make(chan bool, 1)

	// Log command execution with client IP
	logger.Infof("Client [%s] sent run signal for command: %s", clientIP, commandID)

	h.setActiveCommand(commandID, stopChan)

	// Execute command with streaming output
	err := h.agentManager.ExecuteCommandStreamingWithStopAndID(req.Agent, cmd, commandID, stopChan, func(output string, isError bool, isComplete bool, isStopped bool) {
		if isStopped {
			// Send stopped message
			stoppedResp := h.createCommandResponse(req, false)
			stoppedResp.Output = "*** Stopped ***"
			stoppedResp.Error = "*** Stopped ***"
			h.sendStreamingResponse(conn, stoppedResp, true)
		} else if isComplete {
			// Send completion message
			if isError {
				// Command failed (e.g., queue limit reached)
				resp.Success = false
				resp.Error = output
				resp.Output = ""
			} else {
				// Command completed successfully
				resp.Success = true
				resp.Output = "" // Final message with empty output to signal completion
			}
			h.sendStreamingResponse(conn, resp, true)
		} else {
			// Send streaming output
			streamResp := h.createCommandResponse(req, true)
			streamResp.Output = output
			if isError {
				streamResp.Error = output
				streamResp.Output = ""
			}
			h.sendStreamingResponse(conn, streamResp, false)
		}
	})

	// Clean up stop channel
	h.removeActiveCommand(commandID)

	if err != nil {
		resp.Error = err.Error()
		h.sendResponse(conn, resp)
		return
	}
}

// handleGetCommands handles the get commands request
func (h *Handler) handleGetCommands(conn *websocket.Conn) {
	// Get all unique commands from all connected agents
	commands := h.agentManager.GetAllAvailableCommands()
	response := CommandsListResponse{
		Type:     "commands_list",
		Commands: commands,
	}

	h.sendJSONResponse(conn, response, "commands response")
}

// handleGetAgentCommands handles the get agent commands request
func (h *Handler) handleGetAgentCommands(conn *websocket.Conn, req CommandRequest) {
	if req.Agent == "" {
		logger.Warnf("Agent name is required for get_agent_commands request")
		return
	}

	// Get agent's available commands directly from the agent manager
	commands := h.agentManager.GetAgentCommands(req.Agent)
	response := CommandsListResponse{
		Type:     "commands_list",
		Commands: commands,
	}

	h.sendJSONResponse(conn, response, "agent commands response")
}

// handleGetAgentStats handles the get_agent_stats request
func (h *Handler) handleGetAgentStats(conn *websocket.Conn) {
	stats := h.agentManager.GetAgentStats()

	response := map[string]any{
		"type":  "agent_stats",
		"stats": stats,
	}

	h.sendJSONResponse(conn, response, "agent stats")
}

// handleGetConfig handles the get_config request
func (h *Handler) handleGetConfig(conn *websocket.Conn) {
	cfg := config.GetConfig()
	if cfg == nil {
		logger.Errorf("Configuration not available")
		return
	}

	// Get agent statistics
	stats := h.agentManager.GetAgentStats()

	response := AppConfigResponse{
		Type:    "app_config",
		Version: cfg.App.Version,
		Config: map[string]string{
			"server_host": cfg.Server.Host,
			"server_port": fmt.Sprintf("%d", cfg.Server.Port),
			"log_level":   cfg.Server.LogLevel,
		},
	}

	// Add agent statistics to response
	if response.Config == nil {
		response.Config = make(map[string]string)
	}
	response.Config["agents_total"] = fmt.Sprintf("%d", stats["total"])
	response.Config["agents_online"] = fmt.Sprintf("%d", stats["online"])
	response.Config["agents_offline"] = fmt.Sprintf("%d", stats["offline"])

	if err := conn.WriteJSON(response); err != nil {
		logger.Errorf("Failed to send app config: %v", err)
	}
}

// sendJSONResponse sends a JSON response to the client
func (h *Handler) sendJSONResponse(conn *websocket.Conn, response interface{}, responseType string) {
	data, err := json.Marshal(response)
	if err != nil {
		logger.Errorf("Failed to marshal %s: %v", responseType, err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	if _, ok := h.clients[conn]; ok {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// sendResponse sends a response to the client
func (h *Handler) sendResponse(conn *websocket.Conn, resp CommandResponse) {
	h.sendJSONResponse(conn, resp, "command response")
}

// sendStreamingResponse sends a streaming response to the client
func (h *Handler) sendStreamingResponse(conn *websocket.Conn, resp CommandResponse, isComplete bool) {
	streamResp := StreamingCommandResponse{
		Type:       "command_output",
		Success:    resp.Success,
		Agent:      resp.Agent,
		Command:    resp.Command,
		Target:     resp.Target,
		Output:     resp.Output,
		Error:      resp.Error,
		IsComplete: isComplete,
	}

	data, err := json.Marshal(streamResp)
	if err != nil {
		logger.Errorf("Failed to marshal streaming response: %v", err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	if _, ok := h.clients[conn]; ok {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// sendAgentStatus sends the agent status to a client
func (h *Handler) sendAgentStatus(conn *websocket.Conn) {
	groups := h.agentManager.GetAgentGroups()
	update := map[string]any{
		"type":   "agent_status",
		"groups": groups,
	}

	// Send directly without client lock check (for initial connection)
	data, err := json.Marshal(update)
	if err != nil {
		logger.Errorf("Failed to marshal agent status: %v", err)
		return
	}

	conn.WriteMessage(websocket.TextMessage, data)
}

// handleStopCommand handles a stop command request
func (h *Handler) handleStopCommand(req CommandRequest, clientIP string) {
	if req.CommandID == "" {
		logger.Warnf("Stop command request missing command_id")
		return
	}

	if h.stopActiveCommand(req.CommandID) {
		logger.Infof("Client [%s] sent stop signal for command: %s", clientIP, req.CommandID)
	}
}

// BroadcastAgentStatus broadcasts agent status to all clients
func (h *Handler) BroadcastAgentStatus() {
	groups := h.agentManager.GetAgentGroups()
	update := AgentStatusUpdate{
		Type:   "agent_status",
		Groups: groups,
	}

	h.broadcastToAllClients(update, "agent status")
}

// broadcastToAllClients broadcasts a message to all connected clients
func (h *Handler) broadcastToAllClients(message interface{}, messageType string) {
	data, err := json.Marshal(message)
	if err != nil {
		logger.Errorf("Failed to marshal %s: %v", messageType, err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// generateCommandID generates a unique command ID
func (h *Handler) generateCommandID(command, target, agent string) string {
	return fmt.Sprintf("%s-%s-%s", command, target, agent)
}

// createCommandResponse creates a base command response
func (h *Handler) createCommandResponse(req CommandRequest, success bool) CommandResponse {
	return CommandResponse{
		Success: success,
		Agent:   req.Agent,
		Command: req.Command,
		Target:  req.Target,
	}
}

// setActiveCommand safely sets an active command
func (h *Handler) setActiveCommand(commandID string, stopChan chan bool) {
	h.commandsLock.Lock()
	h.activeCommands[commandID] = stopChan
	h.commandsLock.Unlock()
}

// removeActiveCommand safely removes an active command
func (h *Handler) removeActiveCommand(commandID string) {
	h.commandsLock.Lock()
	delete(h.activeCommands, commandID)
	h.commandsLock.Unlock()
}

// stopActiveCommand safely stops and removes an active command
func (h *Handler) stopActiveCommand(commandID string) bool {
	h.commandsLock.Lock()
	defer h.commandsLock.Unlock()

	if stopChan, exists := h.activeCommands[commandID]; exists {
		close(stopChan) // Send stop signal
		delete(h.activeCommands, commandID)
		return true
	}
	return false
}

// getCommandConfig gets the command configuration from the agent manager
func (h *Handler) getCommandConfig(agentName, commandName string) (config.CommandInfo, bool) {
	// This is a helper method to get command config from agent manager
	// We need to access the agent's command configuration
	agents := h.agentManager.GetAgents()
	for _, a := range agents {
		if a["name"] == agentName {
			// Get the agent's commands with full configuration
			agentCommands := h.agentManager.GetAgentCommands(agentName)
			for _, cmd := range agentCommands {
				if cmd.Name == commandName {
					// Convert validator.CommandDetail to config.CommandInfo
					// This is a limitation of the current design - we need to access the full config
					// For now, we'll use the agent manager's internal method
					return h.agentManager.GetCommandConfigInternal(agentName, commandName)
				}
			}
		}
	}
	return config.CommandInfo{}, false
}
