package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/golroblox/golrox-realtime/internal/client"
	"github.com/golroblox/golrox-realtime/internal/config"
	"github.com/golroblox/golrox-realtime/internal/handler"
	"github.com/golroblox/golrox-realtime/internal/middleware"
	"github.com/golroblox/golrox-realtime/internal/pkg/logger"
	"github.com/golroblox/golrox-realtime/internal/service"
)

func main() {
	// Load .env files (try multiple paths)
	envPaths := []string{"/secrets/.env", ".env"}
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			log.Printf("Loaded environment from %s", path)
			break
		}
	}

	// Load configuration
	cfg := config.Load()

	// Initialize logger
	appLogger := logger.New(cfg.LogLevel)
	defer appLogger.Sync()

	appLogger.Infow("Starting golrox-realtime service...",
		"appEnv", cfg.AppEnv,
		"port", cfg.Port,
		"rabbitmqExchange", cfg.RabbitMQExchange,
		"rabbitmqQueue", cfg.RabbitMQQueue,
	)

	// Setup context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Create hub (central connection manager)
	hub := service.NewHub(appLogger)
	go hub.Run()
	appLogger.Info("Hub started")

	// Create event dispatcher
	dispatcher := service.NewEventDispatcher(hub, appLogger)

	// Create and connect RabbitMQ consumer
	consumer := client.NewRabbitMQConsumer(&cfg, appLogger, dispatcher)
	if err := consumer.Connect(ctx); err != nil {
		appLogger.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer consumer.Disconnect()

	// Create authenticator
	authenticator := middleware.NewAuthenticator(&cfg, appLogger)

	// Create handlers
	wsHandler := handler.NewWebSocketHandler(hub, authenticator, &cfg, appLogger)
	healthHandler := handler.NewHealthHandler(hub, consumer)

	// Setup Gin router
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	// CORS middleware
	engine.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		for _, allowed := range cfg.CORSOrigins {
			if allowed == "*" || allowed == origin {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Routes
	engine.GET("/health", healthHandler.Health)
	engine.GET("/ws", wsHandler.HandleWebSocket)
	engine.GET("/socket.io/", wsHandler.HandleWebSocket) // Compatibility endpoint

	// Create HTTP server
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		appLogger.Infof("golrox-realtime started successfully on port %d", cfg.Port)
		appLogger.Infow("WebSocket endpoints available",
			"primary", fmt.Sprintf("ws://localhost:%d/ws", cfg.Port),
			"compatibility", fmt.Sprintf("ws://localhost:%d/socket.io/", cfg.Port),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	appLogger.Info("Received shutdown signal, starting graceful shutdown...")

	// Shutdown hub (closes all WebSocket connections)
	hub.Shutdown()

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		appLogger.Warnf("Graceful shutdown failed: %v", err)
	}

	appLogger.Info("Graceful shutdown completed")
}
