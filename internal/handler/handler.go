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
	mux.HandleFunc("/api/node", h.handleGetNodes)
	mux.HandleFunc("/api/exec", h.handleExecCommand)
	mux.HandleFunc("/api/stop", h.handleStopCommand)
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

		if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "application/html") {
			indexPath := filepath.Join(h.webDir, "index.html")
			http.ServeFile(w, r, indexPath)
			return
		}

		http.NotFound(w, r)
		return
	}
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

// generateCommandID generates a unique command ID
func (h *Handler) generateCommandID(command, target, agent, sessionID string) string {
	return fmt.Sprintf("%s-%s-%s-%s", command, target, agent, sessionID)
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

// validateSessionID validates if a session ID is valid
func (h *Handler) validateSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	// Basic validation: check format
	if !strings.HasPrefix(sessionID, "session_") {
		return false
	}
	return true
}

// NodeInfo represents node information response
type NodeInfo struct {
	Name     string   `json:"name"`
	Group    string   `json:"group"`
	Status   int      `json:"status"`
	Details  string   `json:"details"`
	Commands []string `json:"commands"`
}

// NodesResponse represents the response for /api/node
type NodesResponse struct {
	Version      string           `json:"version"`
	TotalNodes   int              `json:"total_nodes"`
	OnlineNodes  int              `json:"online_nodes"`
	OfflineNodes int              `json:"offline_nodes"`
	Groups       []map[string]any `json:"groups"`
}

// handleGetNodes handles GET /api/node - returns node list and status
func (h *Handler) handleGetNodes(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate session ID
	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	// Get agent statistics
	stats := h.agentManager.GetAgentStats()
	groups := h.agentManager.GetAgentGroups()

	response := NodesResponse{
		Version:      utils.GetAppVersion(),
		TotalNodes:   stats["total"].(int),
		OnlineNodes:  stats["online"].(int),
		OfflineNodes: stats["offline"].(int),
		Groups:       groups,
	}

	// Set headers to prevent CDN caching
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode nodes response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// ExecRequest represents command execution request
type ExecRequest struct {
	Agent     string `json:"agent"`
	Command   string `json:"command"`
	Target    string `json:"target"`
	IPVersion string `json:"ip_version"`
}

// handleExecCommand handles POST /api/exec - executes command and streams output via SSE
func (h *Handler) handleExecCommand(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate session ID from query parameter
	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	clientIP := h.getRealIP(r)

	// Set SSE headers to prevent CDN caching
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Check rate limit
	if !h.rateLimiter.checkRateLimit(sessionID) {
		remaining := h.rateLimiter.getRemainingTime(sessionID)
		errorMsg := fmt.Sprintf("Rate limit exceeded. Please wait %d seconds before trying again.", int(remaining.Seconds())+1)
		h.sendSSEError(w, flusher, errorMsg)
		logger.Warnf("Client [%s] rate limit exceeded for session: %s", clientIP, sessionID)
		return
	}

	// Validate agent and command
	agents := h.agentManager.GetAgents()
	var agentCommands []string
	var requiresTarget bool = true
	var agentFound bool = false
	var agentOnline bool = false

	// Check if agent exists and is online
	for _, a := range agents {
		if a["name"] == req.Agent {
			agentFound = true
			// Check if agent is online
			if statusVal, ok := a["status"].(int); ok && statusVal == 1 {
				agentOnline = true
			}
			break
		}
	}

	if !agentFound {
		h.sendSSEError(w, flusher, "Agent not found")
		return
	}

	if !agentOnline {
		h.sendSSEError(w, flusher, "Agent is not connected")
		return
	}

	// Get commands from agent manager
	cmdDetails := h.agentManager.GetAgentCommands(req.Agent)
	for _, cmd := range cmdDetails {
		agentCommands = append(agentCommands, cmd.Name)
	}

	if len(agentCommands) == 0 {
		h.sendSSEError(w, flusher, "No commands available for agent")
		return
	}

	// Validate input only if target is required
	if requiresTarget {
		inputType := validator.ValidateInput(req.Target)
		if inputType == validator.InvalidInput {
			h.sendSSEError(w, flusher, "Invalid target: must be an IP address or domain name, and not exceed 256 characters")
			return
		}
	}

	cmd, ok := validator.SanitizeCommand(req.Command, req.Target, agentCommands)
	if !ok {
		h.sendSSEError(w, flusher, "Invalid command")
		return
	}

	// Generate commandID
	commandID := h.generateCommandID(req.Command, req.Target, req.Agent, sessionID)
	stopChan := make(chan bool, 1)

	logger.Infof("Client [%s] executing command: %s", clientIP, commandID)

	h.setActiveCommand(commandID, stopChan)
	defer h.removeActiveCommand(commandID)

	// Execute command with streaming output via SSE
	err := h.agentManager.ExecuteCommandStreamingWithStopAndID(req.Agent, cmd, commandID, req.IPVersion, stopChan, func(output string, isError bool, isComplete bool, isStopped bool) {
		if isStopped {
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":    "output",
				"output":  "\n*** Stopped ***",
				"stopped": true,
			})
			h.sendSSEMessage(w, flusher, map[string]any{
				"type":    "complete",
				"success": false,
				"stopped": true,
			})
		} else if isComplete {
			if isError {
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":    "complete",
					"success": false,
					"error":   output,
				})
			} else {
				// Send final output before completion message
				if output != "" {
					h.sendSSEMessage(w, flusher, map[string]any{
						"type":   "output",
						"output": output,
					})
				}
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":    "complete",
					"success": true,
				})
			}
		} else {
			if isError {
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":  "error",
					"error": output,
				})
			} else {
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":   "output",
					"output": output,
				})
			}
		}
	})

	if err != nil {
		h.sendSSEError(w, flusher, err.Error())
		return
	}
}

// sendSSEMessage sends an SSE message
func (h *Handler) sendSSEMessage(w http.ResponseWriter, flusher http.Flusher, data map[string]any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Errorf("Failed to marshal SSE message: %v", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// sendSSEError sends an SSE error message and completes the stream
func (h *Handler) sendSSEError(w http.ResponseWriter, flusher http.Flusher, errorMsg string) {
	h.sendSSEMessage(w, flusher, map[string]any{
		"type":    "complete",
		"success": false,
		"error":   errorMsg,
	})
}

// StopRequest represents command stop request
type StopRequest struct {
	CommandID string `json:"command_id"`
}

// handleStopCommand handles POST /api/stop - stops a running command
func (h *Handler) handleStopCommand(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate session ID from query parameter
	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.CommandID == "" {
		http.Error(w, "Missing command_id", http.StatusBadRequest)
		return
	}

	clientIP := h.getRealIP(r)

	// Stop the command
	if h.stopActiveCommand(req.CommandID) {
		logger.Infof("Client [%s] sent stop signal for command: %s", clientIP, req.CommandID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"message": "Command stopped",
		})
	} else {
		logger.Warnf("Client [%s] attempted to stop non-existent command: %s", clientIP, req.CommandID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "Command not found or already completed",
		})
	}
}
