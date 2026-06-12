package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/plugin"
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
	case "/", "/index", "/index.html",
		"/control", "/control/", "/control.html",
		"/status", "/status/", "/status.html",
		"/probes", "/probes/", "/probes.html":
		// Single-page app: every client-side route is served the same
		// index.html, which dispatches on window.location.pathname.
		http.ServeFile(w, r, filepath.Join(h.webDir, "index.html"))
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

// getRealIP returns the client IP. Proxy headers (X-Real-IP / X-Forwarded-For)
// are only honored when the operator has explicitly enabled trust_proxy_headers,
// because otherwise any client can spoof them to forge logs or bypass per-IP
// rate limiting. Without that flag we fall back to the connection's RemoteAddr.
func (h *Handler) getRealIP(r *http.Request) string {
	if cfg := config.GetConfig(); cfg != nil && cfg.Server.TrustProxyHeaders {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
		forwarded := r.Header.Get("X-Forwarded-For")
		if forwarded != "" {
			if idx := strings.Index(forwarded, ","); idx != -1 {
				return strings.TrimSpace(forwarded[:idx])
			}
			return strings.TrimSpace(forwarded)
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// generateCommandID builds the stable identifier for one command execution. The
// per-client sessionID is part of the key on purpose: the same command+target on
// the same agent, issued from different clients (browser tabs), must map to
// distinct ids. That is what lets /api/stop abort exactly the one running
// execution the caller means, instead of every client's matching command — the
// problem that motivated introducing the session id in the first place.
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

func (h *Handler) setNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
}

// GenerateRandomString returns a cryptographically secure random string of the
// given length using the charset [a-z0-9]. Rejection sampling is used so the
// resulting distribution is uniform (no modulo bias).
func GenerateRandomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	// 252 is the largest multiple of len(charset) (36) that fits in a byte;
	// bytes >= 252 are rejected to keep the distribution uniform.
	const maxByte = 252
	result := make([]byte, length)
	buf := make([]byte, 1)
	for i := 0; i < length; {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("generate random string: %w", err)
		}
		if buf[0] >= maxByte {
			continue
		}
		result[i] = charset[buf[0]%byte(len(charset))]
		i++
	}
	return string(result), nil
}

// sessionIDPattern bounds the client-generated session id to a safe shape. The
// session id is purely a client-side correlation token (it is woven into command
// ids and server logs), so the server validates its shape — length-bounded and
// restricted to an unambiguous charset — rather than trusting it blindly. It is
// no longer issued by the server; clients generate it themselves.
var sessionIDPattern = regexp.MustCompile(`^session_[A-Za-z0-9_-]{8,128}$`)

func (h *Handler) validateSessionID(sessionID string) bool {
	return sessionIDPattern.MatchString(sessionID)
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

// handleVersion exposes the application version as public, unauthenticated build
// info so every page's shared footer can render it without a session.
func (h *Handler) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(map[string]string{"version": utils.GetAppVersion()})
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
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(cfg.Server.Password)) != 1 {
		http.Error(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token, err := GenerateRandomString(32)
	if err != nil {
		logger.Errorf("Failed to generate control token: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
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

// PluginInfo describes a built-in agent plugin and whether it forces (overrides)
// the ignore_target / maximum_queue settings, so the control UI can present
// those fields correctly instead of letting the operator set values the agent
// would silently ignore.
type PluginInfo struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	IgnoreTarget           bool   `json:"ignore_target"`
	IgnoreTargetOverridden bool   `json:"ignore_target_overridden"`
	MaximumQueue           int    `json:"maximum_queue"`
	MaximumQueueOverridden bool   `json:"maximum_queue_overridden"`
}

func (h *Handler) handleControlPlugins(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	names := plugin.GetManager().ListPlugins()
	sort.Strings(names)

	infos := make([]PluginInfo, 0, len(names))
	for _, name := range names {
		ignoreOverridden, ignoreTarget := plugin.GetPluginIgnoreTarget(name)
		queueOverridden, maximumQueue := plugin.GetPluginMaximumQueue(name)
		infos = append(infos, PluginInfo{
			Name:                   name,
			Description:            plugin.GetPluginDescription(name),
			IgnoreTarget:           ignoreTarget,
			IgnoreTargetOverridden: ignoreOverridden,
			MaximumQueue:           maximumQueue,
			MaximumQueueOverridden: queueOverridden,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(infos)
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

// handleControlAgentsOrder persists a drag-reordered agent list. Body:
// {"order": ["uuid1", "uuid2", ...]}. Registered as an exact path so it takes
// precedence over the /api/control/agents/ subtree handler.
func (h *Handler) handleControlAgentsOrder(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Order []string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.store.UpdateAgentOrder(payload.Order); err != nil {
		logger.Errorf("Failed to update agent order: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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

// validateAgentPayload checks an agent create/update payload before it reaches
// the store, so the operator gets a precise error instead of the store silently
// normalizing away empty or duplicate commands (which could otherwise persist an
// agent with zero usable commands, or a command that references a plugin that
// does not exist).
func validateAgentPayload(payload AgentConfigPayload) error {
	if strings.TrimSpace(payload.Name) == "" {
		return fmt.Errorf("agent name is required")
	}
	if len(payload.Commands) == 0 {
		return fmt.Errorf("at least one command is required")
	}

	seen := make(map[string]bool, len(payload.Commands))
	for i, cmd := range payload.Commands {
		name := strings.TrimSpace(cmd.Name)
		if name == "" {
			return fmt.Errorf("command #%d: name is required", i+1)
		}
		if seen[name] {
			return fmt.Errorf("duplicate command name: %q", name)
		}
		seen[name] = true

		template := strings.TrimSpace(cmd.Template)
		usePlugin := strings.TrimSpace(cmd.UsePlugin)
		if template == "" && usePlugin == "" {
			return fmt.Errorf("command %q: a template or a plugin is required", name)
		}
		if usePlugin != "" {
			if _, ok := plugin.GetManager().GetPlugin(usePlugin); !ok {
				return fmt.Errorf("command %q: unknown plugin %q", name, usePlugin)
			}
		}
	}
	return nil
}

func (h *Handler) handleControlCreateAgent(w http.ResponseWriter, r *http.Request) {
	var payload AgentConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := validateAgentPayload(payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	payload.UUID = uuid.NewString()
	if payload.Token == "" {
		generatedToken, err := GenerateRandomString(32)
		if err != nil {
			logger.Errorf("Failed to generate agent token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		payload.Token = generatedToken
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

	if err := validateAgentPayload(payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	_ = h.store.DeleteAgentMetrics(uuidValue)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (h *Handler) getControlToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	// Tokens are only accepted via the Authorization header. Accepting them from
	// the URL query string would leak them into access logs, proxy logs, browser
	// history and Referer headers.
	return ""
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
