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
)

func main() {
	// Parse command line flags
	configFile := flag.String("c", "config.yaml", "Path to configuration file")
	webDir := flag.String("w", "./web", "Path to web frontend directory")
	flag.Parse()

	// Check if web directory exists
	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		log.Printf("Warning: Web directory '%s' does not exist", *webDir)
	} else {
		log.Printf("Using web directory: %s", *webDir)
	}

	// Load configuration
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set up logging
	setupLogging(cfg.Server.LogLevel)

	// Create agent manager
	agentManager := agent.NewManager()

	// Configure offline agent cleanup (if enabled)
	if cfg.Connection.DeleteOfflineAgents > 0 {
		maxOfflineDuration := time.Duration(cfg.Connection.DeleteOfflineAgents) * time.Second
		log.Printf("Offline agent cleanup enabled: delete after %v offline", maxOfflineDuration)

		// Start periodic cleanup (using keepalive interval, reduced by 10x to save resources)
		go func() {
			checkInterval := time.Duration(cfg.Connection.Keepalive*10) * time.Second
			if checkInterval < time.Minute {
				checkInterval = time.Minute // Check at least once per minute
			}

			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()

			for range ticker.C {
				cleaned := agentManager.CleanupOfflineAgents(maxOfflineDuration)
				if cleaned > 0 {
					log.Printf("Cleaned up %d offline agents (offline > %v)", cleaned, maxOfflineDuration)
				}
			}
		}()
	} else {
		log.Printf("Offline agent cleanup disabled")
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
				log.Fatalf("TLS certificate file not found: %s", cfg.Server.TLSCertFile)
			}
			if _, err := os.Stat(cfg.Server.TLSKeyFile); os.IsNotExist(err) {
				log.Fatalf("TLS key file not found: %s", cfg.Server.TLSKeyFile)
			}

			log.Printf("Starting HTTPS server on %s", addr)

			if err := server.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start HTTPS server: %v", err)
			}
		} else {
			log.Printf("Starting HTTP server on %s", addr)
			log.Printf("Warning: TLS is disabled. Consider enabling TLS for production use.")

			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Failed to start HTTP server: %v", err)
			}
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down server...")
}

// setupLogging configures the logging based on the log level
func setupLogging(level string) {
	// Set up logging format
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Configure log level (simplified implementation)
	switch level {
	case "debug":
		// In a real implementation, this would configure more verbose logging
	case "info":
		// Default level
	case "warn":
		// In a real implementation, this would filter out info logs
	case "error":
		// In a real implementation, this would filter out info and warn logs
	default:
		log.Printf("Unknown log level: %s, using 'info'", level)
	}
}
