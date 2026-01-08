package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/utils"
	"YALS/internal/validator"

	"github.com/gorilla/websocket"
)

// Handler handles HTTP and WebSocket requests
type Handler struct {
	agentManager    *agent.Manager
	upgrader        websocket.Upgrader
	clients         map[*websocket.Conn]bool
	clientIPs       map[*websocket.Conn]string
	clientSessions  map[*websocket.Conn]string
	sessionConns    map[string]*websocket.Conn
	commandSessions map[string]string
	clientsLock     sync.RWMutex
	pingInterval    time.Duration
	pongWait        time.Duration
	activeCommands  map[string]chan bool
	commandsLock    sync.RWMutex
	webDir          string
	rateLimiter     *RateLimiter
}

// RateLimiter manages rate limiting for command execution
type RateLimiter struct {
	enabled     bool
	maxCommands int
	timeWindow  time.Duration
	sessions    map[string]*SessionRateLimit
	mu          sync.RWMutex
}

// SessionRateLimit tracks command execution for a session
type SessionRateLimit struct {
	timestamps []time.Time
}

// CommandRequest represents a command request from the client
type CommandRequest struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
	IPVersion string `json:"ip_version,omitempty"` // "auto", "ipv4", or "ipv6"
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
	CommandID  string `json:"command_id,omitempty"`
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

// AppVersion represents the application configuration response
type AppVersion struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Config  map[string]string `json:"config"`
}

// NewHandler creates a new handler
func NewHandler(agentManager *agent.Manager, pingInterval, pongWait time.Duration) *Handler {
	cfg := config.GetConfig()

	rateLimiter := &RateLimiter{
		enabled:     cfg.RateLimit.Enabled,
		maxCommands: cfg.RateLimit.MaxCommands,
		timeWindow:  time.Duration(cfg.RateLimit.TimeWindow) * time.Second,
		sessions:    make(map[string]*SessionRateLimit),
	}

	return &Handler{
		agentManager: agentManager,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  65536,
			WriteBufferSize: 65536,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		clients:         make(map[*websocket.Conn]bool),
		clientIPs:       make(map[*websocket.Conn]string),
		clientSessions:  make(map[*websocket.Conn]string),
		sessionConns:    make(map[string]*websocket.Conn),
		commandSessions: make(map[string]string),
		pingInterval:    pingInterval,
		pongWait:        pongWait,
		activeCommands:  make(map[string]chan bool),
		rateLimiter:     rateLimiter,
	}
}

// SetupRoutes sets up the HTTP routes
func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/session", h.handleGetSession)
	mux.HandleFunc("/ws/", h.handleWebSocket)
	mux.HandleFunc("/ws/agent", h.handleAgentWebSocket)

	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

// handleIndex handles the index page
func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		indexPath := filepath.Join(h.webDir, "index.html")
		http.ServeFile(w, r, indexPath)
		return
	default:
		filePath := filepath.Join(h.webDir, r.URL.Path[1:])
		if _, err := http.Dir(h.webDir).Open(r.URL.Path[1:]); err == nil {
			http.ServeFile(w, r, filePath)
			return
		}

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
	clientIP := h.getRealIP(r)

	var sessionID string
	path := strings.TrimPrefix(r.URL.Path, "/ws/")

	if path != "" && path != r.URL.Path {
		sessionID = path
	} else {
		sessionID = r.URL.Query().Get("sessionId")
	}

	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d_%s", time.Now().UnixNano(), clientIP)
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Failed to upgrade connection: %v", err)
		return
	}

	h.clientsLock.Lock()
	h.clients[conn] = true
	h.clientIPs[conn] = clientIP
	h.clientSessions[conn] = sessionID
	h.sessionConns[sessionID] = conn
	h.clientsLock.Unlock()

	conn.SetReadLimit(32768)
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	h.sendAgentStatus(conn)
	go h.pingClient(conn)
	go h.readPump(conn, clientIP)
}

// handleAgentWebSocket handles WebSocket connections from agents
func (h *Handler) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
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

	conn.SetReadLimit(65536)
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	realIP := h.getRealIP(r)
	logger.Infof("Agent connected from %s", realIP)

	go h.pingAgent(conn)
	h.agentManager.HandleAgentConnection(conn)
}

