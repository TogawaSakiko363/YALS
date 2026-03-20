package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/proto"

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
	}
}

// Handshake implements the gRPC Handshake method
func (h *Handler) Handshake(ctx context.Context, req *proto.HandshakeRequest) (*proto.HandshakeResponse, error) {
	cfg := config.GetConfig()
	if err := proto.ValidateToken(ctx, cfg.Server.Password); err != nil {
		logger.Warnf("Unauthorized agent connection attempt")
		return nil, err
	}

	logger.Infof("Agent handshake received: %s (Group: %s)", req.Name, req.Group)

	h.agentManager.RegisterAgent(req.Name, req.Group, req.Details, req.Commands, nil)

	return &proto.HandshakeResponse{
		Success: true,
		Message: "Agent registered successfully",
	}, nil
}

// StreamCommands implements the gRPC StreamCommands method
func (h *Handler) StreamCommands(stream proto.AgentService_StreamCommandsServer) error {
	ctx := stream.Context()
	cfg := config.GetConfig()
	if err := proto.ValidateToken(ctx, cfg.Server.Password); err != nil {
		logger.Warnf("Unauthorized agent stream attempt")
		return err
	}

	md, _ := metadata.FromIncomingContext(ctx)
	names := md.Get("agent-name")

	if len(names) == 0 {
		return status.Errorf(codes.InvalidArgument, "missing agent name")
	}

	agentName := names[0]

	h.agentManager.RegisterAgentStream(agentName, "", stream)
	defer h.agentManager.UnregisterAgentStream(agentName)

	logger.Infof("Agent stream connected: %s", agentName)

	return h.agentManager.HandleAgentConnection(stream)
}

// SetupRoutes sets up the HTTP routes
func (h *Handler) SetupRoutes(mux *http.ServeMux, webDir string) {
	h.webDir = webDir

	mux.HandleFunc("/", h.handleIndex)
	mux.HandleFunc("/api/session", h.handleGetSession)
	mux.HandleFunc("/api/node", h.handleGetNodes)
	mux.HandleFunc("/api/exec", h.handleExecCommand)
	mux.HandleFunc("/api/stop", h.handleStopCommand)

	fs := http.FileServer(http.Dir(webDir))
	mux.Handle("/assets/", fs)
}

// RegisterGRPCServer registers the gRPC service
func (h *Handler) RegisterGRPCServer(grpcServer *grpc.Server) {
	proto.RegisterAgentServiceServer(grpcServer, h)
}
