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
	"YALS/internal/tls"
	"YALS/internal/utils"

	_ "YALS/internal/plugin/server"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

func main() {
	// Parse command line flags
	configFile := flag.String("c", "config.yaml", "Path to configuration file")
	webDir := flag.String("w", "./web", "Path to web frontend directory")
	showVersion := flag.Bool("version", false, "Show version information")

	flag.Parse()

	// Handle version flag
	if *showVersion {
		// Import plugin package to ensure plugins are registered
		plugins := plugin.GetRegisteredPlugins()
		fmt.Printf("%s Server\n%s\n", utils.GetAppName(), utils.GetVersionInfo(plugins))
		os.Exit(0)
	}

	// Load configuration first to get log level
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set up logging with configured level
	setupLogging(cfg.Server.LogLevel)

	// Check if web directory exists
	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		logger.Warnf("Web directory '%s' does not exist", *webDir)
	} else {
		logger.Infof("Using web directory: %s", *webDir)
	}

	// Create agent manager
	agentManager := agent.NewManager()

	// Configure offline agent cleanup (if enabled)
	if cfg.Connection.KeepAlive > 0 {
		maxOfflineDuration := time.Duration(cfg.Connection.KeepAlive) * time.Second
		logger.Infof("Offline agent cleanup enabled: delete after %v offline", maxOfflineDuration)

		// Start periodic cleanup
		go func() {
			checkInterval := time.Duration(cfg.Connection.KeepAlive) * time.Second
			if checkInterval < time.Minute {
				checkInterval = time.Minute // Check at least once per minute
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

	// Create HTTP handler
	pingInterval := time.Duration(cfg.GRPC.PingInterval) * time.Second
	pongWait := time.Duration(cfg.GRPC.PongWait) * time.Second
	h := handler.NewHandler(agentManager, pingInterval, pongWait)

	// Generate or validate TLS certificates
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

	// Set up gRPC server for agent connections
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Configure gRPC server with keepalive
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

	// Register gRPC service
	h.RegisterGRPCServer(grpcServer)

	// Set up HTTP routes
	mux := http.NewServeMux()
	h.SetupRoutes(mux, *webDir)

	// Create unified server that handles both gRPC and HTTP
	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Route gRPC requests to gRPC server
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				grpcServer.ServeHTTP(w, r)
			} else {
				// Route HTTP requests to HTTP handler
				mux.ServeHTTP(w, r)
			}
		}),
	}

	// Start unified server
	go func() {
		logger.Infof("Starting unified HTTPS server (gRPC + HTTP) on %s", addr)

		if err := server.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Failed to start HTTPS server: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-stop
	logger.Info("Shutting down server...")

	// Gracefully stop gRPC server
	grpcServer.GracefulStop()

	// Shutdown HTTP server
	server.Shutdown(context.Background())
}

// setupLogging configures the logging based on the log level
func setupLogging(level string) {
	// Set the global logger level
	logger.SetGlobalLevelFromString(level)

	// Log the configured level for debugging
	logger.Debugf("Logging level set to: %s", level)

	// Also configure the standard log package to use our logger for any legacy log calls
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

// fileExists checks if a file exists and is not a directory
func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
