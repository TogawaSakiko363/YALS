package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/validator"

	"github.com/gorilla/websocket"
)

// Handler handles HTTP and WebSocket requests
type Handler struct {
	agentManager   *agent.Manager
	upgrader       websocket.Upgrader
	clients        map[*websocket.Conn]bool
	clientsLock    sync.RWMutex
	pingInterval   time.Duration
	pongWait       time.Duration
	activeCommands map[string]chan bool // 用于停止命令的通道
	commandsLock   sync.RWMutex
	webDir         string // 前端文件目录
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
	Type   string                   `json:"type"`
	Groups []map[string]interface{} `json:"groups"`
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
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins in this example
			},
		},
		clients:        make(map[*websocket.Conn]bool),
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
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	// Register client
	h.clientsLock.Lock()
	h.clients[conn] = true
	h.clientsLock.Unlock()

	// Set up connection handling
	conn.SetReadLimit(512) // Limit size of incoming messages
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
	go h.readPump(conn)
}

// handleAgentWebSocket handles WebSocket connections from agents
func (h *Handler) handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check password
	password := r.Header.Get("X-Agent-Password")
	cfg := config.GetConfig()
	if cfg == nil || password != cfg.Server.Password {
		realIP := h.getRealIP(r)
		log.Printf("Unauthorized agent connection attempt from %s", realIP)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade agent connection: %v", err)
		return
	}

	// Set up connection handling for agents (similar to web clients)
	conn.SetReadLimit(1024) // Limit size of incoming messages
	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	realIP := h.getRealIP(r)
	log.Printf("Agent connected from %s", realIP)

	// Start ping routine for agent (keep connection alive)
	go h.pingAgent(conn)

	// Handle agent connection
	h.agentManager.HandleAgentConnection(conn)
}

// getRealIP 获取客户端真实IP地址，支持反向代理
func (h *Handler) getRealIP(r *http.Request) string {
	// 优先从 X-Real-IP 获取
	ip := r.Header.Get("X-Real-IP")
	if ip != "" {
		return ip
	}

	// 其次从 X-Forwarded-For 获取（取第一个IP）
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// X-Forwarded-For 可能包含多个IP，取第一个
		if idx := strings.Index(forwarded, ","); idx != -1 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}

	// 最后使用 RemoteAddr
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
		log.Printf("Agent ping routine stopped, connection closed")
	}()

	for range ticker.C {
		if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
			log.Printf("Failed to send ping to agent: %v", err)
			return
		}
	}
}

// readPump handles incoming messages from the client
func (h *Handler) readPump(conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var req CommandRequest
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("Failed to parse command request: %v", err)
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
			go h.handleCommand(conn, req)
		case "stop_command":
			h.handleStopCommand(req)
		default:
			log.Printf("Unknown message type: %s", req.Type)
		}
	}
}

// handleCommand handles a command request
func (h *Handler) handleCommand(conn *websocket.Conn, req CommandRequest) {
	resp := h.createCommandResponse(req, false)

	// Validate input
	inputType := validator.ValidateInput(req.Target)
	if inputType == validator.InvalidInput {
		resp.Error = "Invalid target: must be an IP address or domain name"
		h.sendResponse(conn, resp)
		return
	}

	// Get agent
	agents := h.agentManager.GetAgents()
	var agentCommands []string

	for _, a := range agents {
		if a["name"] == req.Agent {
			// 检查代理状态：前端格式中1表示在线，0表示离线
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

	// Sanitize command
	cmd, ok := validator.SanitizeCommand(req.Command, req.Target, agentCommands)
	if !ok {
		resp.Error = "Invalid command"
		h.sendResponse(conn, resp)
		return
	}

	// 创建停止通道
	commandID := h.generateCommandID(req.Command, req.Target, req.Agent)
	stopChan := make(chan bool, 1)

	// 记录执行命令的日志
	log.Printf("Sent run signal for command: %s", commandID)

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
			resp.Success = true
			resp.Output = "" // Final message with empty output to signal completion
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

	// 清理停止通道
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
		log.Printf("Agent name is required for get_agent_commands request")
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

	response := map[string]interface{}{
		"type":  "agent_stats",
		"stats": stats,
	}

	h.sendJSONResponse(conn, response, "agent stats")
}

// handleGetConfig handles the get_config request
func (h *Handler) handleGetConfig(conn *websocket.Conn) {
	cfg := config.GetConfig()
	if cfg == nil {
		log.Printf("Configuration not available")
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
		log.Printf("Failed to send app config: %v", err)
	}
}

// sendJSONResponse sends a JSON response to the client
func (h *Handler) sendJSONResponse(conn *websocket.Conn, response interface{}, responseType string) {
	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal %s: %v", responseType, err)
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
		log.Printf("Failed to marshal streaming response: %v", err)
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
	update := map[string]interface{}{
		"type":   "agent_status",
		"groups": groups,
	}

	// 直接发送，不需要客户端锁定检查（用于初始连接）
	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal agent status: %v", err)
		return
	}

	conn.WriteMessage(websocket.TextMessage, data)
}

// handleStopCommand handles a stop command request
func (h *Handler) handleStopCommand(req CommandRequest) {
	if req.CommandID == "" {
		log.Printf("Stop command request missing command_id")
		return
	}

	if h.stopActiveCommand(req.CommandID) {
		log.Printf("Sent stop signal for command: %s", req.CommandID)
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
		log.Printf("Failed to marshal %s: %v", messageType, err)
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
		close(stopChan) // 发送停止信号
		delete(h.activeCommands, commandID)
		return true
	}
	return false
}
