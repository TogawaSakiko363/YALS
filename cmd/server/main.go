package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"YALS/internal/agent"
	"YALS/internal/config"
	"YALS/internal/handler"
	"YALS/internal/logger"
	"YALS/internal/plugin"
	serverstore "YALS/internal/store/server"
	yalstls "YALS/internal/tls"
	"YALS/internal/utils"

	// Register agent plugin metadata so the control API can enumerate plugins and
	// server-side target validation can see each plugin's ignore_target /
	// maximum_queue overrides. Plugins only execute on agents; this import only
	// registers their metadata, it does not run them.
	_ "YALS/internal/plugin/agent"

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

	runtimeSettings, err := store.EnsureRuntimeSettings(cfg.DefaultRuntimeSettings())
	if err != nil {
		logger.Fatalf("Failed to initialize runtime settings: %v", err)
	}

	if _, err := os.Stat(*webDir); os.IsNotExist(err) {
		logger.Warnf("Web directory '%s' does not exist", *webDir)
	} else {
		logger.Infof("Using web directory: %s", *webDir)
	}

	agentManager := agent.NewManager()
	seedStoredAgents(agentManager, store, cfg)

	h := handler.NewHandler(agentManager, store, *runtimeSettings)

	// Load latency-probe targets, wire agent metrics/probe reports to the store,
	// and start the targets hot-reload watcher + retention pruner.
	h.InitProbing("targets.yaml")

	// Load (or generate and persist) this server's self-signed certificate. It is
	// no longer committed to the repo; each instance has its own under <data>/tls.
	// Agents verify the server with standard CA validation, so for a trusted setup
	// (browsers and agents over the public internet) terminate TLS at a reverse
	// proxy / CDN holding a CA-trusted certificate for the domain.
	tlsDir := filepath.Join(filepath.Dir(cfg.Database.Path), "tls")
	serverCert, err := yalstls.LoadOrGenerateServerCert(tlsDir, cfg.Server.Host)
	if err != nil {
		logger.Fatalf("Failed to load/generate TLS certificate: %v", err)
	}
	logger.Infof("Using self-signed TLS certificate from %s", tlsDir)
	logger.Warnf("Agents validate the server certificate against system CAs; terminate TLS with a CA-trusted certificate (reverse proxy / CDN) for public deployments")

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	grpcServer := newGRPCServer(*runtimeSettings)

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
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCert},
			MinVersion:   tls.VersionTLS12,
		},
		// Drop the stdlib's benign "TLS handshake error" lines (see
		// httpErrorLogFilter); they are expected with the built-in self-signed
		// certificate and would otherwise flood the log on every browser hit.
		ErrorLog: log.New(httpErrorLogFilter{}, "", log.Ldate|log.Ltime|log.Lshortfile),
	}

	go func() {
		logger.Infof("Starting unified HTTPS server (gRPC + HTTP) on %s", addr)
		// Empty cert/key paths make ListenAndServeTLS use TLSConfig.Certificates.
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
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

// httpErrorLogFilter is the writer behind the HTTPS server's ErrorLog. It drops
// the stdlib's benign "TLS handshake error" lines — emitted whenever a client
// rejects the built-in self-signed certificate (every browser does, since it is
// untrusted by design) or a port scanner opens and immediately drops the
// connection. These are expected noise, not actionable faults. Every other line
// is forwarded to stdout unchanged, matching the rest of the server's logging.
type httpErrorLogFilter struct{}

func (httpErrorLogFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("http: TLS handshake error")) {
		return len(p), nil
	}
	return os.Stdout.Write(p)
}

func newGRPCServer(settings config.RuntimeSettings) *grpc.Server {
	config.NormalizeRuntimeSettings(&settings)
	return grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(settings.GRPC.PingInterval) * time.Second,
			Timeout: time.Duration(settings.GRPC.PongWait) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
}

func setupLogging(level string) {
	logger.SetGlobalLevelFromString(level)
	logger.Debugf("Logging level set to: %s", level)
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
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
