package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lowkruc/warp-proxy-manager/internal/api"
	"github.com/lowkruc/warp-proxy-manager/internal/config"
	"github.com/lowkruc/warp-proxy-manager/internal/docker"
	"github.com/lowkruc/warp-proxy-manager/internal/proxy"
	"github.com/lowkruc/warp-proxy-manager/internal/scaler"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("Failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Setup logging
	if cfg.Manager.LogLevel == "debug" {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	log.Printf("[MAIN] Starting Warp Proxy Manager")
	log.Printf("[MAIN] API port: %d", cfg.Manager.APIPort)
	log.Printf("[MAIN] Proxy listen: %s", cfg.Proxy.Listen)

	// Initialize Docker client
	dockerClient, err := docker.NewClient(cfg)
	if err != nil {
		log.Fatalf("[MAIN] Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Initialize load balancer
	balancer := proxy.NewLoadBalancer(cfg.LoadBalancer.Algorithm)

	// Initialize scaler
	scalerInstance := scaler.NewScaler(cfg, dockerClient, balancer)

	// Initialize proxy server
	proxyServer := proxy.NewProxyServer(cfg, balancer)

	// Start API server
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(api.CORSMiddleware())
	router.Use(api.LoggerMiddleware())

	// Register API routes
	handler := api.NewHandler(cfg, dockerClient, balancer, scalerInstance, proxyServer)
	handler.RegisterRoutes(router)

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	apiAddr := fmt.Sprintf(":%d", cfg.Manager.APIPort)
	apiServer := &http.Server{
		Addr:    apiAddr,
		Handler: router,
	}

	// Start API server
	go func() {
		log.Printf("[MAIN] API server listening on %s", apiAddr)
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[MAIN] API server failed: %v", err)
		}
	}()

	// Start proxy server
	if err := proxyServer.Start(); err != nil {
		log.Fatalf("[MAIN] Failed to start proxy server: %v", err)
	}

	// Start scaler
	scalerInstance.Start()

	// Ensure at least one container is running
	ensureMinimumContainers(dockerClient, cfg)

	log.Printf("[MAIN] Warp Proxy Manager started successfully")
	log.Printf("[MAIN] Proxy: %s", cfg.Proxy.Listen)
	log.Printf("[MAIN] API: %s", apiAddr)

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("[MAIN] Shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop scaler
	scalerInstance.Stop()

	// Stop proxy
	proxyServer.Stop()

	// Stop API server
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("[MAIN] API server shutdown error: %v", err)
	}

	log.Printf("[MAIN] Shutdown complete")
}

func ensureMinimumContainers(dockerClient *docker.Client, cfg *config.Config) {
	containers, err := dockerClient.ListContainers(context.Background())
	if err != nil {
		log.Printf("[MAIN] Failed to list containers: %v", err)
		return
	}

	running := 0
	for _, c := range containers {
		if c.Status == "running" {
			running++
		}
	}

	if running < cfg.Scaling.Min {
		needed := cfg.Scaling.Min - running
		log.Printf("[MAIN] Ensuring minimum containers: need %d more", needed)
		for i := 0; i < needed; i++ {
			container, err := dockerClient.CreateContainer(context.Background(), "")
			if err != nil {
				log.Printf("[MAIN] Failed to create container: %v", err)
				continue
			}
			log.Printf("[MAIN] Created container: %s", container.Name)
		}
	}
}
