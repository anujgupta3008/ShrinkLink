package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"url-shortener/internal/config"
	"url-shortener/internal/database"
	"url-shortener/internal/handler"
	"url-shortener/internal/idgen"
	"url-shortener/internal/middleware"
	"url-shortener/internal/redis"
	"url-shortener/internal/worker"
)

func main() {
	log.Println("Starting Distributed URL Shortener Service...")

	// 1. Load configuration
	cfg := config.LoadConfig()
	log.Printf("Loaded DB config - host: %s, port: %s, user: %s, name: %s", cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBName)

	// Set Gin mode
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 2. Connect to Redis
	rdb, err := redis.NewClient(cfg)
	if err != nil {
		log.Fatalf("Critical error connecting to Redis: %v", err)
	}
	log.Println("Successfully connected to Redis")

	// 3. Connect to PostgreSQL
	db, err := database.NewDB(cfg)
	if err != nil {
		log.Fatalf("Critical error connecting to Database: %v", err)
	}
	log.Println("Successfully connected to Database")

	// 4. Initialize ID Allocator (Range size: 1000)
	allocator := idgen.NewAllocator(rdb, 1000)

	// 5. Initialize Handlers
	h := handler.NewHandler(db, rdb, allocator, cfg.BaseURL)

	// 6. Start Background Analytics Worker
	workerCtx, workerCancel := context.WithCancel(context.Background())
	// NOTE: workerCancel is NOT deferred here — it must be called explicitly after
	// server.Shutdown() returns, so in-flight requests finish queuing analytics
	// events before the worker stops consuming them.

	analyticsWorker := worker.NewAnalyticsWorker(db, rdb, "queue:analytics", 50, 2*time.Second)
	go analyticsWorker.Start(workerCtx)

	// 7. Setup HTTP Router
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Enable CORS for frontend flexibility
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Register API Routes with per-route rate limits.
	// POST /shorten is a write op (DB + Redis) — stricter 10 req/min per IP.
	// GET  /analytics and /urls are read-only — 30 req/min per IP.
	api := router.Group("/api")
	{
		api.POST("/shorten", middleware.RateLimiter(rdb, 10, time.Minute), h.Shorten)
		api.GET("/analytics/:code", middleware.RateLimiter(rdb, 30, time.Minute), h.GetAnalytics)
		api.GET("/urls", middleware.RateLimiter(rdb, 30, time.Minute), h.GetAllURLs)
	}

	// Serve static files if web directory is present (for local running without Nginx)
	if _, err := os.Stat("web"); err == nil {
		router.StaticFile("/", "web/index.html")
		router.StaticFile("/index.html", "web/index.html")
		router.StaticFile("/styles.css", "web/styles.css")
		router.StaticFile("/app.js", "web/app.js")
	} else if _, err := os.Stat("../web"); err == nil {
		router.StaticFile("/", "../web/index.html")
		router.StaticFile("/index.html", "../web/index.html")
		router.StaticFile("/styles.css", "../web/styles.css")
		router.StaticFile("/app.js", "../web/app.js")
	}

	// Redirection route
	router.GET("/:code", h.Redirect)

	// 8. Graceful HTTP Server Startup
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("HTTP Server is listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to listen and serve: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server gracefully...")

	// Timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	log.Println("HTTP server stopped. Draining analytics worker...")

	// Cancel the worker now that the HTTP server is fully stopped —
	// no new analytics events can be enqueued after this point.
	workerCancel()

	// Give the worker up to 3 seconds to flush its current batch to PostgreSQL.
	time.Sleep(3 * time.Second)

	log.Println("Server exiting")
}
