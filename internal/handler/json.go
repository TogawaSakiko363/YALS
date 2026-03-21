package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	serverstore "YALS/internal/store/server"
	"YALS/internal/utils"
	"YALS/internal/validator"

	"github.com/google/uuid"
)

type CommandRequest struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
	IPVersion string `json:"ip_version,omitempty"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Agent   string `json:"agent"`
	Command string `json:"command"`
	Target  string `json:"target"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

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

type AgentStatusUpdate struct {
	Type   string           `json:"type"`
	Groups []map[string]any `json:"groups"`
}

type CommandsListResponse struct {
	Type     string                    `json:"type"`
	Commands []validator.CommandDetail `json:"commands"`
}

type AppVersion struct {
	Type    string            `json:"type"`
	Version string            `json:"version"`
	Config  map[string]string `json:"config"`
}

type SessionResponse struct {
	SessionID string `json:"session_id"`
}

type NodesResponse struct {
	Version      string           `json:"version"`
	TotalNodes   int              `json:"total_nodes"`
	OnlineNodes  int              `json:"online_nodes"`
	OfflineNodes int              `json:"offline_nodes"`
	Groups       []map[string]any `json:"groups"`
}

type ExecRequest struct {
	Agent     string `json:"agent"`
	Command   string `json:"command"`
	Target    string `json:"target"`
	IPVersion string `json:"ip_version"`
}

type StopRequest struct {
	CommandID string `json:"command_id"`
}

type ControlLoginRequest struct {
	Password string `json:"password"`
}

type ControlSessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Token         string `json:"token,omitempty"`
}

type RuntimeSettingsResponse struct {
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

type RuntimeSettingsPayload struct {
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

type AgentConfigPayload struct {
	UUID     string                      `json:"uuid,omitempty"`
	Token    string                      `json:"token"`
	Name     string                      `json:"name"`
	Group    string                      `json:"group"`
	Details  config.AgentDetails         `json:"details"`
	Commands []serverstore.CommandRecord `json:"commands"`
}

type AgentConfigResponse struct {
	UUID      string                      `json:"uuid"`
	Token     string                      `json:"token"`
	Name      string                      `json:"name"`
	Group     string                      `json:"group"`
	Details   config.AgentDetails         `json:"details"`
	Commands  []serverstore.CommandRecord `json:"commands"`
	CreatedAt string                      `json:"created_at"`
	UpdatedAt string                      `json:"updated_at"`
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/", "/index", "/index.html":
		http.ServeFile(w, r, filepath.Join(h.webDir, "index.html"))
		return
	case "/control", "/control/", "/control.html":
		http.ServeFile(w, r, filepath.Join(h.webDir, "control.html"))
		return
	default:
		filePath := filepath.Join(h.webDir, r.URL.Path[1:])
		if _, err := http.Dir(h.webDir).Open(r.URL.Path[1:]); err == nil {
			http.ServeFile(w, r, filePath)
			return
		}

		accept := r.Header.Get("Accept")
		if accept != "" && !strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/xhtml+xml") {
			http.ServeFile(w, r, filepath.Join(h.webDir, "index.html"))
			return
		}

		http.NotFound(w, r)
	}
}

func (h *Handler) getRealIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
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

func (h *Handler) generateCommandID(command, target, agentName, sessionID string) string {
	return fmt.Sprintf("%s-%s-%s-%s", command, target, agentName, sessionID)
}

func (h *Handler) setActiveCommand(commandID string, stopChan chan bool) {
	h.commandsLock.Lock()
	h.activeCommands[commandID] = stopChan
	h.commandsLock.Unlock()
}

func (h *Handler) removeActiveCommand(commandID string) {
	h.commandsLock.Lock()
	delete(h.activeCommands, commandID)
	h.commandsLock.Unlock()
}

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

func (h *Handler) getCommandConfig(agentName, commandName string) (config.CommandInfo, bool) {
	agents := h.agentManager.GetAgents()
	for _, a := range agents {
		if a["name"] == agentName {
			agentCommands := h.agentManager.GetAgentCommands(agentName)
			for _, cmd := range agentCommands {
				if cmd.Name == commandName {
					return h.agentManager.GetCommandConfigInternal(agentName, commandName)
				}
			}
		}
	}
	return config.CommandInfo{}, false
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := SessionResponse{SessionID: h.generateSessionID()}
	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode session response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
}

func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

func (h *Handler) generateSessionID() string {
	return fmt.Sprintf("session_%d_%s", time.Now().UnixMilli(), GenerateRandomString(10))
}

func (h *Handler) validateSessionID(sessionID string) bool {
	return sessionID != "" && strings.HasPrefix(sessionID, "session_")
}

func (h *Handler) handleGetNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	stats := h.agentManager.GetAgentStats()
	response := NodesResponse{
		Version:      utils.GetAppVersion(),
		TotalNodes:   stats["total"].(int),
		OnlineNodes:  stats["online"].(int),
		OfflineNodes: stats["offline"].(int),
		Groups:       h.agentManager.GetAgentGroups(),
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode nodes response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *Handler) handleControlLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ControlLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg := config.GetConfig()
	if req.Password != cfg.Server.Password {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token := GenerateRandomString(32)
	h.controlSessions.Store(token, time.Now().Add(24*time.Hour))
	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(ControlSessionResponse{Authenticated: true, Token: token})
}

func (h *Handler) handleControlSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := h.getControlToken(r)
	if !h.validateControlToken(token) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(ControlSessionResponse{Authenticated: true, Token: token})
}