// getRealIP gets the real client IP address
func (h *Handler) getRealIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}

	return r.RemoteAddr
}

// pingClient sends periodic pings to the client
func (h *Handler) pingClient(conn *websocket.Conn) {
	ticker := time.NewTicker(h.pingInterval)
	defer func() {
		ticker.Stop()
		conn.Close()

		h.clientsLock.Lock()
		sessionID := h.clientSessions[conn]
		delete(h.clients, conn)
		delete(h.clientIPs, conn)
		delete(h.clientSessions, conn)
		if sessionID != "" {
			delete(h.sessionConns, sessionID)
		}
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

	// Get session ID for rate limiting
	h.clientsLock.RLock()
	sessionID := h.clientSessions[conn]
	h.clientsLock.RUnlock()

	// Check rate limit
	if !h.rateLimiter.checkRateLimit(sessionID) {
		remaining := h.rateLimiter.getRemainingTime(sessionID)
		resp.Success = false
		resp.Error = fmt.Sprintf("Rate limit exceeded. Please wait %d seconds before trying again.", int(remaining.Seconds())+1)
		h.sendStreamingResponseWithID(conn, resp, true, "")
		logger.Warnf("Client [%s] rate limit exceeded for session: %s", clientIP, sessionID)
		return
	}

	agents := h.agentManager.GetAgents()
	var agentCommands []string
	var requiresTarget bool = true

	for _, a := range agents {
		if a["name"] == req.Agent {
			if status, ok := a["status"].(int); !ok || status != 1 {
				resp.Success = false
				resp.Error = "Agent is not connected"
				h.sendStreamingResponseWithID(conn, resp, true, "")
				return
			}

			if cmds, ok := a["commands"].([]string); ok {
				agentCommands = cmds
			}
			break
		}
	}

	// Validate input only if target is required
	if requiresTarget {
		inputType := validator.ValidateInput(req.Target)
		if inputType == validator.InvalidInput {
			resp.Success = false
			resp.Error = "Invalid target: must be an IP address or domain name, and not exceed 256 characters"
			h.sendStreamingResponseWithID(conn, resp, true, "")
			return
		}
	}

	cmd, ok := validator.SanitizeCommand(req.Command, req.Target, agentCommands)
	if !ok {
		resp.Success = false
		resp.Error = "Invalid command"
		h.sendStreamingResponseWithID(conn, resp, true, "")
		return
	}

	// Generate commandID with sessionID included to ensure uniqueness across users
	commandID := h.generateCommandID(req.Command, req.Target, req.Agent, sessionID)
	stopChan := make(chan bool, 1)

	// Store command-to-session mapping
	h.clientsLock.Lock()
	h.commandSessions[commandID] = sessionID
	h.clientsLock.Unlock()

	// Clean up mapping when command completes
	defer func() {
		h.clientsLock.Lock()
		delete(h.commandSessions, commandID)
		h.clientsLock.Unlock()
	}()

	// Log command execution with client IP and session
	logger.Infof("Client [%s] sent run signal for command: %s", clientIP, commandID)

	h.setActiveCommand(commandID, stopChan)

	// Execute command with streaming output
	err := h.agentManager.ExecuteCommandStreamingWithStopAndID(req.Agent, cmd, commandID, req.IPVersion, stopChan, func(output string, isError bool, isComplete bool, isStopped bool) {
		// Get the correct connection for this command using commandID routing
		targetConn := h.getConnectionForCommand(commandID, conn)

		if isStopped {
			// Send stopped message - use append mode to preserve last output
			stoppedResp := h.createCommandResponse(req, true)
			stoppedResp.Output = "\n*** Stopped ***"
			stoppedResp.Error = ""
			h.sendStreamingResponse(targetConn, stoppedResp, true, commandID, "append", true)
		} else if isComplete {
			// Send completion message
			if isError {
				resp.Success = false
				resp.Error = output
				resp.Output = ""
			} else {
				resp.Success = true
				resp.Output = ""
			}
			h.sendStreamingResponseWithID(targetConn, resp, true, commandID)
		} else {
			// Send streaming output
			streamResp := h.createCommandResponse(req, true)
			streamResp.Output = output
			if isError {
				streamResp.Error = output
				streamResp.Output = ""
			}
			h.sendStreamingResponseWithID(targetConn, streamResp, false, commandID)
		}
	})

	h.removeActiveCommand(commandID)

	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		h.sendStreamingResponseWithID(conn, resp, true, commandID)
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

	response := AppVersion{
		Type:    "app_config",
		Version: utils.GetAppVersion(),
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

// getConnectionForCommand returns the WebSocket connection for a given command ID
// If commandID is not found or session is disconnected, returns the original conn
func (h *Handler) getConnectionForCommand(commandID string, fallbackConn *websocket.Conn) *websocket.Conn {
	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	// Find session ID for this command
	sessionID, exists := h.commandSessions[commandID]
	if !exists {
		// Command not found in mapping, use fallback
		return fallbackConn
	}

	// Find connection for this session
	conn, exists := h.sessionConns[sessionID]
	if !exists || conn == nil {
		// Session disconnected, use fallback
		return fallbackConn
	}

	// Verify connection is still active
	if _, active := h.clients[conn]; !active {
		// Connection no longer active, use fallback
		return fallbackConn
	}

	return conn
}

// sendStreamingResponse sends a streaming response to the client with all options
func (h *Handler) sendStreamingResponse(conn *websocket.Conn, resp CommandResponse, isComplete bool, commandID string, outputMode string, stopped bool) {
	streamResp := map[string]any{
		"type":        "command_output",
		"success":     resp.Success,
		"agent":       resp.Agent,
		"command":     resp.Command,
		"target":      resp.Target,
		"output":      resp.Output,
		"error":       resp.Error,
		"is_complete": isComplete,
		"command_id":  commandID,
		"output_mode": outputMode,
		"stopped":     stopped,
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

// sendStreamingResponseWithID sends a streaming response with command ID (default: replace mode, not stopped)
func (h *Handler) sendStreamingResponseWithID(conn *websocket.Conn, resp CommandResponse, isComplete bool, commandID string) {
	h.sendStreamingResponse(conn, resp, isComplete, commandID, "replace", false)
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
func (h *Handler) generateCommandID(command, target, agent, sessionID string) string {
	return fmt.Sprintf("%s-%s-%s-%s", command, target, agent, sessionID)
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
		close(stopChan)
		delete(h.activeCommands, commandID)
		return true
	}
	return false
}

// checkRateLimit checks if a session has exceeded the rate limit
func (rl *RateLimiter) checkRateLimit(sessionID string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	if _, exists := rl.sessions[sessionID]; !exists {
		rl.sessions[sessionID] = &SessionRateLimit{
			timestamps: []time.Time{},
		}
	}

	session := rl.sessions[sessionID]

	// Remove timestamps outside the time window
	validTimestamps := []time.Time{}
	for _, ts := range session.timestamps {
		if now.Sub(ts) < rl.timeWindow {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	session.timestamps = validTimestamps

	// Check if limit exceeded
	if len(session.timestamps) >= rl.maxCommands {
		return false
	}

	// Add current timestamp
	session.timestamps = append(session.timestamps, now)
	return true
}

// getRemainingTime returns the time until the rate limit resets
func (rl *RateLimiter) getRemainingTime(sessionID string) time.Duration {
	if !rl.enabled {
		return 0
	}

	rl.mu.RLock()
	defer rl.mu.RUnlock()

	session, exists := rl.sessions[sessionID]
	if !exists || len(session.timestamps) == 0 {
		return 0
	}

	oldestTimestamp := session.timestamps[0]
	elapsed := time.Since(oldestTimestamp)
	remaining := rl.timeWindow - elapsed

	if remaining < 0 {
		return 0
	}
	return remaining
}

// SessionResponse represents the response for session creation
type SessionResponse struct {
	SessionID string `json:"session_id"`
	Timestamp int64  `json:"timestamp"`
}

// handleGetSession handles the session creation API
func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Generate new session ID
	sessionID := h.generateSessionID()

	// Create response
	response := SessionResponse{
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
	}

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode session response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// GenerateRandomString generates a random alphanumeric string of specified length
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

	// Initialize random seed
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

// generateSessionID generates a unique session ID
func (h *Handler) generateSessionID() string {
	timestamp := time.Now().UnixMilli()
	randomStr := GenerateRandomString(10)
	return fmt.Sprintf("session_%d_%s", timestamp, randomStr)
}
