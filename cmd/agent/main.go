package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"YALS_SSH/internal/agent"
)

func main() {
	// Parse command line flags
	listen := flag.String("l", "0.0.0.0:9527", "Listen address and port")
	password := flag.String("p", "", "Connection password")
	flag.Parse()

	if *password == "" {
		log.Fatal("Password is required. Use -p flag to specify password.")
	}

	log.Printf("Starting YALS Agent on %s", *listen)

	// Create agent client
	agentClient := agent.NewClient(*password)

	// Set up HTTP server for WebSocket connections
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", agentClient.HandleWebSocket)

	server := &http.Server{
		Addr:    *listen,
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start agent server: %v", err)
		}
	}()

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down agent...")
}
