package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/utils"
	"YALS/internal/validator"
)

// CommandRequest represents a command request from the client
type CommandRequest struct {
	Type      string `json:"type"`
	Agent     string `json:"agent,omitempty"`
	Command   string `json:"command,omitempty"`
	Target    string `json:"target,omitempty"`
	CommandID string `json:"command_id,omitempty"`
	IPVersion string `json:"ip_version,omitempty"`
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

// SessionResponse represents the response for session creation
type SessionResponse struct {
	SessionID string `json:"session_id"`
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

// ExecRequest represents command execution request
type ExecRequest struct {
	Agent     string `json:"agent"`
	Command   string `json:"command"`
	Target    string `json:"target"`
	IPVersion string `json:"ip_version"`
}

// StopRequest represents command stop request
type StopRequest struct {
	CommandID string `json:"command_id"`
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

// getCommandConfig gets the command configuration from the agent manager
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

// handleGetSession handles the session creation API
func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := h.generateSessionID()

	response := SessionResponse{
		SessionID: sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode session response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// setNoCacheHeaders sets headers to prevent CDN caching for API responses
func (h *Handler) setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
}

// GenerateRandomString generates a random alphanumeric string of specified length
func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

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
	if !strings.HasPrefix(sessionID, "session_") {
		return false
	}
	return true
}

// handleGetNodes handles GET /api/node - returns node list and status
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
	groups := h.agentManager.GetAgentGroups()

	response := NodesResponse{
		Version:      utils.GetAppVersion(),
		TotalNodes:   stats["total"].(int),
		OnlineNodes:  stats["online"].(int),
		OfflineNodes: stats["offline"].(int),
		Groups:       groups,
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode nodes response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
