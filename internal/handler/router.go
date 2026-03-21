package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
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
	pingInterval    time.Duration
	pongWait        time.Duration
	activeCommands  map[string]chan bool
	commandsLock    sync.RWMutex
	webDir          string
	rateLimiter     *RateLimiter
	store           *serverstore.Store
	controlSessions sync.Map
}

// NewHandler creates a new handler
func NewHandler(agentManager *agent.Manager, store *serverstore.Store, pingInterval, pongWait time.Duration) *Handler {
	cfg := config.GetConfig()

	rateLimiter := &RateLimiter{
		enabled:     cfg.RateLimit.Enabled,
		maxCommands: cfg.RateLimit.MaxCommands,
		timeWindow:  time.Duration(cfg.RateLimit.TimeWindow) * time.Second,
		sessions:    make(map[string]*SessionRateLimit),
	}

	return &Handler{
		agentManager:    agentManager,
		clients:         make(map[*interface{}]bool),
		clientIPs:       make(map[*interface{}]string),
		clientSessions:  make(map[*interface{}]string),
		sessionConns:    make(map[string]*interface{}),
		commandSessions: make(map[string]string),
		pingInterval:    pingInterval,
		pongWait:        pongWait,
		activeCommands:  make(map[string]chan bool),
		rateLimiter:     rateLimiter,
		store:           store,
	}
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
	if strings.TrimSpace(record.Token) != strings.TrimSpace(req.Token) {
		logger.Warnf("Invalid token for agent uuid: %s", req.UUID)
		return nil, status.Errorf(codes.Unauthenticated, "invalid agent token")
	}

	runtimeConfig := serverstore.BuildRuntimeConfig(config.GetConfig().Server.Host, config.GetConfig().Server.Port, *record, config.GetConfig().Server.LogLevel)
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
		return status.Errorf(codes.NotFound, err.Error())
	}
	defer h.agentManager.UnregisterAgentStream(uuidValue)

	logger.Infof("Agent stream connected: %s (%s)", agentInfo.Name, uuidValue)
	return h.agentManager.HandleAgentConnection(stream)
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
	mux.HandleFunc("/api/session", h.handleGetSession)
	mux.HandleFunc("/api/node", h.handleGetNodes)
	mux.HandleFunc("/api/exec", h.handleExecCommand)
	mux.HandleFunc("/api/stop", h.handleStopCommand)
	mux.HandleFunc("/api/control/login", h.handleControlLogin)
	mux.HandleFunc("/api/control/session", h.handleControlSession)
	mux.HandleFunc("/api/control/agents", h.handleControlAgents)
	mux.HandleFunc("/api/control/agents/", h.handleControlAgentByUUID)

	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

// RegisterGRPCServer registers the gRPC service
func (h *Handler) RegisterGRPCServer(grpcServer *grpc.Server) {
	proto.RegisterAgentServiceServer(grpcServer, h)
}
