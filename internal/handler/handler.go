package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"YALS_SSH/internal/agent"
	"YALS_SSH/internal/config"
	"YALS_SSH/internal/validator"

	"github.com/gorilla/websocket"
)

type Handler struct {
	agentManager *agent.Manager
	upgrader     websocket.Upgrader
	clients      map[*websocket.Conn]bool
	clientsLock  sync.RWMutex
	pingInterval time.Duration
	pongWait     time.Duration
}

type CommandRequest struct {
	Type    string `json:"type"`
	Agent   string `json:"agent,omitempty"`
	Command string `json:"command,omitempty"`
	Target  string `json:"target,omitempty"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Target  string `json:"target"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

type AgentStatusUpdate struct {
	Type   string                   `json:"type"`
	Groups []map[string]interface{} `json:"groups"`
}

type CommandsListResponse struct {
	Type     string                    `json:"type"`
	Commands []validator.CommandDetail `json:"commands"`
}

type AppConfigResponse struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Config  map[string]string `json:"config"`
}

func NewHandler(agentManager *agent.Manager, pingInterval, pongWait time.Duration) *Handler {
	return &Handler{
		agentManager: agentManager,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		clients:      make(map[*websocket.Conn]bool),
		pingInterval: pingInterval,
		pongWait:     pongWait,
	}
}

func (h *Handler) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/ws", h.handleWebSocket)

	fs := http.FileServer(http.Dir("./web"))
	mux.Handle("/assets/", fs)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		http.ServeFile(w, r, "./web/index.html")
		return
	default:
		if _, err := http.Dir("./web").Open(r.URL.Path[1:]); err == nil {
			http.ServeFile(w, r, "./web/"+r.URL.Path[1:])
			return
		}

		if r.Header.Get("Accept") != "" && !strings.Contains(r.Header.Get("Accept"), "application/json") {
			http.ServeFile(w, r, "./web/index.html")
			return
		}

		http.NotFound(w, r)
		return
	}
}

// handleWebSocket handles WebSocket connections
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
		case "get_config":
			h.handleGetConfig(conn)
		case "execute_command":
			go h.handleCommand(conn, req)
		default:
			log.Printf("Unknown message type: %s", req.Type)
		}
	}
}

// handleCommand handles a command request
func (h *Handler) handleCommand(conn *websocket.Conn, req CommandRequest) {
	resp := CommandResponse{
		Success: false,
		Agent:   req.Agent,
		Command: req.Command,
		Target:  req.Target,
	}

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
			if a["status"].(agent.Status) != agent.StatusConnected {
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

	// Execute command
	output, err := h.agentManager.ExecuteCommand(req.Agent, cmd)
	if err != nil {
		resp.Error = err.Error()
		h.sendResponse(conn, resp)
		return
	}

	resp.Success = true
	resp.Output = output
	h.sendResponse(conn, resp)
}

// handleGetCommands handles the get commands request
func (h *Handler) handleGetCommands(conn *websocket.Conn) {
	commands := validator.GetAvailableCommands()
	response := CommandsListResponse{
		Type:     "commands_list",
		Commands: commands,
	}

	data, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal commands response: %v", err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	if _, ok := h.clients[conn]; ok {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (h *Handler) handleGetConfig(conn *websocket.Conn) {
	cfg := config.GetConfig()
	if cfg == nil {
		log.Printf("Configuration not available")
		return
	}

	response := AppConfigResponse{
		Type:    "app_config",
		Version: cfg.App.Version,
		Config: map[string]string{
			"server_host": cfg.Server.Host,
			"server_port": fmt.Sprintf("%d", cfg.Server.Port),
			"log_level":   cfg.Server.LogLevel,
		},
	}

	if err := conn.WriteJSON(response); err != nil {
		log.Printf("Failed to send app config: %v", err)
	}
}

// sendResponse sends a response to the client
func (h *Handler) sendResponse(conn *websocket.Conn, resp CommandResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Failed to marshal response: %v", err)
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

	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal agent status: %v", err)
		return
	}

	conn.WriteMessage(websocket.TextMessage, data)
}

// BroadcastAgentStatus broadcasts agent status to all clients
func (h *Handler) BroadcastAgentStatus() {
	groups := h.agentManager.GetAgentGroups()
	update := AgentStatusUpdate{
		Type:   "agent_status",
		Groups: groups,
	}

	data, err := json.Marshal(update)
	if err != nil {
		log.Printf("Failed to marshal agent status: %v", err)
		return
	}

	h.clientsLock.RLock()
	defer h.clientsLock.RUnlock()

	for conn := range h.clients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}
