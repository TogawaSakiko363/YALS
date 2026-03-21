package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/handler"
	"YALS/internal/logger"
	"YALS/internal/plugin"
	serverstore "YALS/internal/store/server"
	"YALS/internal/tls"
	"YALS/internal/utils"

	_ "YALS/internal/plugin/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	configFile := flag.String("c", "config.yaml", "Path to configuration file")
	webDir := flag.String("w", "./web", "Path to web frontend directory")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		plugins := plugin.GetRegisteredPlugins()
		fmt.Printf("%s Server\n%s\n", utils.GetAppName(), utils.GetVersionInfo(plugins))
		os.Exit(0)
	}

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	setupLogging(cfg.Server.LogLevel)

	store, err := serverstore.NewStore(cfg.Database.Path)
	if err != nil {
		logger.Fatalf("Failed to initialize SQLite store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warnf("Failed to close SQLite store: %v", err)
		}
	}()

	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		logger.Warnf("Web directory '%s' does not exist", *webDir)
	} else {
		logger.Infof("Using web directory: %s", *webDir)
	}

	agentManager := agent.NewManager()
	seedStoredAgents(agentManager, store, cfg)

	if cfg.Connection.KeepAlive > 0 {
		maxOfflineDuration := time.Duration(cfg.Connection.KeepAlive) * time.Second
		logger.Infof("Offline agent cleanup enabled: delete after %v offline", maxOfflineDuration)
		go func() {
			checkInterval := time.Duration(cfg.Connection.KeepAlive) * time.Second
			if checkInterval < time.Minute {
				checkInterval = time.Minute
			}

			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()
			for range ticker.C {
				cleaned := agentManager.CleanupOfflineAgents(maxOfflineDuration)
				if cleaned > 0 {
					logger.Infof("Cleaned up %d offline agents (offline > %v)", cleaned, maxOfflineDuration)
				}
			}
		}()
	} else {
		logger.Infof("Offline agent cleanup disabled")
	}

	pingInterval := time.Duration(cfg.GRPC.PingInterval) * time.Second
	pongWait := time.Duration(cfg.GRPC.PongWait) * time.Second
	h := handler.NewHandler(agentManager, store, pingInterval, pongWait)

	certExists := fileExists(cfg.Server.TLSCertFile) && fileExists(cfg.Server.TLSKeyFile)
	if certExists {
		logger.Infof("Found existing certificates at %s and %s, using them", cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
	} else {
		logger.Warnf("No certificates found at %s and %s, generating temporary self-signed certificate", cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		if err := tls.GenerateSelfSignedCert(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile, "YALS_INSECURE"); err != nil {
			logger.Fatalf("Failed to generate TLS certificates: %v", err)
		}
		logger.Warnf("Generated temporary self-signed certificate with SNI=YALS_INSECURE")
		logger.Warnf("For production use, please provide valid TLS certificates")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(cfg.GRPC.PingInterval) * time.Second,
			Timeout: time.Duration(cfg.GRPC.PongWait) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	h.RegisterGRPCServer(grpcServer)
	mux := http.NewServeMux()
	h.SetupRoutes(mux, *webDir)

	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				grpcServer.ServeHTTP(w, r)
			} else {
				mux.ServeHTTP(w, r)
			}
		}),
	}

	go func() {
		logger.Infof("Starting unified HTTPS server (gRPC + HTTP) on %s", addr)
		if err := server.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to start HTTPS server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	logger.Info("Shutting down server...")

	grpcServer.GracefulStop()
	_ = server.Shutdown(context.Background())
}

func setupLogging(level string) {
	logger.SetGlobalLevelFromString(level)
	logger.Debugf("Logging level set to: %s", level)
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func seedStoredAgents(agentManager *agent.Manager, store *serverstore.Store, cfg *config.Config) {
	records, err := store.ListAgents()
	if err != nil {
		logger.Warnf("Failed to preload stored agents: %v", err)
		return
	}

	for _, record := range records {
		runtimeConfig := serverstore.BuildRuntimeConfig(cfg.Server.Host, cfg.Server.Port, record, cfg.Server.LogLevel)
		agentManager.RegisterAgent(agent.AgentRegistration{
			UUID:     record.UUID,
			Name:     record.Name,
			Group:    record.Group,
			Details:  record.Details,
			Commands: runtimeConfig.GetAvailableCommands(),
		}, nil)
	}

	logger.Infof("Preloaded %d stored agent definitions", len(records))
}
