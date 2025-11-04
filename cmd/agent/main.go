package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
)

func main() {
	// Parse command line flags
	configFile := flag.String("c", "agent.yaml", "Path to agent configuration file")
	flag.Parse()

	// Load agent configuration
	agentConfig, err := config.LoadAgentConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load agent configuration: %v", err)
	}

	log.Printf("Starting YALS Agent: %s", agentConfig.Agent.Name)
	log.Printf("Server: %s:%d", agentConfig.Server.Host, agentConfig.Server.Port)
	log.Printf("Loaded %d allowed commands", len(agentConfig.Commands))

	// Create agent client with configuration
	agentClient := agent.NewClientWithConfig(agentConfig)

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Connect to server with retry logic
	go func() {
		for {
			err := agentClient.ConnectToServer()
			if err != nil {
				log.Printf("Connection failed: %v", err)
				log.Println("Retrying in 10 seconds...")
				time.Sleep(10 * time.Second)
				continue
			}
			// If we reach here, connection was closed normally
			log.Println("Connection closed, retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}()

	// Wait for interrupt signal
	<-stop
	log.Println("Shutting down agent...")
}
