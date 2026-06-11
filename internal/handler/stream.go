package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"YALS/internal/logger"
	"YALS/internal/plugin"
	"YALS/internal/validator"
)

// handleExecCommand handles POST /api/exec - executes command and streams output via SSE
func (h *Handler) handleExecCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	clientIP := h.getRealIP(r)

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

	// Rate limit on the real client IP rather than the session id: the session id
	// is a client-generated correlation token (not authentication), so a session
	// key would be trivially bypassable.
	if !h.rateLimiter.checkRateLimit(clientIP) {
		remaining := h.rateLimiter.getRemainingTime(clientIP)
		errorMsg := fmt.Sprintf("Rate limit exceeded. Please wait %d seconds before trying again.", int(remaining.Seconds())+1)
		h.sendSSEError(w, flusher, errorMsg)
		logger.Warnf("Client [%s] rate limit exceeded for session: %s", clientIP, sessionID)
		return
	}

	agents := h.agentManager.GetAgents()
	var agentCommands []string
	var requiresTarget bool = true
	var agentFound bool = false
	var agentOnline bool = false

	for _, a := range agents {
		if a["name"] == req.Agent {
			agentFound = true
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

	cmdDetails := h.agentManager.GetAgentCommands(req.Agent)
	for _, cmd := range cmdDetails {
		agentCommands = append(agentCommands, cmd.Name)
	}

	if len(agentCommands) == 0 {
		h.sendSSEError(w, flusher, "No commands available for agent")
		return
	}

	if cmdConfig, exists := h.getCommandConfig(req.Agent, req.Command); exists {
		if cmdConfig.UsePlugin != "" {
			if hasOverride, ignoreTarget := plugin.GetPluginIgnoreTarget(cmdConfig.UsePlugin); hasOverride {
				requiresTarget = !ignoreTarget
			} else if cmdConfig.IgnoreTarget {
				requiresTarget = false
			}
		} else if cmdConfig.IgnoreTarget {
			requiresTarget = false
		}
	}

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

	commandID := h.generateCommandID(req.Command, req.Target, req.Agent, sessionID)
	stopChan := make(chan bool, 1)

	logger.Infof("Client [%s] executing command: %s", clientIP, commandID)

	h.setActiveCommand(commandID, stopChan)
	defer h.removeActiveCommand(commandID)

	err := h.agentManager.ExecuteCommandStreamingWithStopAndID(req.Agent, cmd, commandID, req.IPVersion, stopChan, func(output string, isError bool, isComplete bool, isStopped bool) {
		if isComplete {
			if isError {
				h.sendSSEMessage(w, flusher, map[string]any{
					"type":    "complete",
					"success": false,
					"error":   output,
				})
			} else {
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

// handleStopCommand handles POST /api/stop - stops a running command
func (h *Handler) handleStopCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if !h.validateSessionID(sessionID) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	var req StopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.CommandID == "" {
		http.Error(w, "Missing command_id", http.StatusBadRequest)
		return
	}

	h.stopActiveCommand(req.CommandID)

	response := map[string]any{
		"success": true,
		"message": fmt.Sprintf("Command %s stopped successfully", req.CommandID),
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Errorf("Failed to encode stop response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
