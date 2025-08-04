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
	configFile := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	setupLogging(cfg.Server.LogLevel)

	agentManager := agent.NewManager(cfg)
	agentManager.Connect()

	pingInterval := time.Duration(cfg.WebSocket.PingInterval) * time.Second
	pongWait := time.Duration(cfg.WebSocket.PongWait) * time.Second
	h := handler.NewHandler(agentManager, pingInterval, pongWait)

	mux := http.NewServeMux()
	h.SetupRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		log.Printf("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("Shutting down server...")
}

func setupLogging(level string) {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	switch level {
	case "debug":
	case "info":
	case "warn":
	case "error":
	default:
		log.Printf("Unknown log level: %s, using 'info'", level)
	}
}