func (h *Handler) handleControlRuntime(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings := h.GetRuntimeSettings()
		response := RuntimeSettingsResponse{}
		response.GRPC.PingInterval = settings.GRPC.PingInterval
		response.GRPC.PongWait = settings.GRPC.PongWait
		response.RateLimit.Enabled = settings.RateLimit.Enabled
		response.RateLimit.MaxCommands = settings.RateLimit.MaxCommands
		response.RateLimit.TimeWindow = settings.RateLimit.TimeWindow
		w.Header().Set("Content-Type", "application/json")
		h.setNoCacheHeaders(w)
		_ = json.NewEncoder(w).Encode(response)
	case http.MethodPut:
		var payload RuntimeSettingsPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		settings := config.RuntimeSettings{}
		settings.GRPC.PingInterval = payload.GRPC.PingInterval
		settings.GRPC.PongWait = payload.GRPC.PongWait
		settings.RateLimit.Enabled = payload.RateLimit.Enabled
		settings.RateLimit.MaxCommands = payload.RateLimit.MaxCommands
		settings.RateLimit.TimeWindow = payload.RateLimit.TimeWindow
		saved, err := h.store.UpsertRuntimeSettings(settings)
		if err != nil {
			http.Error(w, "Failed to persist runtime settings", http.StatusInternalServerError)
			return
		}
		h.UpdateRuntimeSettings(*saved)
		response := RuntimeSettingsResponse{}
		response.GRPC.PingInterval = saved.GRPC.PingInterval
		response.GRPC.PongWait = saved.GRPC.PongWait
		response.RateLimit.Enabled = saved.RateLimit.Enabled
		response.RateLimit.MaxCommands = saved.RateLimit.MaxCommands
		response.RateLimit.TimeWindow = saved.RateLimit.TimeWindow
		w.Header().Set("Content-Type", "application/json")
		h.setNoCacheHeaders(w)
		_ = json.NewEncoder(w).Encode(response)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleControlAgents(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleControlListAgents(w)
	case http.MethodPost:
		h.handleControlCreateAgent(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleControlAgentByUUID(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}

	uuidValue := strings.TrimPrefix(r.URL.Path, "/api/control/agents/")
	if uuidValue == "" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.handleControlUpdateAgent(w, r, uuidValue)
	case http.MethodDelete:
		h.handleControlDeleteAgent(w, uuidValue)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleControlListAgents(w http.ResponseWriter) {
	records, err := h.store.ListAgents()
	if err != nil {
		logger.Errorf("Failed to list stored agents: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := make([]AgentConfigResponse, 0, len(records))
	for _, record := range records {
		response = append(response, agentRecordToResponse(record))
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(response)
}

func (h *Handler) handleControlCreateAgent(w http.ResponseWriter, r *http.Request) {
	var payload AgentConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	payload.UUID = uuid.NewString()
	if payload.Token == "" {
		payload.Token = GenerateRandomString(32)
	}
	record, err := h.store.UpsertAgent(serverstore.AgentUpsertInput{
		UUID:     payload.UUID,
		Token:    payload.Token,
		Name:     payload.Name,
		Group:    payload.Group,
		Details:  payload.Details,
		Commands: payload.Commands,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.syncStoredAgent(*record)
	_ = h.agentManager.ReloadAgent(record.UUID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(agentRecordToResponse(*record))
}

func (h *Handler) handleControlUpdateAgent(w http.ResponseWriter, r *http.Request, uuidValue string) {
	var payload AgentConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	record, err := h.store.UpsertAgent(serverstore.AgentUpsertInput{
		UUID:     uuidValue,
		Token:    payload.Token,
		Name:     payload.Name,
		Group:    payload.Group,
		Details:  payload.Details,
		Commands: payload.Commands,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.syncStoredAgent(*record)
	_ = h.agentManager.ReloadAgent(record.UUID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(agentRecordToResponse(*record))
}

func (h *Handler) handleControlDeleteAgent(w http.ResponseWriter, uuidValue string) {
	if err := h.store.DeleteAgent(uuidValue); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	_ = h.agentManager.DisconnectAgent(uuidValue)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (h *Handler) getControlToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (h *Handler) validateControlToken(token string) bool {
	if token == "" {
		return false
	}
	value, ok := h.controlSessions.Load(token)
	if !ok {
		return false
	}
	expiresAt, ok := value.(time.Time)
	if !ok || time.Now().After(expiresAt) {
		h.controlSessions.Delete(token)
		return false
	}
	return true
}

func (h *Handler) requireControlAuth(w http.ResponseWriter, r *http.Request) bool {
	if !h.validateControlToken(h.getControlToken(r)) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (h *Handler) syncStoredAgent(record serverstore.AgentRecord) {
	bootstrapCfg := config.GetConfig()
	runtimeConfig := serverstore.BuildRuntimeConfig(bootstrapCfg.Server.Host, bootstrapCfg.Server.Port, record, bootstrapCfg.Server.LogLevel)
	h.agentManager.RegisterAgent(agent.AgentRegistration{
		UUID:     record.UUID,
		Name:     record.Name,
		Group:    record.Group,
		Details:  record.Details,
		Commands: runtimeConfig.GetAvailableCommands(),
	}, nil)
}

func agentRecordToResponse(record serverstore.AgentRecord) AgentConfigResponse {
	return AgentConfigResponse{
		UUID:      record.UUID,
		Token:     record.Token,
		Name:      record.Name,
		Group:     record.Group,
		Details:   record.Details,
		Commands:  record.Commands,
		CreatedAt: record.CreatedAt.Format(time.RFC3339),
		UpdatedAt: record.UpdatedAt.Format(time.RFC3339),
	}
}
