package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/probe"
	"YALS/internal/proto"
	serverstore "YALS/internal/store/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Handler handles HTTP requests and implements gRPC service
type Handler struct {
	agentManager    *agent.Manager
	clients         map[*interface{}]bool
	clientIPs       map[*interface{}]string
	clientSessions  map[*interface{}]string
	sessionConns    map[string]*interface{}
	commandSessions map[string]string
	clientsLock     sync.RWMutex
	activeCommands  map[string]chan bool
	commandsLock    sync.RWMutex
	webDir          string
	rateLimiter     *RateLimiter
	store           *serverstore.Store
	controlSessions sync.Map
	runtimeMu       sync.RWMutex
	runtimeSettings config.RuntimeSettings

	// Latency-probe state. targets.yaml is the source of truth; the loaded set and
	// interval are pushed to agents and used to render the Probes/Monitoring APIs.
	probeMu       sync.RWMutex
	probeTargets  []probe.Target
	probeInterval int
	probePath     string
	probeModTime  time.Time
}

// NewHandler creates a new handler
func NewHandler(agentManager *agent.Manager, store *serverstore.Store, runtimeSettings config.RuntimeSettings) *Handler {
	rateLimiter := NewRateLimiter(runtimeSettings)

	return &Handler{
		agentManager:    agentManager,
		clients:         make(map[*interface{}]bool),
		clientIPs:       make(map[*interface{}]string),
		clientSessions:  make(map[*interface{}]string),
		sessionConns:    make(map[string]*interface{}),
		commandSessions: make(map[string]string),
		activeCommands:  make(map[string]chan bool),
		rateLimiter:     rateLimiter,
		store:           store,
		runtimeSettings: runtimeSettings,
	}
}

// GetRuntimeSettings returns current hot runtime settings.
func (h *Handler) GetRuntimeSettings() config.RuntimeSettings {
	h.runtimeMu.RLock()
	defer h.runtimeMu.RUnlock()
	return h.runtimeSettings
}

// UpdateRuntimeSettings replaces runtime settings and updates dependent components.
//
// Only the rate limiter is reconfigured live. The gRPC keepalive parameters
// (settings.GRPC) are baked into the grpc.Server at process start (see
// newGRPCServer) and cannot be changed on a running server, so they are
// persisted here but only take effect on the next restart. The control UI
// surfaces this distinction to the operator.
func (h *Handler) UpdateRuntimeSettings(settings config.RuntimeSettings) {
	config.NormalizeRuntimeSettings(&settings)
	h.runtimeMu.Lock()
	h.runtimeSettings = settings
	h.runtimeMu.Unlock()
	h.rateLimiter.Update(settings)
}

// Handshake implements the gRPC Handshake method
func (h *Handler) Handshake(ctx context.Context, req *proto.HandshakeRequest) (*proto.HandshakeResponse, error) {
	if req == nil || req.UUID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "missing agent uuid")
	}
	if strings.TrimSpace(req.Token) == "" {
		return nil, status.Errorf(codes.InvalidArgument, "missing agent token")
	}

	record, err := h.store.GetAgentByUUID(req.UUID)
	if err != nil {
		logger.Warnf("Unauthorized agent connection attempt for uuid: %s", req.UUID)
		return nil, status.Errorf(codes.Unauthenticated, "unknown agent uuid")
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(record.Token)), []byte(strings.TrimSpace(req.Token))) != 1 {
		logger.Warnf("Invalid token for agent uuid: %s", req.UUID)
		return nil, status.Errorf(codes.Unauthenticated, "invalid agent token")
	}

	bootstrapCfg := config.GetConfig()
	runtimeConfig := serverstore.BuildRuntimeConfig(bootstrapCfg.Server.Host, bootstrapCfg.Server.Port, *record, bootstrapCfg.Server.LogLevel)
	configJSON, err := json.Marshal(runtimeConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode agent config")
	}

	h.agentManager.RegisterAgent(agent.AgentRegistration{
		UUID:     record.UUID,
		Name:     record.Name,
		Group:    record.Group,
		Details:  record.Details,
		Commands: runtimeConfig.GetAvailableCommands(),
	}, nil)

	logger.Infof("Agent handshake received: %s (%s)", record.Name, record.UUID)
	return &proto.HandshakeResponse{
		Success: true,
		Message: "Agent registered successfully",
		Config:  configJSON,
	}, nil
}

// StreamCommands implements the gRPC StreamCommands method
func (h *Handler) StreamCommands(stream proto.AgentService_StreamCommandsServer) error {
	ctx := stream.Context()
	md, _ := metadata.FromIncomingContext(ctx)
	uuids := md.Get("agent-uuid")
	if len(uuids) == 0 || uuids[0] == "" {
		return status.Errorf(codes.InvalidArgument, "missing agent uuid")
	}
	if err := proto.ValidateToken(ctx, h.lookupAgentToken(uuids[0])); err != nil {
		return err
	}

	uuidValue := uuids[0]
	agentInfo, err := h.agentManager.RegisterAgentStream(uuidValue, stream)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	defer h.agentManager.UnregisterAgentStream(uuidValue)

	logger.Infof("Agent stream connected: %s (%s)", agentInfo.Name, uuidValue)
	h.pushProbeConfigToAgent(uuidValue)
	return h.agentManager.HandleAgentConnection(uuidValue, stream)
}

func (h *Handler) lookupAgentToken(uuidValue string) string {
	record, err := h.store.GetAgentByUUID(uuidValue)
	if err != nil {
		return ""
	}
	return record.Token
}

// SetupRoutes sets up the HTTP routes
func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/version", h.handleVersion)
	mux.HandleFunc("/api/node", h.handleGetNodes)
	mux.HandleFunc("/api/exec", h.handleExecCommand)
	mux.HandleFunc("/api/stop", h.handleStopCommand)
	mux.HandleFunc("/api/control/login", h.handleControlLogin)
	mux.HandleFunc("/api/control/session", h.handleControlSession)
	mux.HandleFunc("/api/control/agents", h.handleControlAgents)
	mux.HandleFunc("/api/control/agents/", h.handleControlAgentByUUID)
	mux.HandleFunc("/api/control/runtime", h.handleControlRuntime)
	mux.HandleFunc("/api/control/plugins", h.handleControlPlugins)
	mux.HandleFunc("/api/control/targets", h.handleControlTargets)
	mux.HandleFunc("/api/status", h.handleStatus)
	mux.HandleFunc("/api/probes", h.handleProbes)
	mux.HandleFunc("/api/probes/series", h.handleProbesSeries)
	mux.HandleFunc("/api/probes/meta", h.handleProbesMeta)

	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

// RegisterGRPCServer registers the gRPC service
func (h *Handler) RegisterGRPCServer(grpcServer *grpc.Server) {
	proto.RegisterAgentServiceServer(grpcServer, h)
}
