package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/handler"
	"YALS/internal/logger"
	"YALS/internal/utils"
)

func main() {
	// Parse command line flags
	configFile := flag.String("c", "config.yaml", "Path to configuration file")
	webDir := flag.String("w", "./web", "Path to web frontend directory")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Printf("%s Server\n%s\n", utils.GetAppName(), utils.GetVersionInfo())
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
	pingInterval := time.Duration(cfg.WebSocket.PingInterval) * time.Second
	pongWait := time.Duration(cfg.WebSocket.PongWait) * time.Second
	h := handler.NewHandler(agentManager, pingInterval, pongWait)

	// Set up HTTP server
	mux := http.NewServeMux()
	h.SetupRoutes(mux, *webDir)

	// Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		if cfg.Server.TLS {
			// Check if TLS certificate files exist
			if _, err := os.Stat(cfg.Server.TLSCertFile); os.IsNotExist(err) {
				logger.Fatalf("TLS certificate file not found: %s", cfg.Server.TLSCertFile)
			}
			if _, err := os.Stat(cfg.Server.TLSKeyFile); os.IsNotExist(err) {
				logger.Fatalf("TLS key file not found: %s", cfg.Server.TLSKeyFile)
			}

			logger.Infof("Starting HTTPS server on %s", addr)

			if err := server.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("Failed to start HTTPS server: %v", err)
			}
		} else {
			logger.Infof("Starting HTTP server on %s", addr)
			logger.Warnf("TLS is disabled. Consider enabling TLS for production use.")

			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("Failed to start HTTP server: %v", err)
			}
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-stop
	logger.Info("Shutting down server...")
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
