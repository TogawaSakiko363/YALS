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

	"YALS_SSH/internal/agent"
	"YALS_SSH/internal/config"
	"YALS_SSH/internal/handler"
)

func main() {
	// Parse command line flags
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Set up logging
	setupLogging(cfg.Server.LogLevel)

	// Create agent manager
	agentManager := agent.NewManager(cfg)

	// Connect to agents
	agentManager.Connect()

	// Create HTTP handler
	pingInterval := time.Duration(cfg.WebSocket.PingInterval) * time.Second
	pongWait := time.Duration(cfg.WebSocket.PongWait) * time.Second
	h := handler.NewHandler(agentManager, pingInterval, pongWait)

	// Set up HTTP server
	mux := http.NewServeMux()
	h.SetupRoutes(mux)

	// Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
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