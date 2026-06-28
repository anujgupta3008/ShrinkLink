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
	defer workerCancel()

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

	// Register API Routes with Rate Limiting (e.g. 30 requests per minute per IP)
	api := router.Group("/api")
	api.Use(middleware.RateLimiter(rdb, 30, time.Minute))
	{
		api.POST("/shorten", h.Shorten)
		api.GET("/analytics/:code", h.GetAnalytics)
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
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server gracefully...")

	// Timeout context for shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Stop analytics worker
	workerCancel()
	// Wait a moment for worker to flush
	time.Sleep(1 * time.Second)

	log.Println("Server exiting")
}
